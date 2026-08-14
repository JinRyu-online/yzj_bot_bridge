package sessions

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadRecentTurnsReadsTodayAndYesterday(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 14, 18, 9, 0, 0, time.Local)
	yest := now.AddDate(0, 0, -1).Format("2006-01-02")
	today := now.Format("2006-01-02")
	group, botID, uid := "operate_group", "bsame_city_log", "user1"
	writeJSONL(t, filepath.Join(dir, ConversationFileName(yest, group, botID, uid)),
		`{"role":"user","content":"查询301027582"}`,
		`{"role":"assistant","content":"旧单结论"}`,
	)
	writeJSONL(t, filepath.Join(dir, ConversationFileName(today, group, botID, uid)),
		`{"role":"user","content":"查询301038245"}`,
		`{"role":"assistant","content":"推送失败5次"}`,
	)
	got := LoadRecentTurnsAt(dir, group, botID, uid, 80, now)
	if len(got) != 4 {
		t.Fatalf("len=%d %+v", len(got), got)
	}
	if got[2].Content != "查询301038245" || got[3].Role != "assistant" {
		t.Fatalf("today turns missing: %+v", got)
	}
}

func TestLoadRecentTurnsSkipsEmptyAndKeepsTail(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.Local)
	today := now.Format("2006-01-02")
	path := filepath.Join(dir, ConversationFileName(today, "g", "b", "u"))
	writeJSONL(t, path,
		`{"role":"user","content":"one"}`,
		`{"role":"assistant","content":""}`,
		`{"role":"assistant","content":"(空回复: x)"}`,
		`{"role":"user","content":"two"}`,
		`{"role":"assistant","content":"ok"}`,
		`{"role":"system","content":"nope"}`,
	)
	got := LoadRecentTurnsAt(dir, "g", "b", "u", 2, now)
	if len(got) != 2 {
		t.Fatalf("len=%d %+v", len(got), got)
	}
	if got[0].Content != "two" || got[1].Content != "ok" {
		t.Fatalf("want last two real turns, got %+v", got)
	}
}

func writeJSONL(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, line := range lines {
		if _, err := f.WriteString(line + "\n"); err != nil {
			t.Fatal(err)
		}
	}
}
