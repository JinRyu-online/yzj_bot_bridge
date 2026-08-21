package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"
)

// inferredPatch is the JSON shape expected from the profiler model.
type inferredPatch struct {
	HowToAddress string     `json:"how_to_address"`
	Role         string     `json:"role"`
	AskStyle     string     `json:"ask_style"`
	ReplyStyle   string     `json:"reply_style"`
	Donts        []string   `json:"donts"`
	Notes        string     `json:"notes"`
	FactCards    []FactCard `json:"fact_cards,omitempty"`
}

// RunProfiler runs openai-compatible → Claude fallback for one openID.
func (s *Service) RunProfiler(ctx context.Context, openID string) error {
	cfg := s.cfg()
	if !cfg.Enabled {
		return nil
	}
	p, err := s.Store.GetOrCreate(openID)
	if err != nil {
		return err
	}
	if p.OptedOut {
		return nil
	}
	unprofiled, err := s.Store.UnprofiledTurns(openID, p.ProfiledCount)
	if err != nil {
		return err
	}
	if len(unprofiled) == 0 {
		return nil
	}
	window := selectProfilerWindow(unprofiled, cfg)
	if len(window) == 0 {
		return nil
	}
	prompt := buildProfilerPrompt(p, window, cfg)

	engine := cfg.ResolveEngine(s.defaults())
	var patch *inferredPatch
	var lastErr error
	patch, lastErr = s.profileViaOpenAI(ctx, engine, prompt)
	if lastErr != nil || patch == nil {
		patch2, err2 := s.profileViaClaude(ctx, engine, prompt)
		if err2 != nil {
			p.LastError = fmt.Sprintf("openai: %v; claude: %v", lastErr, err2)
			_ = s.Store.Save(p)
			if lastErr != nil {
				return lastErr
			}
			return err2
		}
		patch = patch2
	}
	if patch == nil {
		p.LastError = "empty profiler result"
		_ = s.Store.Save(p)
		return fmt.Errorf("empty profiler result")
	}
	if err := applyInferredPatch(p, patch, cfg, s.now()); err != nil {
		p.LastError = err.Error()
		_ = s.Store.Save(p)
		return err
	}
	// Advance cursor by the number of unprofiled turns we considered (full unprofiled set
	// up to window selection start — we advance by len(window) from the front of unprofiled).
	consumed := len(window)
	// If we truncated from the end of a large unprofiled set, still only advance by window
	// that was sent... Spec: "画像成功后才把游标推到已消费的最后一对".
	// We consume the window we actually profiled (oldest-first within the capped window).
	// When unprofiled was larger than max, selectProfilerWindow keeps the newest N —
	// advancing by len(window) from ProfiledCount would skip older unsent turns.
	// Spec says 增量 ∩（最多 24 对 / 12k）— drop older beyond cap, so cursor jumps over them.
	p.ProfiledCount += len(unprofiled) // all prior unprofiled are considered "handled" (older dropped)
	_ = consumed
	p.LastProfileAt = s.now().UTC().Format(time.RFC3339)
	p.LastError = ""
	return s.Store.Save(p)
}

func selectProfilerWindow(turns []Turn, cfg Config) []Turn {
	maxTurns := cfg.ProfilerMaxTurns
	if maxTurns <= 0 {
		maxTurns = 24
	}
	maxRunes := cfg.ProfilerMaxRunes
	if maxRunes <= 0 {
		maxRunes = 12000
	}
	// Keep newest turns within caps (drop older).
	start := 0
	if len(turns) > maxTurns {
		start = len(turns) - maxTurns
	}
	window := turns[start:]
	for {
		n := 0
		for _, t := range window {
			n += utf8.RuneCountInString(t.User) + utf8.RuneCountInString(t.Assistant)
		}
		if n <= maxRunes || len(window) <= 1 {
			break
		}
		window = window[1:]
	}
	// Drop oversized single turns.
	var out []Turn
	for _, t := range window {
		u := t.User
		a := t.Assistant
		if utf8.RuneCountInString(u) > 2000 {
			u = clipRunes(u, 2000)
		}
		if utf8.RuneCountInString(a) > 2000 {
			a = clipRunes(a, 2000)
		}
		t.User, t.Assistant = u, a
		out = append(out, t)
	}
	return out
}

func buildProfilerPrompt(p *Profile, turns []Turn, cfg Config) string {
	var b strings.Builder
	b.WriteString("你是用户画像提取器。根据增量对话与旧 inferred，输出严格 JSON（不要 Markdown）。\n")
	b.WriteString("字段: how_to_address, role, ask_style, reply_style, donts(数组), notes")
	if cfg.FactCardsEnabled {
		b.WriteString(", fact_cards([{text, expires_at?}])")
	}
	b.WriteString("。\n禁止写入运单号、手机号、token、完整日志墙；不确定则留空字符串/空数组。\n")
	b.WriteString("只描述稳定偏好与风格，不要「当前在办」。\n\n")
	b.WriteString("旧 inferred:\n")
	old, _ := json.Marshal(map[string]any{
		"how_to_address": p.HowToAddress.Inferred,
		"role":           p.Role.Inferred,
		"ask_style":      p.AskStyle.Inferred,
		"reply_style":    p.ReplyStyle.Inferred,
		"donts":          p.Donts.Inferred,
		"notes":          p.Notes.Inferred,
	})
	b.Write(old)
	b.WriteString("\n\n增量对话:\n")
	for i, t := range turns {
		fmt.Fprintf(&b, "[%d] bot=%s group=%s\n用户: %s\n助手: %s\n",
			i+1, t.BotID, t.Group, StripPII(t.User), StripPII(t.Assistant))
	}
	return b.String()
}

