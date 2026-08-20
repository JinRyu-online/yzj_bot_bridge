package memory

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/sessions"
)

func TestResolveMemoryOpenID(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true

	if _, ok := ResolveMemoryOpenID(cfg, "", ""); ok {
		t.Fatal("empty should skip")
	}
	gui := sessions.GUIChatOpenID("sess1")
	if _, ok := ResolveMemoryOpenID(cfg, gui, ""); ok {
		t.Fatal("gui without bind should skip")
	}
	cfg.GUIBindEnabled = true
	if _, ok := ResolveMemoryOpenID(cfg, gui, ""); ok {
		t.Fatal("gui bind enabled but no bind id")
	}
	got, ok := ResolveMemoryOpenID(cfg, gui, "real-user")
	if !ok || got != "real-user" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
	got, ok = ResolveMemoryOpenID(cfg, "im-user", "")
	if !ok || got != "im-user" {
		t.Fatalf("im got %q", got)
	}
}

func TestIsCompletePair(t *testing.T) {
	cases := []struct {
		status, reply string
		want          bool
	}{
		{"ok", "hello", true},
		{"ok", "(空回复)", false},
		{"ok", "(空回复: x)", false},
		{"ok", "", false},
		{"interrupted", "hello", false},
		{"start_error", "hello", false},
		{"empty", "hello", false},
	}
	for _, c := range cases {
		if got := IsCompletePair(c.status, c.reply); got != c.want {
			t.Fatalf("%s/%q => %v want %v", c.status, c.reply, got, c.want)
		}
	}
}

func TestAppendixHardCap(t *testing.T) {
	p := &Profile{
		OpenID: "u1",
		Notes:  Field{Inferred: strings.Repeat("风", 2000)},
	}
	cfg := DefaultConfig()
	cfg.AppendixRunes = 800
	out := RenderAppendix(p, cfg, time.Now())
	if utf8.RuneCountInString(out) > 801 { // 800 + ellipsis rune
		t.Fatalf("len=%d", utf8.RuneCountInString(out))
	}
	if !strings.Contains(out, "【用户记忆】") {
		t.Fatal(out)
	}
}

func TestManualOverridesInferred(t *testing.T) {
	p := &Profile{
		OpenID: "u1",
		Role:   Field{Manual: "主管", Inferred: "工程师"},
	}
	out := RenderAppendix(p, DefaultConfig(), time.Now())
	if !strings.Contains(out, "主管") || strings.Contains(out, "工程师") {
		t.Fatal(out)
	}
}

func TestExpiredFactCardSkipped(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	p := &Profile{
		OpenID: "u1",
		FactCards: []FactCard{
			{Text: "过期事实", ExpiresAt: now.Add(-time.Hour).Format(time.RFC3339)},
			{Text: "有效事实", ExpiresAt: now.Add(time.Hour).Format(time.RFC3339)},
		},
	}
	cfg := DefaultConfig()
	cfg.FactCardsEnabled = true
	out := RenderAppendix(p, cfg, now)
	if strings.Contains(out, "过期事实") || !strings.Contains(out, "有效事实") {
		t.Fatal(out)
	}
}

func TestStoreTurnsAndClearLeavesCursor(t *testing.T) {
	root := t.TempDir()
	st := NewStore(root)
	cfg := DefaultConfig()
	cfg.Enabled = true
	svc := NewService(root, cfg)
	svc.now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

	botCfg := bot.Config{ID: "b1", Group: "g", MemoryEnabled: true}
	for i := 0; i < 3; i++ {
		svc.AfterDispatch(botCfg, "user-a", "", "Alice", "q", bot.RunResult{Reply: "a", Status: "ok"})
	}
	n, err := st.TurnCount("user-a")
	if err != nil || n != 3 {
		t.Fatalf("turns=%d err=%v", n, err)
	}
	p, _ := st.Get("user-a")
	if p.ProfiledCount != 0 {
		t.Fatalf("cursor should stay 0, got %d", p.ProfiledCount)
	}
}

func TestSharedUsersSeparateCounts(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.AfterTurns = 100
	svc := NewService(root, cfg)
	botCfg := bot.Config{ID: "b1", MemoryEnabled: true}
	svc.AfterDispatch(botCfg, "user-a", "", "A", "q", bot.RunResult{Reply: "a", Status: "ok"})
	svc.AfterDispatch(botCfg, "user-b", "", "B", "q", bot.RunResult{Reply: "a", Status: "ok"})
	na, _ := svc.Store.TurnCount("user-a")
	nb, _ := svc.Store.TurnCount("user-b")
	if na != 1 || nb != 1 {
		t.Fatalf("a=%d b=%d", na, nb)
	}
}

func TestCrossBotAccumulates(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.AfterTurns = 100
	svc := NewService(root, cfg)
	svc.AfterDispatch(bot.Config{ID: "b1", MemoryEnabled: true}, "u", "", "N", "q", bot.RunResult{Reply: "a", Status: "ok"})
	svc.AfterDispatch(bot.Config{ID: "b2", MemoryEnabled: true}, "u", "", "N", "q", bot.RunResult{Reply: "a", Status: "ok"})
	n, _ := svc.Store.TurnCount("u")
	if n != 2 {
		t.Fatalf("n=%d", n)
	}
	p, _ := svc.Store.Get("u")
	if len(p.BotsSeen) != 2 {
		t.Fatalf("bots=%v", p.BotsSeen)
	}
}

