package runtime

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/inbound"
	"yzj-bridge/internal/yzjcli"
)

// pollTarget is one bot subscribed to a polled group.
type pollTarget struct {
	BotID        string
	Aliases      []string
	BotOpenIDs   []string
	ImageTrigger string // quote_bot | any | off
	Trigger      string // mention | direct | any
	ChannelGroup string // bot 通道的 group 标签，用于识别继承了错误 yzj_group_id 的通道
}

// pollGroup identifies the group behind a watcher key.
type pollGroup struct {
	GroupID   string
	GroupName string
	Profile   string
}

func mapFrom(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func strFrom(v any, def string) string {
	if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s)
	}
	return def
}

// setupPollers rebuilds the per-group watchers. It is called from Load while
// r.mu is held; watchers start disabled and are switched on by recompute.
func (r *Runtime) setupPollers(bots []*bot.Bot) {
	for _, w := range r.PollWatchers {
		w.Dispose()
	}
	r.PollWatchers = map[string]*yzjcli.Watcher{}
	r.pollTargets = map[string][]pollTarget{}
	r.pollMeta = map[string]pollGroup{}

	cfg := r.yzjCLIConfigLocked()
	interval := r.yzjPollIntervalLocked()
	catchup := r.yzjCatchupPagesLocked()
	startup := r.yzjStartupModeLocked()

	targets := map[string][]pollTarget{}
	meta := map[string]pollGroup{}
	for _, b := range bots {
		c := b.Config
		if !bot.ModeUsesPoll(c.InboundMode) {
			continue
		}
		if len(c.AllowUsers) > 0 {
			log.Printf("yzjcli: bot=%s poll 模式无法按姓名匹配 allow_users（消息只有 openID），白名单可能拒绝所有人；请改用 allow_openids", c.ID)
		}
		gid := strings.TrimSpace(c.YzjGroupID)
		if gid == "" {
			log.Printf("yzjcli: bot %s uses inbound_mode=%s but yzj_group_id is empty; skipped", c.ID, c.InboundMode)
			continue
		}
		profile := strFrom(c.YzjProfile, cfg.Profile)
		key := profile + "|" + gid
		aliases := append([]string(nil), c.YzjBotNames...)
		if len(aliases) == 0 && strings.TrimSpace(c.Name) != "" {
			aliases = []string{strings.TrimSpace(c.Name)}
		}
		var ids []string
		if strings.TrimSpace(c.YzjBotOpenID) != "" {
			ids = []string{strings.TrimSpace(c.YzjBotOpenID)}
		}
		trigger := strings.ToLower(strings.TrimSpace(c.YzjImageTrigger))
		if trigger == "" {
			trigger = "quote_bot"
		}
		msgTrigger := strings.ToLower(strings.TrimSpace(c.YzjTrigger))
		if msgTrigger == "" {
			msgTrigger = "mention"
		}
		targets[key] = append(targets[key], pollTarget{
			BotID: c.ID, Aliases: aliases, BotOpenIDs: ids,
			ImageTrigger: trigger, Trigger: msgTrigger, ChannelGroup: c.Group,
		})
		if _, ok := meta[key]; !ok {
			meta[key] = pollGroup{GroupID: gid, GroupName: c.YzjGroupName, Profile: profile}
		}
	}
	if len(targets) == 0 {
		return
	}
	state, err := yzjcli.OpenState("")
	if err != nil {
		log.Printf("yzjcli: open poll state: %v", err)
		return
	}
	var downloadClient *yzjcli.Client
	for key := range targets {
		m := meta[key]
		profileCfg := cfg
		profileCfg.Profile = m.Profile
		// Load 持锁执行，绝不能在锁内下载安装（最坏会卡住 GUI 数分钟）。
		// 未安装时只标记该群不可用；安装由用户显式完成或轮询启动前另行处理。
		bin, err := yzjcli.ResolveBinary(profileCfg)
		if err != nil {
			log.Printf("yzjcli: group %s (%s) disabled: %v（请先安装 yzj-cli 再重启桥）", m.GroupName, m.GroupID, err)
			continue
		}
		client := yzjcli.NewClient(bin, m.Profile, profileCfg.ConfigDir, profileCfg.Timeout)
		if downloadClient == nil {
			downloadClient = client
		}
		groupID := m.GroupID
		watcher := &yzjcli.Watcher{
			Client: client, Store: state, Key: key,
			GroupID: groupID, GroupName: m.GroupName,
			Interval: interval, CatchupPages: catchup, StartupMode: startup,
			OnMessage: func(ctx context.Context, msg yzjcli.Message) {
				r.onPollMessage(key, msg)
			},
			OnAuthGiveUp: func(k, reason string) {
				log.Printf("yzjcli: group %s paused (re-login required): %s", groupID, reason)
			},
		}
		r.PollWatchers[key] = watcher
	}
	if r.Disp != nil {
		enabled, maxMB, types, _ := r.yzjImageSettingsLocked()
		r.Disp.ImageEnabled = enabled
		r.Disp.ImageMaxMB = maxMB
		r.Disp.ImageTypes = types
		if downloadClient != nil {
			r.Disp.DownloadAttachment = downloadClient.FileDownload
		}
	}
	r.pollTargets = targets
	r.pollMeta = meta
	if err := state.Save(); err != nil {
		log.Printf("yzjcli: save poll state: %v", err)
	}
}

