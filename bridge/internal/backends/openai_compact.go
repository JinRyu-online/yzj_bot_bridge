package backends

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/sessions"
)

// Defaults tuned for DeepSeek V4 Flash 0731:
// 1M 窗口用不满——网关更窄、Flash 中间遗忘更明显、工具轮次已经很占配额。
// 保留最近 3 轮原文，这样「查单 → 追问推送为何报错」不会丢订单号。
// 摘要单独请求关掉 thinking，避免归档浪费推理 token；system 前缀保持稳定以便缓存命中。
const (
	defaultCompactKeep       = 6
	defaultCompactAfterTurns = 10
	defaultCompactAfterRunes = 8000
	defaultHistoryLoadLimit  = 80
	uncappedHistoryWhenOff   = 40
	summarizeTurnClip        = 2000
	summarizeTimeout         = 20 * time.Second
)

const compactUserLead = "【会话摘要】下面是此前对话的压缩记录，请据此延续；不要当成新问题，也不要更换用户未点名的订单号。\n"
const compactAssistantAck = "已记住摘要，后续按其中的当前订单号和未决问题延续。"

type compactLimits struct {
	Enabled    bool
	Keep       int
	AfterTurns int
	AfterRunes int
}

func resolveCompactLimits(cfg bot.Config) compactLimits {
	lim := compactLimits{
		Enabled:    cfg.OpenAICompact,
		Keep:       cfg.OpenAICompactKeep,
		AfterTurns: cfg.OpenAICompactAfterTurns,
		AfterRunes: cfg.OpenAICompactAfterRunes,
	}
	// Config zero-value: Compact defaults to true in mapToBotConfig.
	if lim.Keep <= 0 {
		lim.Keep = defaultCompactKeep
	}
	if lim.AfterTurns <= 0 {
		lim.AfterTurns = defaultCompactAfterTurns
	}
	if lim.AfterRunes <= 0 {
		lim.AfterRunes = defaultCompactAfterRunes
	}
	return lim
}

func splitForCompact(hist []bot.HistoryTurn, lim compactLimits) (prefix, recent []bot.HistoryTurn, ok bool) {
	if !lim.Enabled || len(hist) <= lim.Keep {
		return nil, hist, false
	}
	runes := runeCountTurns(hist)
	if len(hist) < lim.AfterTurns && runes < lim.AfterRunes {
		return nil, hist, false
	}
	cut := len(hist) - lim.Keep
	return hist[:cut], hist[cut:], true
}

func runeCountTurns(turns []bot.HistoryTurn) int {
	n := 0
	for _, t := range turns {
		n += utf8.RuneCountInString(t.Content)
	}
	return n
}

