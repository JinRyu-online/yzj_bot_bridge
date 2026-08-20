package sessions

import (
	"testing"

	"yzj-bridge/internal/bot"
)

func TestRunLockKeyGUIUsesOpenID(t *testing.T) {
	cfg := bot.Config{SessionMode: "shared", SharedSessionKey: "__shared__"}
	gui := GUIChatOpenID("sess-1")
	if got := RunLockKey(cfg, gui); got != gui {
		t.Fatalf("got %q want %q", got, gui)
	}
}

func TestRunLockKeyShared(t *testing.T) {
	cfg := bot.Config{SessionMode: "shared", SharedSessionKey: "team-a"}
	if got := RunLockKey(cfg, "user-1"); got != "team-a" {
		t.Fatalf("got %q", got)
	}
}

func TestRunLockKeyPerUser(t *testing.T) {
	cfg := bot.Config{SessionMode: "per_user"}
	if got := RunLockKey(cfg, "user-1"); got != "user-1" {
		t.Fatalf("got %q", got)
	}
}

func TestRunLockKeyOneshot(t *testing.T) {
	cfg := bot.Config{SessionMode: "oneshot"}
	if got := RunLockKey(cfg, "user-1"); got != oneshotLockKey {
		t.Fatalf("got %q want %q", got, oneshotLockKey)
	}
	key, ok := ResolveSessionKey(cfg, "user-1")
	if ok || key != "" {
		t.Fatalf("ResolveSessionKey oneshot=%q ok=%v", key, ok)
	}
}
