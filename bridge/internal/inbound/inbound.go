package inbound

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/commands"
	"yzj-bridge/internal/dedupe"
	"yzj-bridge/internal/jobs"
	"yzj-bridge/internal/memory"
	"yzj-bridge/internal/orchestrator"
	"yzj-bridge/internal/registry"
	"yzj-bridge/internal/sessions"
	"yzj-bridge/internal/yzjcli"
	"yzj-bridge/internal/yzjout"
)

type Normalized struct {
	OpenID    string
	Name      string
	MsgID     string
	Content   string
	RobotID   string
	RobotName string
	GroupType int
	// GroupID is the platform conversation id (WSS 帧里是 groupId，可用于兜底
	// 反查消息参数；轮询路径为群 openId)。
	GroupID string
	// Source identifies the inbound channel: websocket|webhook|yzj_cli.
	Source string
	// Attachments are media items carried by the message (Phase 2 downloads
	// them to the bot workspace before dispatch).
	Attachments []Attachment
	// ForceTarget skips the "@bot" text check because the poller already
	// matched the message (image quote, at-segment, etc.).
	ForceTarget bool
	Raw         map[string]any
}

// Attachment is one media item carried by an inbound message.
type Attachment struct {
	Kind   string // image | file
	FileID string
	Name   string
	Ext    string
	Size   int64
	Width  int
	Height int
}

type Dispatcher struct {
	Reg    *registry.Registry
	Orch   *orchestrator.Orchestrator
	Dedupe *dedupe.Store
	Jobs   *jobs.Manager
	Store  *sessions.Store
	Memory *memory.Service
	// SendText, when set, replaces yzjout.SendText (tests).
	SendText func(sendURL, content, openID string, reply *yzjout.ReplyMeta) error
	// DownloadAttachment downloads one yzj-cli media item to outPath.
	DownloadAttachment func(ctx context.Context, fileID, outPath string) error
	// ImageEnabled turns image download/prompt injection on.
	ImageEnabled bool
	ImageMaxMB   int
	ImageTypes   []string
	// FileEnabled turns non-image attachment (document) download on.
	FileEnabled bool
	// FileMaxMB caps a single file download (default 50MB).
	FileMaxMB int
	// FileBlockExt is a deny-list of extensions (".exe", ".bat", ...).
	FileBlockExt []string
	// MaxAttachments caps attachments handled per message (default 5).
	MaxAttachments int
}