func (s *Service) profileViaOpenAI(ctx context.Context, cfg Config, prompt string) (*inferredPatch, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.OpenAIBaseURL), "/")
	key := strings.TrimSpace(cfg.OpenAIAPIKey)
	model := strings.TrimSpace(cfg.Model)
	if base == "" || key == "" || model == "" {
		return nil, fmt.Errorf("openai profiler not configured")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 120
	}
	body := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": "Return only valid JSON object. No tools."},
			{"role": "user", "content": prompt},
		},
		"temperature": 0.2,
		"response_format": map[string]string{
			"type": "json_object",
		},
	}
	raw, _ := json.Marshal(body)
	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, base+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai status %d: %s", resp.StatusCode, clipRunes(string(respBody), 200))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, err
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("openai empty choices")
	}
	return parseInferredJSON(parsed.Choices[0].Message.Content)
}

func (s *Service) profileViaClaude(ctx context.Context, cfg Config, prompt string) (*inferredPatch, error) {
	bin := strings.TrimSpace(cfg.ClaudeBin)
	if bin == "" {
		bin = "claude"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 120
	}
	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	// New session, ask mode, no tools — never resume user chat.
	args := []string{
		"--print",
		"--output-format", "text",
		"--permission-mode", "default",
		"-p", prompt + "\n\n只输出 JSON 对象。",
	}
	cmd := exec.CommandContext(reqCtx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("claude: %w (%s)", err, clipRunes(stderr.String(), 200))
	}
	return parseInferredJSON(stdout.String())
}

func parseInferredJSON(text string) (*inferredPatch, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("empty json")
	}
	// Strip optional markdown fences.
	if strings.HasPrefix(text, "```") {
		text = strings.TrimPrefix(text, "```json")
		text = strings.TrimPrefix(text, "```JSON")
		text = strings.TrimPrefix(text, "```")
		if i := strings.LastIndex(text, "```"); i >= 0 {
			text = text[:i]
		}
		text = strings.TrimSpace(text)
	}
	// Find first { ... last }
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no json object")
	}
	text = text[start : end+1]
	var patch inferredPatch
	if err := json.Unmarshal([]byte(text), &patch); err != nil {
		return nil, fmt.Errorf("bad json: %w", err)
	}
	return &patch, nil
}

func applyInferredPatch(p *Profile, patch *inferredPatch, cfg Config, now time.Time) error {
	if patch == nil {
		return fmt.Errorf("nil patch")
	}
	styleMax := cfg.StyleRunes
	if styleMax <= 0 {
		styleMax = 400
	}
	mergeField(&p.HowToAddress, patch.HowToAddress, 80)
	mergeField(&p.Role, patch.Role, 80)
	mergeField(&p.AskStyle, patch.AskStyle, 120)
	mergeField(&p.ReplyStyle, patch.ReplyStyle, 120)
	mergeField(&p.Notes, patch.Notes, styleMax)
	if !p.Donts.Locked {
		cleaned := SanitizeDonts(patch.Donts, cfg.DontsMax)
		if cleaned != nil || patch.Donts != nil {
			// Only update if sanitize didn't reject everything when input had content
			// that was all PII — empty is allowed.
			p.Donts.Inferred = cleaned
		}
	}
	if cfg.FactCardsEnabled && patch.FactCards != nil {
		p.FactCards = mergeFactCards(p.FactCards, patch.FactCards, cfg, now)
	}
	return nil
}

func mergeField(f *Field, inferred string, maxRunes int) {
	if f.Locked {
		return
	}
	v := SanitizeFieldValue(inferred, maxRunes)
	// Bad/empty after sanitize: keep old inferred (do not clear on empty patch field
	// unless explicitly empty string from model wanting clear — we keep old on empty).
	if v == "" {
		return
	}
	f.Inferred = v
}

func mergeFactCards(old, incoming []FactCard, cfg Config, now time.Time) []FactCard {
	max := cfg.FactCardsMax
	if max <= 0 {
		max = 12
	}
	ttl := cfg.FactCardTTLDays
	if ttl <= 0 {
		ttl = 14
	}
	var out []FactCard
	for _, c := range old {
		if c.Locked || c.Source == "manual" {
			out = append(out, c)
		}
	}
	for _, c := range incoming {
		text := SanitizeFieldValue(c.Text, 120)
		if text == "" {
			continue
		}
		exp := c.ExpiresAt
		if exp == "" {
			exp = now.AddDate(0, 0, ttl).UTC().Format(time.RFC3339)
		}
		out = append(out, FactCard{Text: text, Source: "inferred", ExpiresAt: exp})
	}
	if len(out) > max {
		out = out[:max]
	}
	return out
}
