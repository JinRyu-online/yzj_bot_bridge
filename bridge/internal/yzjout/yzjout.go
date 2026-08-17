package yzjout

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"yzj-bridge/internal/jobs"
)

const (
	groupTypeNoNotify = 3
	maxAttempts       = 3
	retryDelay        = 800 * time.Millisecond
)

type ReplyMeta struct {
	MsgID      string
	Summary    string
	PersonName string
	GroupType  int
}

func SendText(sendURL, content, openID string, reply *ReplyMeta) error {
	content = strings.TrimSpace(content)
	if content == "" {
		content = "(空回复)"
	}
	if len([]rune(content)) > 8000 {
		r := []rune(content)
		content = string(r[:7900]) + "\n\n…(内容过长已截断)"
	}
	payload := map[string]any{"msgtype": 2, "content": content}
	if openID != "" && (reply == nil || reply.GroupType != groupTypeNoNotify) {
		payload["notifyParams"] = []map[string]any{
			{"type": "openIds", "values": []string{openID}},
		}
	}
	if reply != nil && reply.MsgID != "" {
		summary := reply.Summary
		if len([]rune(summary)) > 200 {
			summary = string([]rune(summary)[:200])
		}
		payload["paramType"] = 3
		payload["param"] = map[string]any{
			"replyMsgId": reply.MsgID, "replyTitle": "", "isReference": true,
			"replySummary": summary, "replyPersonName": reply.PersonName,
		}
	}
	body, _ := json.Marshal(payload)
	var lastErr error
	for i := 0; i < maxAttempts; i++ {
		req, err := http.NewRequest(http.MethodPost, sendURL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(retryDelay)
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("http %d", resp.StatusCode)
			time.Sleep(retryDelay)
			continue
		}
		if resp.StatusCode >= 400 {
			return fmt.Errorf("http %d", resp.StatusCode)
		}
		return nil
	}
	return lastErr
}

func FormatCompletionReply(body, openID, name string, mention bool) string {
	body = strings.TrimSpace(body)
	body = strings.TrimPrefix(body, "@"+name)
	body = strings.TrimSpace(body)
	body = strings.TrimPrefix(body, "任务已完成")
	body = strings.TrimSpace(body)
	if !mention {
		return body
	}
	if openID != "" {
		return "任务已完成\n\n" + body
	}
	if name != "" {
		return "@" + name + " 任务已完成\n\n" + body
	}
	return body
}

func FormatPendingAck() string {
	return "正在处理中…"
}

// FormatQueuePosition tells a waiting user where they stand (1-based).
func FormatQueuePosition(position int) string {
	if position < 1 {
		position = 1
	}
	return fmt.Sprintf("正在处理之前的问题，你的问题排在第%d位", position)
}

// FormatStopAck is the immediate reply to --stop / --abort / --interrupt.
func FormatStopAck(cancelled bool) string {
	if cancelled {
		return "已中断当前任务"
	}
	return "当前没有正在执行的任务"
}

// FormatJobList is the reply to --jobs / --queue / /任务.
func FormatJobList(snap jobs.Snapshot) string {
	if snap.Current.OpenID == "" {
		return "当前没有正在执行或排队的任务"
	}
	var b strings.Builder
	b.WriteString("当前任务\n")
	b.WriteString("执行中：")
	b.WriteString(jobWho(snap.Current.Name, snap.Current.OpenID))
	b.WriteString("\n内容：")
	b.WriteString(clipJobText(snap.Current.Content, 80))
	if extra := strings.TrimSpace(snap.Extra); extra != "" {
		b.WriteString("\n补充：")
		b.WriteString(clipJobText(extra, 80))
	}
	b.WriteString("\n\n待执行")
	if len(snap.Queue) == 0 {
		b.WriteString("：无")
		return b.String()
	}
	b.WriteString(fmt.Sprintf("（%d）\n", len(snap.Queue)))
	for i, it := range snap.Queue {
		b.WriteString(fmt.Sprintf("%d. %s：%s", i+1, jobWho(it.Name, it.OpenID), clipJobText(it.Content, 80)))
		if i+1 < len(snap.Queue) {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func jobWho(name, openID string) string {
	if s := strings.TrimSpace(name); s != "" {
		return s
	}
	if s := strings.TrimSpace(openID); s != "" {
		return s
	}
	return "未知用户"
}

func clipJobText(s string, max int) string {
	s = strings.Join(strings.Fields(strings.ReplaceAll(s, "\r", "\n")), " ")
	if s == "" {
		return "（无内容）"
	}
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + "…"
}

// FormatInterruptReply is the job-finished message after the engine was aborted.
func FormatInterruptReply(body, openID, name string, mention bool) string {
	body = strings.TrimSpace(body)
	if body == "" {
		body = "任务已中断"
	}
	if !mention {
		return body
	}
	if openID != "" {
		return body
	}
	if name != "" {
		return "@" + name + " " + body
	}
	return body
}
