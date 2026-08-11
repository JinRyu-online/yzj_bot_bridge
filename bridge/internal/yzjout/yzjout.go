package yzjout

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
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
