package backends

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/sessions"
)

func TestCursorDailyResetDropsStaleChatBeforeCreate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	yesterday := sessions.LocalSessionDay(time.Now().AddDate(0, 0, -1))
	raw := fmt.Sprintf(`{"version":3,"bots":{"fairy":{"users":{"u1":{"current":"old-chat","session_day":%q,"name":"n"}}}}}`, yesterday)
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := sessions.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := bot.Config{ID: "fairy", SessionMode: "per_user", CursorDailyReset: true}
	key, ok := sessions.ResolveSessionKey(cfg, "u1")
	if !ok {
		t.Fatal("key")
	}
	c := NewCursor(cfg, st)
	entry := c.store.GetEntry(cfg.ID, key)
	today := sessions.LocalSessionDay(time.Now())
	if !sessions.NeedsDailyReset(c.cfg.CursorDailyReset, entry.SessionDay, today) {
		t.Fatalf("expected reset for %+v", entry)
	}
	c.store.ClearSession(cfg.ID, key)
	got := c.store.GetEntry(cfg.ID, key)
	if got.Current != "" || got.SessionDay != "" {
		t.Fatalf("after clear %+v", got)
	}
	if len(got.History) != 1 || got.History[0].ChatID != "old-chat" {
		t.Fatalf("history %+v", got.History)
	}
}