type queuedJob struct {
	Bot *bot.Bot
	N   Normalized
	Cmd commands.Result
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			s := strings.TrimSpace(fmt.Sprint(v))
			if s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

func unwrap(msg map[string]any) map[string]any {
	if fmt.Sprint(msg["type"]) == "robotmessage" {
		if inner, ok := msg["msg"].(map[string]any); ok {
			return inner
		}
	}
	for _, k := range []string{"data", "payload", "body", "message", "msg"} {
		switch v := msg[k].(type) {
		case map[string]any:
			return v
		case string:
			var m map[string]any
			if json.Unmarshal([]byte(v), &m) == nil {
				return m
			}
		}
	}
	return msg
}

func Normalize(raw map[string]any) (Normalized, bool) {
	msg := unwrap(raw)
	content := firstString(msg, "content", "text")
	if content == "" {
		if m, ok := msg["content"].(map[string]any); ok {
			content = firstString(m, "text", "content")
		}
	}
	n := Normalized{
		OpenID: firstString(msg, "operatorOpenid", "operatorOpenId", "operator_openid", "openId", "openid",
			"fromOpenId", "fromOpenid", "senderOpenId", "senderOpenid", "userId", "userid", "uid"),
		Name:      firstString(msg, "operatorName", "operator_name", "fromName", "senderName", "userName", "name", "nickName", "nickname"),
		MsgID:     firstString(msg, "msgId", "msgid", "messageId", "id"),
		Content:   content,
		RobotID:   firstString(msg, "robotId", "robot_id", "botId"),
		RobotName: firstString(msg, "robotName", "robot_name", "botName"),
		Raw:       msg,
	}
	if gt := firstString(msg, "groupType", "group_type"); gt != "" {
		fmt.Sscanf(gt, "%d", &n.GroupType)
	}
	if n.Content == "" {
		return n, false
	}
	if n.MsgID == "" {
		h := sha1.Sum([]byte(n.OpenID + "|" + n.Content + "|" + firstString(msg, "time")))
		n.MsgID = "auto-" + hex.EncodeToString(h[:4])
	}
	return n, true
}

// NormalizeWS 解析 WSS/Webhook 推送帧：在 Normalize 基础上补 GroupID 与附件。
// 平台推送把消息参数放在 msgParam 里，且是「JSON 字符串」，需要二次解析；
// msgType 在 WSS 帧里是数字（2=text、8=file、23=富文本/图片）。
func NormalizeWS(raw map[string]any) (Normalized, bool) {
	n, ok := Normalize(raw)
	if !ok {
		return n, false
	}
	n.Source = "websocket"
	msg := unwrap(raw)
	n.GroupID = firstString(msg, "groupId", "group_id", "groupOpenId", "chatId", "conversationId")
	if n.GroupType == 0 {
		if gt := firstString(msg, "groupType", "group_type"); gt != "" {
			fmt.Sscanf(gt, "%d", &n.GroupType)
		}
	}
	if atts := wsAttachments(msg); len(atts) > 0 {
		n.Attachments = atts
	}
	return n, true
}

// wsAttachments 从 WSS 帧的 msgParam 提取附件（图片 + 文件）。
func wsAttachments(msg map[string]any) []Attachment {
	param, ok := parseMsgParam(msg["msgParam"])
	if !ok {
		return nil
	}
	msgType := normalizeWSMsgType(firstString(msg, "msgType", "msg_type"))
	if msgType == "" {
		return nil
	}
	m := yzjcli.Message{MsgType: msgType, Param: param, Content: firstString(msg, "content")}
	atts := m.Attachments()
	out := make([]Attachment, 0, len(atts))
	for _, a := range atts {
		out = append(out, Attachment{
			Kind: a.Kind, FileID: a.FileID, Name: a.Name, Ext: a.Ext,
			Size: a.Size, Width: a.Width, Height: a.Height,
		})
	}
	return out
}

// parseMsgParam 兼容 msgParam 为 JSON 字符串或已解析对象两种形态。
func parseMsgParam(v any) (yzjcli.MessageParam, bool) {
	switch t := v.(type) {
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return yzjcli.MessageParam{}, false
		}
		var p yzjcli.MessageParam
		if err := json.Unmarshal([]byte(s), &p); err != nil {
			return yzjcli.MessageParam{}, false
		}
		return p, true
	case map[string]any:
		b, err := json.Marshal(t)
		if err != nil {
			return yzjcli.MessageParam{}, false
		}
		var p yzjcli.MessageParam
		if err := json.Unmarshal(b, &p); err != nil {
			return yzjcli.MessageParam{}, false
		}
		return p, true
	}
	return yzjcli.MessageParam{}, false
}

// normalizeWSMsgType 把 WSS 的数字 msgType 映射成轮询语义的字符串。
func normalizeWSMsgType(v string) string {
	switch strings.TrimSpace(v) {
	case "2":
		return "text"
	case "8":
		return "file"
	case "23":
		return "richtext"
	case "":
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "text", "file", "richtext", "other":
		return strings.ToLower(strings.TrimSpace(v))
	}
	return ""
}

type ClassifyResult struct {
	Kind string // control|dispatch|invalid|ack
	Norm Normalized
	Ack  map[string]any
}