// onPollMessage maps one CLI message to the bots subscribed to its group.
func (r *Runtime) onPollMessage(key string, msg yzjcli.Message) {
	r.mu.Lock()
	targets := append([]pollTarget(nil), r.pollTargets[key]...)
	enabled := make(map[string]bool, len(targets))
	for _, t := range targets {
		v, ok := r.enabled[t.BotID]
		if !ok {
			v = true // default on until explicitly disabled
		}
		enabled[t.BotID] = v
	}
	meta := r.pollMeta[key]
	disp := r.Disp
	r.mu.Unlock()
	if disp == nil || meta.GroupID == "" {
		return
	}
	for _, t := range targets {
		if !enabled[t.BotID] {
			continue
		}
		// 多通道角色如果都继承了同一个 yzj_group_id，会共用同一 watcher：
		// 只允许通道 group 标签与本 watcher 群名一致的 bot 响应，避免跨群抢答。
		if len(targets) > 1 && meta.GroupName != "" && !samePollGroupLabel(t.ChannelGroup, meta.GroupName) {
			log.Printf("yzjcli: bot=%s poll skip: channel group=%q does not match group=%q (check per-channel yzj_group_id)",
				t.BotID, t.ChannelGroup, meta.GroupName)
			continue
		}
		if !pollTrigger(t, msg) {
			continue
		}
		norm := inbound.Normalized{
			OpenID:      msg.FromOpenID,
			MsgID:       msg.MsgID,
			Content:     msg.DisplayText(),
			Source:      "yzj_cli",
			ForceTarget: true,
			Attachments: convertAttachments(msg.Attachments()),
		}
		disp.Handle(t.BotID, norm)
	}
}

// pollTrigger decides whether a message should reach a bot for this group.
// Robot-sent messages never trigger (prevents reply loops).
// - mention: text requires @bot; media defaults to "quote the bot"
// - direct:  personal robot / DM conversations: every user message triggers
// - any:     every user message in a group triggers (opt-in)
func pollTrigger(t pollTarget, msg yzjcli.Message) bool {
	if msg.IsFromBot() {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(t.Trigger)) {
	case "direct", "any":
		return true
	}
	if msg.HasImages() {
		switch t.ImageTrigger {
		case "off":
			// fall through to mention check below
		case "any":
			return true
		default: // quote_bot
			if msg.QuotesBot(t.Aliases, t.BotOpenIDs) {
				return true
			}
		}
	}
	return msg.MentionsBots(t.Aliases, t.BotOpenIDs)
}

func convertAttachments(in []yzjcli.Attachment) []inbound.Attachment {
	if len(in) == 0 {
		return nil
	}
	out := make([]inbound.Attachment, 0, len(in))
	for _, a := range in {
		out = append(out, inbound.Attachment{
			Kind: a.Kind, FileID: a.FileID, Name: a.Name, Ext: a.Ext,
			Size: a.Size, Width: a.Width, Height: a.Height,
		})
	}
	return out
}

// recomputePollersLocked starts/stops watchers according to the enabled map.
// A watcher runs while at least one of its bots is enabled. Missing entries
// default to enabled (same as WSS clients on first run).
func (r *Runtime) recomputePollersLocked() {
	for key, w := range r.PollWatchers {
		en := false
		for _, t := range r.pollTargets[key] {
			v, ok := r.enabled[t.BotID]
			if !ok {
				v = true
			}
			if v {
				en = true
				break
			}
		}
		w.SetEnabled(en)
	}
}

