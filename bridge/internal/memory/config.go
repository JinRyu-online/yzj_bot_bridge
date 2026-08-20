package memory

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// DefaultConfig returns v1 defaults (memory.enabled=false).
func DefaultConfig() Config {
	return Config{
		Enabled:            false,
		AfterTurns:         8,
		IdleSec:            1800,
		AppendixRunes:      800,
		StyleRunes:         400,
		DontsMax:           8,
		ProfilerMaxTurns:   24,
		ProfilerMaxRunes:   12000,
		FactCardsEnabled:   false,
		FactCardTTLDays:    14,
		FactCardsMax:       12,
		Timeout:            0,
		GUIBindEnabled:     false,
		ForgetPendingSec:   300,
	}
}

// Config is defaults.memory (plus resolved openai fallbacks applied at Service level).
type Config struct {
	Enabled          bool   `json:"enabled"`
	AfterTurns       int    `json:"after_turns"`
	IdleSec          int    `json:"idle_sec"`
	AppendixRunes    int    `json:"appendix_runes"`
	StyleRunes       int    `json:"style_runes"`
	DontsMax         int    `json:"donts_max"`
	ProfilerMaxTurns int    `json:"profiler_max_turns"`
	ProfilerMaxRunes int    `json:"profiler_max_runes"`
	FactCardsEnabled bool   `json:"fact_cards_enabled"`
	FactCardTTLDays  int    `json:"fact_card_ttl_days"`
	FactCardsMax     int    `json:"fact_cards_max"`
	Model            string `json:"model"`
	OpenAIBaseURL    string `json:"openai_base_url"`
	OpenAIAPIKey     string `json:"openai_api_key"`
	Timeout          int    `json:"timeout"`
	ClaudeBin        string `json:"claude_bin"`
	GUIBindEnabled   bool   `json:"gui_bind_enabled"`
	ForgetPendingSec int    `json:"forget_pending_sec"`
}

// ParseConfig reads defaults["memory"] (nested map) and applies defaults.
func ParseConfig(defaults map[string]any) Config {
	cfg := DefaultConfig()
	if defaults == nil {
		return cfg
	}
	raw, ok := defaults["memory"]
	if !ok || raw == nil {
		return cfg
	}
	m, ok := asStringAnyMap(raw)
	if !ok {
		return cfg
	}
	cfg.Enabled = asBool(m["enabled"], false)
	cfg.AfterTurns = asInt(m["after_turns"], 8)
	cfg.IdleSec = asInt(m["idle_sec"], 1800)
	cfg.AppendixRunes = asInt(m["appendix_runes"], 800)
	cfg.StyleRunes = asInt(m["style_runes"], 400)
	cfg.DontsMax = asInt(m["donts_max"], 8)
	cfg.ProfilerMaxTurns = asInt(m["profiler_max_turns"], 24)
	cfg.ProfilerMaxRunes = asInt(m["profiler_max_runes"], 12000)
	cfg.FactCardsEnabled = asBool(m["fact_cards_enabled"], false)
	cfg.FactCardTTLDays = asInt(m["fact_card_ttl_days"], 14)
	cfg.FactCardsMax = asInt(m["fact_cards_max"], 12)
	cfg.Model = asString(m["model"])
	cfg.OpenAIBaseURL = asString(m["openai_base_url"])
	cfg.OpenAIAPIKey = asString(m["openai_api_key"])
	cfg.Timeout = asInt(m["timeout"], 0)
	cfg.ClaudeBin = asString(m["claude_bin"])
	cfg.GUIBindEnabled = asBool(m["gui_bind_enabled"], false)
	cfg.ForgetPendingSec = asInt(m["forget_pending_sec"], 300)
	if cfg.AfterTurns < 1 {
		cfg.AfterTurns = 8
	}
	if cfg.AppendixRunes < 1 {
		cfg.AppendixRunes = 800
	}
	if cfg.DontsMax < 1 {
		cfg.DontsMax = 8
	}
	if cfg.ForgetPendingSec < 1 {
		cfg.ForgetPendingSec = 300
	}
	return cfg
}

// ResolveEngine fills empty openai/claude fields from global defaults.
func (c Config) ResolveEngine(defaults map[string]any) Config {
	out := c
	if out.OpenAIBaseURL == "" {
		out.OpenAIBaseURL = asString(defaults["openai_base_url"])
	}
	if out.OpenAIAPIKey == "" {
		out.OpenAIAPIKey = asString(defaults["openai_api_key"])
	}
	if out.Model == "" {
		out.Model = asString(defaults["openai_model"])
	}
	if out.Timeout <= 0 {
		out.Timeout = asInt(defaults["openai_timeout"], 120)
	}
	if out.ClaudeBin == "" {
		out.ClaudeBin = firstNonEmpty(asString(defaults["claude_bin"]), "claude")
	}
	return out
}

func asStringAnyMap(v any) (map[string]any, bool) {
	switch t := v.(type) {
	case map[string]any:
		return t, true
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[fmt.Sprint(k)] = val
		}
		return out, true
	default:
		return nil, false
	}
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

func asBool(v any, def bool) bool {
	if v == nil {
		return def
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(t, "true") || t == "1"
	default:
		return def
	}
}

func asInt(v any, def int) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	default:
		return def
	}
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func clipRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + "…"
}
