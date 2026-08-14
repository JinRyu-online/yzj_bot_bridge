package inbound

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/commands"
	"yzj-bridge/internal/dedupe"
	"yzj-bridge/internal/jobs"
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
	Raw       map[string]any
}

type Dispatcher struct {
	Reg    *registry.Registry
	Orch   *orchestrator.Orchestrator
	Dedupe *dedupe.Store
	Jobs   *jobs.Manager
	Store  *sessions.Store
	// SendText, when set, replaces yzjout.SendText (tests).
	SendText func(sendURL, content, openID string, reply *yzjout.ReplyMeta) error
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

func (d *Dispatcher) queueScope(b *bot.Bot, openID string) string {
	return jobs.Scope(b.Config.ID, openID, jobs.UseChannelQueue(b.Config.SessionMode, b.Config.JobQueue))
}

func (d *Dispatcher) Handle(botID string, n Normalized) {
	b := d.Reg.Get(botID)
	if b == nil {
		return
	}
	key := botID + ":" + n.MsgID
	if d.Dedupe.AlreadySeen(key) {
		return
	}
	if !commands.MessageTargetsBot(n.Content, b, d.Reg) {
		return
	}
	if !commands.Allowed(b.Config, n.OpenID, n.Name) {
		_ = d.send(b.Config.SendMsgURL, "你不在白名单中", n.OpenID, &yzjout.ReplyMeta{MsgID: n.MsgID, PersonName: n.Name, GroupType: n.GroupType, Summary: n.Content})
		return
	}
	clean := commands.StripBotMention(n.Content, b, d.Reg.Names(), true)
	cmdRes := commands.Parse(clean, b, d.Reg)
	if cmdRes.Handled && strings.TrimSpace(cmdRes.RestText) == "" {
		body := strings.TrimSpace(cmdRes.Reply)
		body = yzjout.FormatCompletionReply(body, n.OpenID, n.Name, b.Config.MentionOnReply)
		_ = d.send(b.Config.SendMsgURL, body, n.OpenID, &yzjout.ReplyMeta{MsgID: n.MsgID, PersonName: n.Name, GroupType: n.GroupType, Summary: n.Content})
		return
	}
	scope := d.queueScope(b, n.OpenID)
	res := d.Jobs.TryAccept(scope, n.OpenID, n.Name, cmdRes.RestText, queuedJob{Bot: b, N: n, Cmd: cmdRes})
	switch res.Status {
	case jobs.StatusMerged:
		if b.Config.AckPending {
			_ = d.send(b.Config.SendMsgURL, "已合并补充内容，稍后一并处理", n.OpenID, nil)
		}
		return
	case jobs.StatusQueued:
		_ = d.send(b.Config.SendMsgURL, yzjout.FormatQueuePosition(res.Position), n.OpenID, nil)
		return
	}
	d.startJob(b, n, cmdRes, true)
}

func (d *Dispatcher) startJob(b *bot.Bot, n Normalized, cmdRes commands.Result, ack bool) {
	if ack && b.Config.AckPending {
		_ = d.send(b.Config.SendMsgURL, yzjout.FormatPendingAck(), n.OpenID, nil)
	}
	go d.runJob(b, n, cmdRes)
}

func (d *Dispatcher) notifyQueue(b *bot.Bot, notices []jobs.Notice) {
	for _, n := range notices {
		_ = d.send(b.Config.SendMsgURL, yzjout.FormatQueuePosition(n.Position), n.OpenID, nil)
	}
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
	overrides := cmdRes.Overrides
	var parts []string
	if strings.TrimSpace(cmdRes.Reply) != "" {
		parts = append(parts, strings.TrimSpace(cmdRes.Reply))
	}
	for {
		dr := d.Orch.Dispatch(b.Config.ID, content, n.OpenID, n.Name, overrides)
		handler := d.Reg.Get(dr.HandlerBotID)
		sendURL := b.Config.SendMsgURL
		mention := b.Config.MentionOnReply
		if handler != nil {
			sendURL = handler.Config.SendMsgURL
			mention = handler.Config.MentionOnReply
		}
		body := yzjout.FormatCompletionReply(dr.Reply, n.OpenID, n.Name, mention)
		if err := d.send(sendURL, body, n.OpenID, &yzjout.ReplyMeta{
			MsgID: n.MsgID, PersonName: n.Name, GroupType: n.GroupType, Summary: n.Content,
		}); err != nil {
			log.Printf("outbound error bot=%s: %v", b.Config.ID, err)
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
