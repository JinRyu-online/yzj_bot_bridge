package yzjout

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
	// formatMarkdown 云之家文本消息以 Markdown 渲染（param.formatType=markdown）。
	formatMarkdown = "markdown"
	// yzjContentLimit 云之家机器人单条消息 content 的最大字符数：实测超过 3000 时
	// 返回 HTTP 200 + {"success":false,"errorCode":1401002,"error":"消息内容太长"}。
	yzjContentLimit = 3000
	// chunkLimit 单条分片上限，低于服务端限制，给分页标注留余量。
	chunkLimit = yzjContentLimit - 100
	// maxChunks 一次回复最多分发的消息条数，防止异常超长回复刷屏。
	maxChunks = 9
)

type ReplyMeta struct {
	MsgID      string
	Summary    string
	PersonName string
	GroupType  int
}

// SendText sends content to the yunzhijia robot webhook. Overlong content is
// split into chunks annotated with (1/3)-style page markers; each chunk is
// retried and the send result is logged. Only the last-chunk error (or a
// single-chunk send's error) is returned, but every failure is logged here.
func SendText(sendURL, content, openID string, reply *ReplyMeta) error {
	content = strings.TrimSpace(content)
	if content == "" {
		content = "(空回复)"
	}
	chunks := SplitMessage(content, chunkLimit)
	for i, chunk := range chunks {
		body := chunk
		if len(chunks) > 1 {
			body = fmt.Sprintf("%s\n\n(%d/%d)", chunk, i+1, len(chunks))
		}
		if err := sendOnce(sendURL, body, openID, reply); err != nil {
			log.Printf("bot outbound send FAIL bot-part=%d/%d len=%d: %v", i+1, len(chunks), utf8.RuneCountInString(body), err)
			if i == len(chunks)-1 {
				return err
			}
		} else {
			log.Printf("bot outbound send OK bot-part=%d/%d len=%d", i+1, len(chunks), utf8.RuneCountInString(body))
		}
		time.Sleep(200 * time.Millisecond) // avoid bursting multiple messages into one group
	}
	return nil
}

// sendOnce posts a single message and treats yunzhijia business failures
// (HTTP 200 + success=false, e.g. errorCode 1401002 消息内容太长) as errors.
func sendOnce(sendURL, content, openID string, reply *ReplyMeta) error {
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
			"formatType": formatMarkdown,
		}
	} else {
		payload["param"] = map[string]any{"formatType": formatMarkdown}
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
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("http %d", resp.StatusCode)
			time.Sleep(retryDelay)
			continue
		}
		if resp.StatusCode >= 400 {
			return fmt.Errorf("http %d", resp.StatusCode)
		}
		if bizErr := parseBizError(respBody); bizErr != "" {
			// Business rejection is deterministic; retrying the same payload is pointless.
			return fmt.Errorf("yzj biz error: %s", bizErr)
		}
		return nil
	}
	return lastErr
}

// parseBizError extracts a readable message from a yunzhijia 200-OK failure
// body like {"success":false,"error":"消息内容太长","errorCode":1401002}.
func parseBizError(body []byte) string {
	var r struct {
		Success   bool   `json:"success"`
		Error     string `json:"error"`
		ErrorCode int    `json:"errorCode"`
	}
	if err := json.Unmarshal(body, &r); err != nil || r.Success {
		return ""
	}
	if r.Error == "" {
		r.Error = "unknown error"
	}
	return fmt.Sprintf("%s (errorCode=%d)", r.Error, r.ErrorCode)
}

// SplitMessage splits content into at most maxChunks pieces, each within
// limit runes. It prefers splitting on paragraph boundaries (--- rules,
// blank lines), then line boundaries, then hard rune cuts.
func SplitMessage(content string, limit int) []string {
	content = strings.TrimSpace(content)
	if content == "" {
		return []string{"(空回复)"}
	}
	if limit <= 0 || utf8.RuneCountInString(content) <= limit {
		return []string{content}
	}
	// Split into paragraphs, keeping blank-line separators.
	paras := strings.Split(content, "\n\n")
	var chunks []string
	cur := ""
	needs := func(a, b string) bool { return utf8.RuneCountInString(a) + 2 + utf8.RuneCountInString(b) > limit }
	for _, p := range paras {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		for utf8.RuneCountInString(p) > limit {
			// Paragraph alone exceeds a chunk: split by lines, then hard-cut.
			hard, rest := splitHard(p, limit)
			if cur != "" {
				chunks = append(chunks, cur)
				cur = ""
			}
			chunks = append(chunks, hard)
			p = rest
		}
		if p == "" {
			continue
		}
		if cur == "" {
			cur = p
		} else if needs(cur, p) {
			chunks = append(chunks, cur)
			cur = p
		} else {
			cur += "\n\n" + p
		}
	}
	if cur != "" {
		chunks = append(chunks, cur)
	}
	if len(chunks) <= maxChunks {
		return chunks
	}
	// Too many chunks: merge tail chunks back with hard cuts.
	return reflowToMax(chunks, limit, maxChunks)
}

// splitHard cuts s at the largest line-aligned point within limit; falls back
// to a raw rune cut. Returns the head and the remainder.
func splitHard(s string, limit int) (head, rest string) {
	r := []rune(s)
	if len(r) <= limit {
		return s, ""
	}
	cut := limit
	if nl := strings.LastIndex(string(r[:limit]), "\n"); nl > 0 {
		return strings.TrimSpace(string(r[:nl])), strings.TrimSpace(string(r[nl:]))
	}
	return strings.TrimSpace(string(r[:cut])), strings.TrimSpace(string(r[cut:]))
}

// reflowToMax hard-merges chunks beyond maxChunks into exactly maxChunks
// pieces, each within limit.
func reflowToMax(chunks []string, limit, max int) []string {
	joined := strings.Join(chunks, "\n\n")
	r := []rune(joined)
	size := (len(r) + max - 1) / max
	out := make([]string, 0, max)
	for len(r) > 0 {
		take := size
		if take > len(r) {
			take = len(r)
		}
		h, rest := splitHard(string(r[:take]), limit)
		if h != "" {
			out = append(out, h)
		}
		r = []rune(rest)
	}
	return out
}

// FormatCompletionReply is the IM completion body wrapper.
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