func ClassifyWS(raw map[string]any) ClassifyResult {
	cmd := strings.ToLower(fmt.Sprint(raw["cmd"]))
	typ := strings.ToLower(fmt.Sprint(raw["type"]))
	if cmd == "ping" || cmd == "pong" || typ == "ping" || typ == "pong" {
		return ClassifyResult{Kind: "control"}
	}
	if n, ok := NormalizeWS(raw); ok {
		return ClassifyResult{Kind: "dispatch", Norm: n}
	}
	needAck := false
	switch v := raw["needAck"].(type) {
	case bool:
		needAck = v
	case string:
		needAck = v == "true" || v == "1"
	}
	if (cmd == "directpush" || typ == "msgchg") && needAck {
		if seq, ok := raw["seq"]; ok {
			return ClassifyResult{Kind: "ack", Ack: map[string]any{"cmd": "ack", "seq": seq}}
		}
	}
	if cmd != "" || typ != "" || raw["event"] != nil {
		return ClassifyResult{Kind: "control"}
	}
	return ClassifyResult{Kind: "invalid"}
}

func (d *Dispatcher) send(sendURL, content, openID string, reply *yzjout.ReplyMeta) error {
	if d.SendText != nil {
		return d.SendText(sendURL, content, openID, reply)
	}
	return yzjout.SendText(sendURL, content, openID, reply)
}

// sendTracked sends a message and logs the delivery result so outbound
// success/failure is always visible in runtime logs.
func (d *Dispatcher) sendTracked(botID, sendURL, content, openID string, reply *yzjout.ReplyMeta) {
	if err := d.send(sendURL, content, openID, reply); err != nil {
		log.Printf("bot=%s outbound FAIL to=%s len=%d: %v", botID, openID, len([]rune(content)), err)
	} else {
		log.Printf("bot=%s outbound OK to=%s len=%d", botID, openID, len([]rune(content)))
	}
}

func (d *Dispatcher) queueScope(b *bot.Bot, openID string) string {
	return jobs.Scope(b.Config.ID, openID, jobs.UseChannelQueue(b.Config.SessionMode, b.Config.JobQueue))
}