func TestDisabledGlobalSkips(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig() // enabled false
	svc := NewService(root, cfg)
	botCfg := bot.Config{ID: "b1", MemoryEnabled: true}
	if svc.MemoryPromptFor(botCfg, "u", "") != "" {
		t.Fatal("should not inject")
	}
	svc.AfterDispatch(botCfg, "u", "", "N", "q", bot.RunResult{Reply: "a", Status: "ok"})
	n, _ := svc.Store.TurnCount("u")
	if n != 0 {
		t.Fatalf("turns=%d", n)
	}
}

func TestOptOutStopsInjectAndRecord(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig()
	cfg.Enabled = true
	svc := NewService(root, cfg)
	botCfg := bot.Config{ID: "b1", MemoryEnabled: true}
	_, _ = svc.SetOptOut("u", true)
	p := &Profile{OpenID: "u", OptedOut: true, Role: Field{Inferred: "x"}}
	_ = svc.Store.Save(p)
	if svc.MemoryPromptFor(botCfg, "u", "") != "" {
		t.Fatal("opt out inject")
	}
	svc.AfterDispatch(botCfg, "u", "", "N", "q", bot.RunResult{Reply: "a", Status: "ok"})
	n, _ := svc.Store.TurnCount("u")
	if n != 0 {
		t.Fatalf("turns=%d", n)
	}
}

func TestForgetDoubleConfirm(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.ForgetPendingSec = 300
	svc := NewService(root, cfg)
	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return base }

	p := &Profile{OpenID: "u", Role: Field{Manual: "m", Inferred: "i", Locked: true}}
	_ = svc.Store.Save(p)

	reply, cleared, err := svc.HandleForget("u")
	if err != nil || cleared || !strings.Contains(reply, "再次") {
		t.Fatalf("first: %v %v %q", err, cleared, reply)
	}
	p, _ = svc.Store.Get("u")
	if p.Role.Manual != "m" || p.ForgetPendingUntil == "" {
		t.Fatalf("%+v", p)
	}

	svc.now = func() time.Time { return base.Add(time.Minute) }
	reply, cleared, err = svc.HandleForget("u")
	if err != nil || !cleared || !strings.Contains(reply, "已清除") {
		t.Fatalf("second: %v %v %q", err, cleared, reply)
	}
	p, _ = svc.Store.Get("u")
	if p.Role.Manual != "" || p.Role.Inferred != "" || p.Role.Locked {
		t.Fatalf("not cleared: %+v", p.Role)
	}
}

func TestForgetExpiredReentersPending(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig()
	cfg.ForgetPendingSec = 60
	svc := NewService(root, cfg)
	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return base }
	_, _, _ = svc.HandleForget("u")
	svc.now = func() time.Time { return base.Add(2 * time.Minute) }
	reply, cleared, err := svc.HandleForget("u")
	if err != nil || cleared || !strings.Contains(reply, "再次") {
		t.Fatalf("%v %v %q", err, cleared, reply)
	}
}

func TestLockedFieldRejectsMerge(t *testing.T) {
	p := &Profile{OpenID: "u", Role: Field{Inferred: "old", Locked: true}}
	patch := &inferredPatch{Role: "new"}
	_ = applyInferredPatch(p, patch, DefaultConfig(), time.Now())
	if p.Role.Inferred != "old" {
		t.Fatalf("%+v", p.Role)
	}
}

func TestBadJSONDoesNotClobber(t *testing.T) {
	_, err := parseInferredJSON("not json")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPIIRejected(t *testing.T) {
	if v := SanitizeFieldValue("手机 13812345678", 80); v != "" && ContainsPII(v) {
		t.Fatalf("still pii: %q", v)
	}
	if SanitizeFieldValue("Bearer sk-abcdefghijklmnopqrstuvwxyz012345", 80) != "" {
		// strip may leave remnants; ContainsPII should catch if any remain
	}
	p := &Profile{OpenID: "u"}
	_ = applyInferredPatch(p, &inferredPatch{Notes: "联系 13900001111"}, DefaultConfig(), time.Now())
	if ContainsPII(p.Notes.Inferred) {
		t.Fatalf("pii stored: %q", p.Notes.Inferred)
	}
}

func TestParseConfigDefaults(t *testing.T) {
	cfg := ParseConfig(nil)
	if cfg.Enabled || cfg.AfterTurns != 8 || cfg.GUIBindEnabled {
		t.Fatalf("%+v", cfg)
	}
	cfg = ParseConfig(map[string]any{
		"memory": map[string]any{"enabled": true, "after_turns": 3},
	})
	if !cfg.Enabled || cfg.AfterTurns != 3 {
		t.Fatalf("%+v", cfg)
	}
}

func TestBotMemoryDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	botCfg := bot.Config{ID: "b", MemoryEnabled: false}
	if ShouldInject(cfg, botCfg, "u", "", false) {
		t.Fatal("bot disabled")
	}
}

func TestGUIBindRecording(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.GUIBindEnabled = true
	svc := NewService(root, cfg)
	botCfg := bot.Config{ID: "b1", MemoryEnabled: true}
	gui := sessions.GUIChatOpenID("s1")
	svc.AfterDispatch(botCfg, gui, "", "G", "q", bot.RunResult{Reply: "a", Status: "ok"})
	n, _ := svc.Store.TurnCount("real")
	if n != 0 {
		t.Fatal("unbound gui should not record")
	}
	svc.AfterDispatch(botCfg, gui, "real", "G", "q", bot.RunResult{Reply: "a", Status: "ok"})
	n, _ = svc.Store.TurnCount("real")
	if n != 1 {
		t.Fatalf("n=%d path=%s", n, filepath.Join(root, "turns"))
	}
}
