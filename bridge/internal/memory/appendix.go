package memory

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// RenderAppendix builds the runtime memory prompt (hard-capped to appendixRunes).
// Only the memory appendix is clipped — callers must not clip skills together with this.
func RenderAppendix(p *Profile, cfg Config, now time.Time) string {
	if p == nil || p.OptedOut {
		return ""
	}
	max := cfg.AppendixRunes
	if max <= 0 {
		max = 800
	}
	styleMax := cfg.StyleRunes
	if styleMax <= 0 {
		styleMax = 400
	}
	dontsMax := cfg.DontsMax
	if dontsMax <= 0 {
		dontsMax = 8
	}

	var b strings.Builder
	b.WriteString("\n\n【用户记忆】\n")
	startLen := b.Len()
	if name := strings.TrimSpace(p.DisplayName); name != "" {
		fmt.Fprintf(&b, "称呼参考：%s\n", name)
	}
	writeField(&b, "如何称呼", p.HowToAddress)
	writeField(&b, "职责/角色", p.Role)
	writeField(&b, "提问风格", p.AskStyle)
	writeField(&b, "期望回答风格", p.ReplyStyle)
	donts := p.Donts.Effective()
	if len(donts) > dontsMax {
		donts = donts[:dontsMax]
	}
	if len(donts) > 0 {
		fmt.Fprintf(&b, "忌口：%s\n", strings.Join(donts, "；"))
	}
	notes := clipRunes(p.Notes.Effective(), styleMax)
	if notes != "" {
		fmt.Fprintf(&b, "风格说明：%s\n", notes)
	}
	if cfg.FactCardsEnabled {
		for _, c := range activeFactCards(p.FactCards, cfg, now) {
			text := SanitizeFieldValue(c.Text, 120)
			if text == "" {
				continue
			}
			fmt.Fprintf(&b, "事实：%s\n", text)
		}
	}
	if b.Len() == startLen {
		return ""
	}
	out := b.String()
	if utf8.RuneCountInString(out) <= max {
		return out
	}
	// Clip only this appendix.
	r := []rune(out)
	return string(r[:max]) + "…"
}

func writeField(b *strings.Builder, label string, f Field) {
	v := f.Effective()
	if v == "" {
		return
	}
	fmt.Fprintf(b, "%s：%s\n", label, v)
}

func activeFactCards(cards []FactCard, cfg Config, now time.Time) []FactCard {
	max := cfg.FactCardsMax
	if max <= 0 {
		max = 12
	}
	var out []FactCard
	for _, c := range cards {
		if isFactExpired(c, now) {
			continue
		}
		out = append(out, c)
		if len(out) >= max {
			break
		}
	}
	return out
}

func isFactExpired(c FactCard, now time.Time) bool {
	if strings.TrimSpace(c.ExpiresAt) == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, c.ExpiresAt)
	if err != nil {
		return false
	}
	return !t.After(now)
}