// pollStatusLocked is used by SnapshotStatus.
func (r *Runtime) pollStatusLocked(c bot.Config) (enabled bool, lastPoll, lastMsg time.Time, lastErr string) {
	gid := strings.TrimSpace(c.YzjGroupID)
	if gid == "" {
		return false, time.Time{}, time.Time{}, ""
	}
	profile := strFrom(c.YzjProfile, r.yzjCLIConfigLocked().Profile)
	w := r.PollWatchers[profile+"|"+gid]
	if w == nil {
		return false, time.Time{}, time.Time{}, ""
	}
	return w.Snapshot()
}

func (r *Runtime) yzjDefaultsLocked() map[string]any {
	return mapFrom(r.Defaults["yzj_cli"])
}

// defaultBlockedFileExts 是附件下载的默认拒绝列表：这些扩展名一律不落盘。
var defaultBlockedFileExts = []string{
	".exe", ".dll", ".msi", ".scr", ".com", ".bat", ".cmd",
	".vbs", ".ps1", ".lnk", ".jar", ".reg", ".hta", ".cpl",
}

// yzjAttachmentSettingsLocked 读取附件（文件）下载配置，缺省开启。
func (r *Runtime) yzjAttachmentSettingsLocked() (enabled bool, maxMB, maxFiles int, block []string) {
	enabled, maxMB, maxFiles, block = true, 50, 5, defaultBlockedFileExts
	att := mapFrom(r.yzjDefaultsLocked()["attachment"])
	if b, ok := att["files_enabled"].(bool); ok {
		enabled = b
	}
	maxMB = intFrom(att["file_max_mb"], maxMB)
	maxFiles = intFrom(att["max_files"], maxFiles)
	if list, ok := att["file_block_ext"].([]any); ok && len(list) > 0 {
		var out []string
		for _, v := range list {
			if s := strings.TrimSpace(strFrom(v, "")); s != "" {
				out = append(out, s)
			}
		}
		if len(out) > 0 {
			block = out
		}
	}
	if maxMB <= 0 {
		maxMB = 50
	}
	if maxFiles <= 0 {
		maxFiles = 5
	}
	return enabled, maxMB, maxFiles, block
}

// applyMediaSettingsLocked 把图片/附件下载设置与「是否存在轮询群」解耦。
// 纯 WSS 部署也需要下载器：WSS 帧里只有 file_id，实际字节必须由 yzj-cli 换取。
func (r *Runtime) applyMediaSettingsLocked(bots []*bot.Bot) {
	if r.Disp == nil {
		return
	}
	enabled, maxMB, types, _ := r.yzjImageSettingsLocked()
	r.Disp.ImageEnabled = enabled
	r.Disp.ImageMaxMB = maxMB
	r.Disp.ImageTypes = types

	filesOn, fileMaxMB, maxFiles, block := r.yzjAttachmentSettingsLocked()
	r.Disp.FileEnabled = filesOn
	r.Disp.FileMaxMB = fileMaxMB
	r.Disp.MaxAttachments = maxFiles
	r.Disp.FileBlockExt = block

	if r.Disp.DownloadAttachment != nil {
		return
	}
	// 只有可能收到附件的入站方式才需要下载器，避免纯 Cursor/OpenAI 部署
	// 在每次 Load 时刷「yzj-cli 未就绪」告警。
	needs := false
	for _, b := range bots {
		mode := b.Config.InboundMode
		if bot.ModeUsesWS(mode) || bot.ModeUsesWebhook(mode) || bot.ModeUsesPoll(mode) {
			needs = true
			break
		}
	}
	if !needs {
		return
	}
	cfg := r.yzjCLIConfigLocked()
	bin, err := yzjcli.ResolveBinary(cfg)
	if err != nil {
		log.Printf("yzjcli: 附件下载不可用（%v）；图片/文件会被跳过，请先安装 yzj-cli 再重启桥", err)
		return
	}
	client := yzjcli.NewClient(bin, cfg.Profile, cfg.ConfigDir, cfg.Timeout)
	r.Disp.DownloadAttachment = client.FileDownload
}

func (r *Runtime) yzjCLIConfigLocked() yzjcli.Config {
	m := r.yzjDefaultsLocked()
	cfg := yzjcli.Config{
		Version: "0.1.8", Profile: "default", Timeout: 20 * time.Second, AutoInstall: true,
	}
	cfg.Bin = strFrom(m["bin"], "")
	cfg.Version = strFrom(m["version"], cfg.Version)
	cfg.Profile = strFrom(m["profile"], cfg.Profile)
	cfg.ConfigDir = strFrom(m["config_dir"], "")
	if b, ok := m["auto_install"].(bool); ok {
		cfg.AutoInstall = b
	}
	if sec := intFrom(m["timeout_sec"], 20); sec > 0 {
		cfg.Timeout = time.Duration(sec) * time.Second
	}
	return cfg
}