func (d *Dispatcher) Handle(botID string, n Normalized) {
	b := d.Reg.Get(botID)
	if b == nil {
		return
	}
	// WSS token 收到的是账号下所有机器人的推送：按消息里的目标 robotName 过滤，
	// 避免 A 群 @ 了寄件日志查询，其他群/其他机器人也抢答。
	if n.Source == "websocket" && !websocketMessageForBot(b, n) {
		log.Printf("bot=%s ws skip: target robot=%q (this bot=%q)", botID, n.RobotName, b.Config.Name)
		return
	}
	// 不把 Source 放进 key：同一平台消息可能同时走 webhook + WSS（both 模式），
	// 需要按 bot+msgID 跨入口去重；yzj-cli 的 msgID 也来自同一平台。
	key := botID + ":" + n.MsgID
	if d.Dedupe.AlreadySeen(key) {
		return
	}
	if !n.ForceTarget && !commands.MessageTargetsBot(n.Content, b, d.Reg, n.GroupType) {
		return
	}
	if !commands.Allowed(b.Config, n.OpenID, n.Name) {
		d.sendTracked(b.Config.ID, b.Config.SendMsgURL, "你不在白名单中", n.OpenID, &yzjout.ReplyMeta{MsgID: n.MsgID, PersonName: n.Name, GroupType: n.GroupType, Summary: n.Content})
		return
	}
	clean := commands.StripBotMention(n.Content, b, d.Reg.Names(), true)
	cmdRes := commands.Parse(clean, b, d.Reg)
	if cmdRes.Overrides["stop"] == "1" {
		scope := d.queueScope(b, n.OpenID)
		ack := yzjout.FormatStopAck(d.Jobs.Cancel(scope))
		d.sendTracked(b.Config.ID, b.Config.SendMsgURL, ack, n.OpenID, &yzjout.ReplyMeta{MsgID: n.MsgID, PersonName: n.Name, GroupType: n.GroupType, Summary: n.Content})
		return
	}
	if cmdRes.Overrides["jobs"] == "1" {
		scope := d.queueScope(b, n.OpenID)
		body := yzjout.FormatJobList(d.Jobs.Snapshot(scope))
		d.sendTracked(b.Config.ID, b.Config.SendMsgURL, body, n.OpenID, &yzjout.ReplyMeta{MsgID: n.MsgID, PersonName: n.Name, GroupType: n.GroupType, Summary: n.Content})
		return
	}
	if memSub := cmdRes.Overrides["memory"]; memSub != "" {
		body := d.handleMemoryCmd(n.OpenID, memSub)
		body = yzjout.FormatCompletionReply(body, n.OpenID, n.Name, b.Config.MentionOnReply)
		d.sendTracked(b.Config.ID, b.Config.SendMsgURL, body, n.OpenID, &yzjout.ReplyMeta{MsgID: n.MsgID, PersonName: n.Name, GroupType: n.GroupType, Summary: n.Content})
		return
	}
	if cmdRes.Overrides["forget"] == "1" {
		body := d.handleForgetCmd(n.OpenID)
		body = yzjout.FormatCompletionReply(body, n.OpenID, n.Name, b.Config.MentionOnReply)
		d.sendTracked(b.Config.ID, b.Config.SendMsgURL, body, n.OpenID, &yzjout.ReplyMeta{MsgID: n.MsgID, PersonName: n.Name, GroupType: n.GroupType, Summary: n.Content})
		return
	}
	if cmdRes.Handled && strings.TrimSpace(cmdRes.RestText) == "" {
		body := strings.TrimSpace(cmdRes.Reply)
		body = yzjout.FormatCompletionReply(body, n.OpenID, n.Name, b.Config.MentionOnReply)
		d.sendTracked(b.Config.ID, b.Config.SendMsgURL, body, n.OpenID, &yzjout.ReplyMeta{MsgID: n.MsgID, PersonName: n.Name, GroupType: n.GroupType, Summary: n.Content})
		return
	}
	scope := d.queueScope(b, n.OpenID)
	res := d.Jobs.TryAccept(scope, n.OpenID, n.Name, cmdRes.RestText, queuedJob{Bot: b, N: n, Cmd: cmdRes})
	switch res.Status {
	case jobs.StatusMerged:
		if b.Config.AckPending {
			d.sendTracked(b.Config.ID, b.Config.SendMsgURL, "已合并补充内容，稍后一并处理", n.OpenID, nil)
		}
		return
	case jobs.StatusQueued:
		d.sendTracked(b.Config.ID, b.Config.SendMsgURL, yzjout.FormatQueuePosition(res.Position), n.OpenID, nil)
		return
	}
	d.startJob(b, n, cmdRes, true)
}

func (d *Dispatcher) startJob(b *bot.Bot, n Normalized, cmdRes commands.Result, ack bool) {
	if ack && b.Config.AckPending {
		d.sendTracked(b.Config.ID, b.Config.SendMsgURL, yzjout.FormatPendingAck(), n.OpenID, nil)
	}
	go d.runJob(b, n, cmdRes)
}

func (d *Dispatcher) notifyQueue(b *bot.Bot, notices []jobs.Notice) {
	for _, n := range notices {
		d.sendTracked(b.Config.ID, b.Config.SendMsgURL, yzjout.FormatQueuePosition(n.Position), n.OpenID, nil)
	}
}