func turnsHash(turns []bot.HistoryTurn) string {
	h := sha256.New()
	for _, t := range turns {
		_, _ = h.Write([]byte(t.Role))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(t.Content))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func compactPrompt(oldSummary string, prefix []bot.HistoryTurn) string {
	var b strings.Builder
	b.WriteString("【会话归档】把下面多轮对话压成一段短摘要，供后续模型延续上下文。\n")
	b.WriteString("要求：纯文本，不超过 800 字；必须保留当前订单号/任务号、用户未决问题、已确认结论、关键错误原文（短）。\n")
	b.WriteString("没有单号就写「无单号」。不要猜测新订单号，不要写分析过程，不要堆日志。\n")
	if strings.TrimSpace(oldSummary) != "" {
		b.WriteString("\n【已有摘要】\n")
		b.WriteString(strings.TrimSpace(oldSummary))
		b.WriteString("\n")
	}
	b.WriteString("\n【新增对话】\n")
	for _, t := range prefix {
		role := t.Role
		if role != "user" && role != "assistant" {
			continue
		}
		b.WriteString(role)
		b.WriteString(": ")
		b.WriteString(clipRunes(t.Content, summarizeTurnClip))
		b.WriteString("\n")
	}
	return b.String()
}

func (o *OpenAIBackend) resolveHistory(opts bot.RunOpts) []bot.HistoryTurn {
	hist := opts.History
	if len(hist) == 0 {
		if key, ok := sessions.ResolveSessionKey(o.cfg, opts.OperatorOpenID); ok {
			hist = sessions.LoadRecentTurns(o.cfg.ConversationsDir, o.cfg.Group, o.cfg.ID, key, defaultHistoryLoadLimit)
		}
	}
	return normalizeTurns(hist)
}

func normalizeTurns(hist []bot.HistoryTurn) []bot.HistoryTurn {
	out := make([]bot.HistoryTurn, 0, len(hist))
	for _, h := range hist {
		role := strings.ToLower(strings.TrimSpace(h.Role))
		if role != "user" && role != "assistant" {
			continue
		}
		content := strings.TrimSpace(h.Content)
		if content == "" || strings.HasPrefix(content, "(空回复") {
			continue
		}
		out = append(out, bot.HistoryTurn{Role: role, Content: content})
	}
	return out
}

func (o *OpenAIBackend) applyCompact(hist []bot.HistoryTurn, sessionKey, model string) (summary string, recent []bot.HistoryTurn) {
	lim := resolveCompactLimits(o.cfg)
	if !lim.Enabled {
		if len(hist) > uncappedHistoryWhenOff {
			hist = hist[len(hist)-uncappedHistoryWhenOff:]
		}
		return "", hist
	}
	prefix, recent, ok := splitForCompact(hist, lim)
	if !ok {
		return "", hist
	}
	hash := turnsHash(prefix)
	var stored sessions.CompactState
	if o.store != nil && sessionKey != "" {
		stored = o.store.OpenAICompact(o.cfg.ID, sessionKey)
	}
	if stored.Summary != "" && stored.Hash == hash && stored.Count == len(prefix) {
		return stored.Summary, recent
	}
	old := ""
	delta := prefix
	if stored.Summary != "" && stored.Count > 0 && stored.Count < len(prefix) && stored.Hash == turnsHash(prefix[:stored.Count]) {
		old = stored.Summary
		delta = prefix[stored.Count:]
	}
	sum, err := o.summarizePrefix(model, old, delta)
	if err != nil || strings.TrimSpace(sum) == "" {
		log.Printf("openai compact failed bot=%s: %v", o.cfg.ID, err)
		return "", recent
	}
	sum = strings.TrimSpace(sum)
	if o.store != nil && sessionKey != "" {
		o.store.SetOpenAICompact(o.cfg.ID, sessionKey, sessions.CompactState{
			Summary: sum, Count: len(prefix), Hash: hash,
		})
		_ = o.store.Save()
	}
	return sum, recent
}

func (o *OpenAIBackend) summarizePrefix(model, oldSummary string, prefix []bot.HistoryTurn) (string, error) {
	if o.summarize != nil {
		return o.summarize(oldSummary, prefix)
	}
	if len(prefix) == 0 {
		return strings.TrimSpace(oldSummary), nil
	}
	timeout := summarizeTimeout
	if o.cfg.OpenAITimeout > 0 && time.Duration(o.cfg.OpenAITimeout)*time.Second < timeout {
		timeout = time.Duration(o.cfg.OpenAITimeout) * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	msgs := []oaMessage{
		{Role: "system", Content: "你是会话归档器，只输出摘要正文。"},
		{Role: "user", Content: compactPrompt(oldSummary, prefix)},
	}
	req := oaRequest{
		Model:    model,
		Messages: msgs,
		Thinking: &oaThinking{Type: "disabled"},
	}
	msg, err := o.postChat(ctx, req)
	if err != nil {
		req.Thinking = nil
		msg, err = o.postChat(ctx, req)
		if err != nil {
			return "", err
		}
	}
	out := strings.TrimSpace(msg.Content)
	if out == "" {
		out = strings.TrimSpace(msg.ReasoningContent)
	}
	if out == "" {
		return "", fmt.Errorf("empty compact summary")
	}
	return out, nil
}

func appendHistoryMessages(messages []oaMessage, summary string, recent []bot.HistoryTurn) []oaMessage {
	if strings.TrimSpace(summary) != "" {
		messages = append(messages,
			oaMessage{Role: "user", Content: compactUserLead + strings.TrimSpace(summary)},
			oaMessage{Role: "assistant", Content: compactAssistantAck},
		)
	}
	for _, h := range recent {
		messages = append(messages, oaMessage{Role: h.Role, Content: h.Content})
	}
	return messages
}