func (r *Runtime) yzjPollIntervalLocked() time.Duration {
	m := r.yzjDefaultsLocked()
	sec := intFrom(m["poll_interval_sec"], 5)
	if sec < 1 {
		sec = 1
	}
	return time.Duration(sec) * time.Second
}

func (r *Runtime) yzjCatchupPagesLocked() int {
	m := r.yzjDefaultsLocked()
	n := intFrom(m["catchup_pages"], 3)
	if n < 1 {
		n = 1
	}
	if n > 10 {
		n = 10
	}
	return n
}

func (r *Runtime) yzjStartupModeLocked() string {
	m := r.yzjDefaultsLocked()
	return strings.ToLower(strFrom(m["startup_mode"], "baseline"))
}

// yzjImageSettingsLocked reads defaults.yzj_cli.image.
func (r *Runtime) yzjImageSettingsLocked() (enabled bool, maxMB int, types []string, keepDays int) {
	img := mapFrom(r.yzjDefaultsLocked()["image"])
	enabled = true
	if b, ok := img["enabled"].(bool); ok {
		enabled = b
	}
	maxMB = intFrom(img["max_mb"], 20)
	if maxMB <= 0 {
		maxMB = 20
	}
	if list, ok := img["types"].([]any); ok {
		for _, v := range list {
			if s := strings.TrimSpace(strFrom(v, "")); s != "" {
				types = append(types, s)
			}
		}
	}
	keepDays = intFrom(img["keep_days"], 7)
	if keepDays < 1 {
		keepDays = 1
	}
	return enabled, maxMB, types, keepDays
}

// cleanupIncomingMedia removes old files under each bot workspace's
// incoming_media directory so downloaded images do not accumulate forever.
func (r *Runtime) cleanupIncomingMedia(bots []*bot.Bot) {
	_, _, _, keepDays := r.yzjImageSettingsLocked()
	cutoff := time.Now().Add(-time.Duration(keepDays) * 24 * time.Hour)
	seen := map[string]bool{}
	for _, b := range bots {
		ws := strings.TrimSpace(b.Config.Workspace)
		if ws == "" || seen[ws] {
			continue
		}
		seen[ws] = true
		root := filepath.Join(ws, "incoming_media")
		if st, err := os.Stat(root); err != nil || !st.IsDir() {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if info, err := d.Info(); err == nil && info.ModTime().Before(cutoff) {
				_ = os.Remove(path)
			}
			return nil
		})
	}
}

// YzjCLIStatus reports binary/login state for the control API / GUI.
func (r *Runtime) YzjCLIStatus(ctx context.Context) yzjcli.Status {
	r.mu.Lock()
	cfg := r.yzjCLIConfigLocked()
	r.mu.Unlock()
	return yzjcli.Detect(ctx, cfg)
}

// YzjCLIGroups lists conversations for the group picker (read-only). With a
// keyword it uses `im group search`, otherwise `im group recent`.
func (r *Runtime) YzjCLIGroups(ctx context.Context, keyword string, limit int) ([]yzjcli.Group, error) {
	r.mu.Lock()
	cfg := r.yzjCLIConfigLocked()
	r.mu.Unlock()
	if limit <= 0 || limit > 20 {
		limit = 20
	}
	// 群列表是只读操作：只解析本地已安装的 bin，不在用户点“搜索/拉取”时静默下载安装。
	bin, err := yzjcli.ResolveBinary(cfg)
	if err != nil {
		return nil, fmt.Errorf("yzj-cli 未安装：%w；请先安装 yzj-cli，或启用 auto_install 后再启动轮询", err)
	}
	client := yzjcli.NewClient(bin, cfg.Profile, cfg.ConfigDir, cfg.Timeout)
	if strings.TrimSpace(keyword) != "" {
		return client.GroupSearch(ctx, keyword, limit, 1)
	}
	return client.GroupRecent(ctx, limit, 1)
}

// samePollGroupLabel 比较通道 group 标签与 watcher 群名，忽略“群”后缀和空白。
func samePollGroupLabel(a, b string) bool {
	norm := func(s string) string {
		s = strings.ToLower(strings.TrimSpace(s))
		s = strings.TrimSuffix(s, "群")
		s = strings.ReplaceAll(s, " ", "")
		return s
	}
	a, b = norm(a), norm(b)
	return a != "" && b != "" && a == b
}