// prepareAttachments downloads message attachments (images and files) into the
// bot workspace and returns a prompt block with local paths. It never fails the
// job: download or validation problems become a short note in the returned text.
func (d *Dispatcher) prepareAttachments(ctx context.Context, b *bot.Bot, n Normalized) string {
	if len(n.Attachments) == 0 {
		return ""
	}
	if !d.ImageEnabled && !d.FileEnabled {
		return ""
	}
	if d.DownloadAttachment == nil {
		return fmt.Sprintf("（用户随消息发送了 %d 个附件，但附件下载器不可用：yzj-cli 未就绪或未登录，无法读取文件内容）", len(n.Attachments))
	}
	ws := sessions.ResolveAgentWorkspace(b.Config, n.OpenID, d.Store)
	if strings.TrimSpace(ws) == "" {
		ws = b.Config.Workspace
	}
	if strings.TrimSpace(ws) == "" {
		return ""
	}
	allowed := map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true}
	if len(d.ImageTypes) > 0 {
		allowed = map[string]bool{}
		for _, t := range d.ImageTypes {
			if e := normalizeImageExt(t); e != "" {
				allowed[e] = true
			}
		}
	}
	maxMB := d.ImageMaxMB
	if maxMB <= 0 {
		maxMB = 20
	}
	maxBytes := int64(maxMB) << 20
	fileMaxMB := d.FileMaxMB
	if fileMaxMB <= 0 {
		fileMaxMB = 50
	}
	fileMaxBytes := int64(fileMaxMB) << 20
	maxFiles := d.MaxAttachments
	if maxFiles <= 0 {
		maxFiles = 5
	}
	dir := filepath.Join(ws, "incoming_media", time.Now().Format("20060102"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Sprintf("（附件保存目录创建失败：%v）", err)
	}
	safeMsg := safeFileToken(n.MsgID)
	var images, files, fails []string
	fileFailure := false
	idx, imagesN, filesN := 0, 0, 0
	for _, a := range n.Attachments {
		kind := strings.ToLower(strings.TrimSpace(a.Kind))
		if idx >= maxFiles {
			if kind == "file" {
				fileFailure = true
			}
			fails = append(fails, fmt.Sprintf("超出单条消息 %d 个附件上限", maxFiles))
			break
		}
		switch kind {
		case "image":
			if !d.ImageEnabled {
				continue
			}
			ext := normalizeImageExt(a.Ext)
			if ext == "" {
				ext = normalizeImageExt(filepath.Ext(a.Name))
			}
			if ext != "" && !allowed[ext] {
				fails = append(fails, fmt.Sprintf("%s（类型不支持）", displayName(a)))
				continue
			}
			if a.Size > maxBytes && maxBytes > 0 {
				fails = append(fails, fmt.Sprintf("%s（超过 %dMB）", displayName(a), maxMB))
				continue
			}
			idx++
			out := filepath.Join(dir, fmt.Sprintf("%s-%d%s", safeMsg, idx, ext))
			st, err := d.downloadOne(ctx, a.FileID, out)
			if err != nil {
				fails = append(fails, fmt.Sprintf("%s（%v）", displayName(a), err))
				continue
			}
			if maxBytes > 0 && st.Size() > maxBytes {
				_ = os.Remove(out)
				fails = append(fails, fmt.Sprintf("%s（超过 %dMB）", displayName(a), maxMB))
				continue
			}
			mime, ok := sniffImageMime(out)
			if !ok {
				_ = os.Remove(out)
				fails = append(fails, fmt.Sprintf("%s（不是可识别的图片）", displayName(a)))
				continue
			}
			imagesN++
			images = append(images, fmt.Sprintf("- 图片 %d: %s (%s, %s)", imagesN, out, mime, humanSize(st.Size())))
		case "file":
			if !d.FileEnabled {
				continue
			}
			ext := normalizeImageExt(a.Ext)
			if ext == "" {
				ext = normalizeImageExt(filepath.Ext(a.Name))
			}
			if blockedExt(ext, d.FileBlockExt) {
				fileFailure = true
				fails = append(fails, fmt.Sprintf("%s（类型不允许）", displayName(a)))
				continue
			}
			if a.Size > fileMaxBytes && fileMaxBytes > 0 {
				fileFailure = true
				fails = append(fails, fmt.Sprintf("%s（超过 %dMB）", displayName(a), fileMaxMB))
				continue
			}
			idx++
			out := filepath.Join(dir, fmt.Sprintf("%s-%d%s", safeMsg, idx, ext))
			st, err := d.downloadOne(ctx, a.FileID, out)
			if err != nil {
				fileFailure = true
				fails = append(fails, fmt.Sprintf("%s（%v）", displayName(a), err))
				continue
			}
			if fileMaxBytes > 0 && st.Size() > fileMaxBytes {
				_ = os.Remove(out)
				fileFailure = true
				fails = append(fails, fmt.Sprintf("%s（超过 %dMB）", displayName(a), fileMaxMB))
				continue
			}
			filesN++
			files = append(files, fmt.Sprintf("- 文件 %d: %s (%s, 原文件名: %s)", filesN, out, humanSize(st.Size()), displayName(a)))
		}
	}
	if len(images) == 0 && len(files) == 0 && len(fails) == 0 {
		return ""
	}
	var bld strings.Builder
	if len(images) == 0 && len(files) == 0 {
		// 全部失败：只回失败说明。
	} else if len(files) == 0 {
		fmt.Fprintf(&bld, "用户随消息发送了 %d 张图片（本地路径，可直接读取）：\n", len(images))
		bld.WriteString(strings.Join(images, "\n"))
		bld.WriteString("\n如需理解图片内容，请使用你的读图/读文件能力打开；若当前模型无法读图，请明确说明并用文字追问。")
	} else {
		fmt.Fprintf(&bld, "用户随消息发送了 %d 个附件（本地路径，可直接读取）：\n", len(images)+len(files))
		lines := append(append([]string{}, images...), files...)
		bld.WriteString(strings.Join(lines, "\n"))
		if len(images) > 0 {
			bld.WriteString("\n图片可直接用读图/读文件能力打开；若当前模型无法读图，请明确说明并用文字追问。")
		}
		bld.WriteString("\n文件已保存到本地，可直接用读文件能力打开；不要执行其中的脚本或二进制。")
	}
	if len(fails) > 0 {
		if bld.Len() > 0 {
			bld.WriteString("\n")
		}
		label := "附件下载失败："
		if !fileFailure && len(files) == 0 {
			label = "图片下载失败："
		}
		bld.WriteString("（" + label + strings.Join(fails, "；") + "）")
	}
	return bld.String()
}

