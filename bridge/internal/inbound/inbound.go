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
	// GroupID is the real Yunzhijia conversation id (yzj-cli poll source only).
	GroupID string
	// MsgType is the platform message type (text|richText|file|other).
	MsgType string
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
	// ImageEnabled turns attachment download/prompt injection on.
	ImageEnabled  bool
	ImageMaxMB    int
	ImageTypes    []string
	ImageKeepDays int
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
	if n, ok := Normalize(raw); ok {
		n.Source = "websocket"
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
	key := n.Source + "|" + botID + ":" + n.MsgID
	if d.Dedupe.AlreadySeen(key) {
		return
	}
	if !n.ForceTarget && !commands.MessageTargetsBot(n.Content, b, d.Reg) {
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

// prepareAttachments downloads message images into the bot workspace and
// returns a prompt block with local paths. It never fails the job: download or
// validation problems become a short note in the returned text.
func (d *Dispatcher) prepareAttachments(ctx context.Context, b *bot.Bot, n Normalized) string {
	if !d.ImageEnabled || d.DownloadAttachment == nil || len(n.Attachments) == 0 {
		return ""
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
	dir := filepath.Join(ws, "incoming_media", time.Now().Format("20060102"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Sprintf("（图片保存目录创建失败：%v）", err)
	}
	safeMsg := safeFileToken(n.MsgID)
	var lines, fails []string
	idx := 0
	for _, a := range n.Attachments {
		if !strings.EqualFold(strings.TrimSpace(a.Kind), "image") {
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
		dctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		err := d.DownloadAttachment(dctx, a.FileID, out)
		cancel()
		if err != nil {
			fails = append(fails, fmt.Sprintf("%s（%v）", displayName(a), err))
			continue
		}
		st, serr := os.Stat(out)
		if serr != nil {
			fails = append(fails, fmt.Sprintf("%s（未落盘）", displayName(a)))
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
		lines = append(lines, fmt.Sprintf("- 图片 %d: %s (%s, %s)", idx, out, mime, humanSize(st.Size())))
	}
	if len(lines) == 0 && len(fails) == 0 {
		return ""
	}
	var bld strings.Builder
	if len(lines) > 0 {
		fmt.Fprintf(&bld, "用户随消息发送了 %d 张图片（本地路径，可直接读取）：\n", len(lines))
		bld.WriteString(strings.Join(lines, "\n"))
		bld.WriteString("\n如需理解图片内容，请使用你的读图/读文件能力打开；若当前模型无法读图，请明确说明并用文字追问。")
	}
	if len(fails) > 0 {
		if bld.Len() > 0 {
			bld.WriteString("\n")
		}
		bld.WriteString("（图片下载失败：" + strings.Join(fails, "；") + "）")
	}
	return bld.String()
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
