package memory

import (
	"strings"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/sessions"
)

// IsCompletePair reports whether a backend result counts toward memory N.
func IsCompletePair(status, reply string) bool {
	if !strings.EqualFold(strings.TrimSpace(status), "ok") {
		return false
	}
	reply = strings.TrimSpace(reply)
	if reply == "" || strings.HasPrefix(reply, "(空回复") {
		return false
	}
	return true
}

// ResolveMemoryOpenID picks the openID used for memory (profiles/turns).
// GUI chat openIDs are skipped unless gui_bind_enabled and bindOpenID is a real id.
func ResolveMemoryOpenID(cfg Config, openID, bindOpenID string) (string, bool) {
	openID = strings.TrimSpace(openID)
	bindOpenID = strings.TrimSpace(bindOpenID)
	if openID == "" && bindOpenID == "" {
		return "", false
	}
	if sessions.IsGUIChatOpenID(openID) {
		if !cfg.GUIBindEnabled {
			return "", false
		}
		if bindOpenID == "" || sessions.IsGUIChatOpenID(bindOpenID) {
			return "", false
		}
		return bindOpenID, true
	}
	if openID == "" {
		return "", false
	}
	return openID, true
}

// ShouldInject reports whether memory appendix should be attached this turn.
func ShouldInject(cfg Config, botCfg bot.Config, openID, bindOpenID string, optedOut bool) bool {
	if !cfg.Enabled {
		return false
	}
	if !botCfg.MemoryEnabled {
		return false
	}
	if optedOut {
		return false
	}
	_, ok := ResolveMemoryOpenID(cfg, openID, bindOpenID)
	return ok
}

// ShouldRecord reports whether this turn should be written to memory turns.
func ShouldRecord(cfg Config, botCfg bot.Config, openID, bindOpenID string, optedOut bool, status, reply string) bool {
	if !cfg.Enabled {
		return false
	}
	if !botCfg.MemoryEnabled {
		return false
	}
	if optedOut {
		return false
	}
	if _, ok := ResolveMemoryOpenID(cfg, openID, bindOpenID); !ok {
		return false
	}
	return IsCompletePair(status, reply)
}