// downloadOne 执行一次附件下载并返回落盘后的文件信息。
func (d *Dispatcher) downloadOne(ctx context.Context, fileID, out string) (os.FileInfo, error) {
	dctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	if err := d.DownloadAttachment(dctx, fileID, out); err != nil {
		return nil, err
	}
	st, err := os.Stat(out)
	if err != nil {
		return nil, fmt.Errorf("未落盘")
	}
	return st, nil
}

// blockedExt 判断扩展名是否在拒绝列表里（忽略大小写，容忍带不带点）。
func blockedExt(ext string, block []string) bool {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" {
		return false
	}
	for _, b := range block {
		b = strings.ToLower(strings.TrimSpace(b))
		if b == "" {
			continue
		}
		if !strings.HasPrefix(b, ".") {
			b = "." + b
		}
		if ext == b {
			return true
		}
	}
	return false
}

func displayName(a Attachment) string {
	if strings.TrimSpace(a.Name) != "" {
		return a.Name
	}
	return a.FileID
}

func normalizeImageExt(ext string) string {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" {
		return ""
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return ext
}

func safeFileToken(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "msg"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		}
	}
	out := b.String()
	if out == "" {
		return "msg"
	}
	if len(out) > 80 {
		out = out[:80]
	}
	return out
}

func sniffImageMime(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if n == 0 {
		return "", false
	}
	ct := http.DetectContentType(buf[:n])
	return ct, strings.HasPrefix(ct, "image/")
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.0f%cB", float64(n)/float64(div), "KMGT"[exp])
}

