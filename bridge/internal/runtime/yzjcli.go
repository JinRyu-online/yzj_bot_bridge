package runtime

import (
	"context"
	"log"
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
		targets[key] = append(targets[key], pollTarget{
			BotID: c.ID, Aliases: aliases, BotOpenIDs: ids, ImageTrigger: trigger,
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
	for key, tg := range targets {
		m := meta[key]
		profileCfg := cfg
		profileCfg.Profile = m.Profile
		bin, err := yzjcli.ResolveBinary(profileCfg)
		if err != nil && profileCfg.AutoInstall {
			installCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			bin, err = yzjcli.EnsureBinary(installCtx, profileCfg)
			cancel()
		}
		if err != nil {
			log.Printf("yzjcli: group %s (%s) disabled: %v", m.GroupName, m.GroupID, err)
			continue
		}
		client := yzjcli.NewClient(bin, m.Profile, profileCfg.ConfigDir, profileCfg.Timeout)
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
		_ = tg
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
		if !pollTrigger(t, msg) {
			continue
		}
		norm := inbound.Normalized{
			OpenID:      msg.FromOpenID,
			MsgID:       msg.MsgID,
			Content:     msg.DisplayText(),
			GroupID:     meta.GroupID,
			MsgType:     msg.MsgType,
			Source:      "yzj_cli",
			ForceTarget: true,
			Attachments: convertAttachments(msg.Attachments()),
		}
		disp.Handle(t.BotID, norm)
	}
}

// pollTrigger decides whether a message should reach a bot for this group.
// Text requires an @bot; media defaults to "quote the bot" so image-only
// messages can trigger without an @ (per product decision).
func pollTrigger(t pollTarget, msg yzjcli.Message) bool {
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
	bin, err := yzjcli.EnsureBinary(ctx, cfg)
	if err != nil {
		return nil, err
	}
	client := yzjcli.NewClient(bin, cfg.Profile, cfg.ConfigDir, cfg.Timeout)
	if strings.TrimSpace(keyword) != "" {
		return client.GroupSearch(ctx, keyword, limit, 1)
	}
	return client.GroupRecent(ctx, limit, 1)
}
