package sessions

import "yzj-bridge/internal/bot"

const oneshotLockKey = "__oneshot__"

// RunLockKey returns the session segment used for orchestrator run locks.
// Unlike ResolveSessionKey, oneshot mode still yields a stable lock key.
func RunLockKey(cfg bot.Config, openID string) string {
	if IsGUIChatOpenID(openID) {
		return openID
	}
	switch cfg.SessionMode {
	case "shared":
		key := cfg.SharedSessionKey
		if key == "" {
			key = "__shared__"
		}
		return key
	case "oneshot":
		return oneshotLockKey
	default:
		if openID == "" {
			return oneshotLockKey
		}
		return openID
	}
}