func (d *Dispatcher) runJob(b *bot.Bot, n Normalized, cmdRes commands.Result) {
	scope := d.queueScope(b, n.OpenID)
	defer func() {
		next, notices := d.Jobs.Finish(scope, n.OpenID)
		d.notifyQueue(b, notices)
		if next == nil {
			return
		}
		job, _ := next.Payload.(queuedJob)
		if job.Bot == nil {
			job.Bot = b
		}
		job.N.OpenID = next.OpenID
		if next.Name != "" {
			job.N.Name = next.Name
		}
		job.Cmd.RestText = next.Content
		d.startJob(job.Bot, job.N, job.Cmd, true)
	}()
	content := cmdRes.RestText
	if block := d.prepareAttachments(d.Jobs.Context(scope), b, n); block != "" {
		if strings.TrimSpace(content) == "" {
			content = block
		} else {
			content = block + "\n\n" + content
		}
	}
	overrides := cmdRes.Overrides
	var parts []string
	if strings.TrimSpace(cmdRes.Reply) != "" {
		parts = append(parts, strings.TrimSpace(cmdRes.Reply))
	}
	for {
		dr := d.Orch.DispatchWithContext(d.Jobs.Context(scope), b.Config.ID, content, n.OpenID, n.Name, overrides)
		handler := d.Reg.Get(dr.HandlerBotID)
		sendURL := b.Config.SendMsgURL
		mention := b.Config.MentionOnReply
		if handler != nil {
			sendURL = handler.Config.SendMsgURL
			mention = handler.Config.MentionOnReply
		}
		var body string
		if dr.Status == "interrupted" {
			body = yzjout.FormatInterruptReply(dr.Reply, n.OpenID, n.Name, mention)
		} else {
			body = yzjout.FormatCompletionReply(dr.Reply, n.OpenID, n.Name, mention)
		}
		d.sendTracked(b.Config.ID, sendURL, body, n.OpenID, &yzjout.ReplyMeta{
			MsgID: n.MsgID, PersonName: n.Name, GroupType: n.GroupType, Summary: n.Content,
		})
		if dr.Status == "interrupted" {
			break
		}
		extra := d.Jobs.DrainExtra(scope, n.OpenID)
		if extra == "" {
			break
		}
		content = extra
		overrides = map[string]string{}
		_ = parts
	}
}

func (d *Dispatcher) handleMemoryCmd(openID, sub string) string {
	if d.Memory == nil {
		return "记忆服务未启用"
	}
	openID = strings.TrimSpace(openID)
	if openID == "" {
		return "无法识别用户 openID，无法操作记忆"
	}
	switch strings.ToLower(strings.TrimSpace(sub)) {
	case "off":
		if _, err := d.Memory.SetOptOut(openID, true); err != nil {
			return "关闭记忆失败: " + err.Error()
		}
		return "已关闭你的用户记忆（不再注入、不再更新）。发送 /memory on 可重新开启。"
	case "on":
		if _, err := d.Memory.SetOptOut(openID, false); err != nil {
			return "开启记忆失败: " + err.Error()
		}
		return "已开启你的用户记忆。"
	default:
		return d.Memory.FormatShow(openID)
	}
}

func (d *Dispatcher) handleForgetCmd(openID string) string {
	if d.Memory == nil {
		return "记忆服务未启用"
	}
	openID = strings.TrimSpace(openID)
	if openID == "" {
		return "无法识别用户 openID，无法清除记忆"
	}
	reply, _, err := d.Memory.HandleForget(openID)
	if err != nil {
		return "清除记忆失败: " + err.Error()
	}
	return reply
}

// websocketMessageForBot 判断 WSS 推送是否发给当前 bot：
// 平台把账号下所有机器人的消息推给每个连接，消息里的 robotName 是目标机器人；
// 没有 robotName 时无法判断，保持放行（历史行为）。
func websocketMessageForBot(b *bot.Bot, n Normalized) bool {
	if b == nil {
		return false
	}
	targetID := strings.TrimSpace(n.RobotID)
	if targetID != "" && strings.TrimSpace(b.Config.YzjBotOpenID) != "" {
		return strings.EqualFold(targetID, strings.TrimSpace(b.Config.YzjBotOpenID))
	}
	target := strings.TrimSpace(n.RobotName)
	if target == "" {
		return true
	}
	names := append([]string{b.Config.Name}, b.Config.YzjBotNames...)
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" && strings.EqualFold(name, target) {
			return true
		}
	}
	return false
}
