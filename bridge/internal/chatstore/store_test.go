package chatstore

import (
	"path/filepath"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "chat_sessions.json"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return st
}

func TestCreateAssignsIDAndPersists(t *testing.T) {
	st := newTestStore(t)
	sess, err := st.Create("bot-a", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if sess.ID == "" {
		t.Fatal("expected non-empty id")
	}
	if sess.BotID != "bot-a" {
		t.Fatalf("bot_id=%q want bot-a", sess.BotID)
	}
	if len(sess.Messages) != 0 {
		t.Fatalf("expected empty messages, got %d", len(sess.Messages))
	}

	// Reopen from disk to confirm persistence.
	st2, err := Open(st.path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := st2.Get(sess.ID); got == nil || got.BotID != "bot-a" {
		t.Fatalf("after reopen got=%v", got)
	}
}

func TestListOrdersByUpdatedAtDesc(t *testing.T) {
	st := newTestStore(t)
	a, _ := st.Create("bot-a", "A")
	b, _ := st.Create("bot-b", "B")
	// Touch A after B was created so A should sort first.
	_ = st.AppendMessages(a.ID, Message{Role: "user", Content: "hi"})

	list := st.List()
	if len(list) != 2 {
		t.Fatalf("len=%d", len(list))
	}
	if list[0].ID != a.ID {
		t.Fatalf("first=%s want %s", list[0].ID, a.ID)
	}
	if list[1].ID != b.ID {
		t.Fatalf("second=%s want %s", list[1].ID, b.ID)
	}
	// Summaries must omit messages but include count.
	if list[0].MessageCount != 1 {
		t.Fatalf("count=%d want 1", list[0].MessageCount)
	}
}

func TestGetReturnsCopy(t *testing.T) {
	st := newTestStore(t)
	sess, _ := st.Create("bot-a", "title")
	_ = st.AppendMessages(sess.ID, Message{Role: "user", Content: "hello"})
	got := st.Get(sess.ID)
	if got == nil {
		t.Fatal("missing")
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != "hello" {
		t.Fatalf("messages=%v", got.Messages)
	}
	// Mutating the returned copy must not affect the store.
	got.Messages[0].Content = "tampered"
	again := st.Get(sess.ID)
	if again.Messages[0].Content != "hello" {
		t.Fatalf("store was mutated via returned copy: %q", again.Messages[0].Content)
	}
}

func TestUpdateChangesFields(t *testing.T) {
	st := newTestStore(t)
	sess, _ := st.Create("bot-a", "old")
	updated, err := st.Update(sess.ID, "new title", "bot-b")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated == nil {
		t.Fatal("missing")
	}
	if updated.Title != "new title" || updated.BotID != "bot-b" {
		t.Fatalf("updated=%+v", updated)
	}
	// Empty args are ignored.
	updated, _ = st.Update(sess.ID, "", "bot-c")
	if updated.Title != "new title" || updated.BotID != "bot-c" {
		t.Fatalf("ignored-empty update=%+v", updated)
	}
	// Unknown id returns nil.
	if got, _ := st.Update("nope", "x", "y"); got != nil {
		t.Fatalf("expected nil for unknown id, got %+v", got)
	}
}

func TestDeleteRemovesSession(t *testing.T) {
	st := newTestStore(t)
	a, _ := st.Create("bot-a", "A")
	b, _ := st.Create("bot-b", "B")
	if err := st.Delete(a.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := st.Get(a.ID); got != nil {
		t.Fatal("deleted session still present")
	}
	if got := st.Get(b.ID); got == nil {
		t.Fatal("unrelated session was removed")
	}
	// Deleting unknown id is a no-op.
	if err := st.Delete("nope"); err != nil {
		t.Fatalf("delete unknown: %v", err)
	}
}

func TestAppendAutoTitlesFromFirstUserMessage(t *testing.T) {
	st := newTestStore(t)
	sess, _ := st.Create("bot-a", "")
	_ = st.AppendMessages(sess.ID,
		Message{Role: "user", BotID: "bot-a", Content: "  please   summarize   this   long   text  "},
		Message{Role: "assistant", BotID: "bot-a", Content: "ok"},
	)
	got := st.Get(sess.ID)
	if got.Title != "please summarize this long text" {
		t.Fatalf("title=%q", got.Title)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("messages=%d", len(got.Messages))
	}
	if got.Messages[0].BotID != "bot-a" {
		t.Fatalf("user msg bot_id=%q", got.Messages[0].BotID)
	}
}

func TestAppendTruncatesLongTitle(t *testing.T) {
	st := newTestStore(t)
	sess, _ := st.Create("bot-a", "")
	long := strings.Repeat("字", 50)
	_ = st.AppendMessages(sess.ID, Message{Role: "user", Content: long})
	got := st.Get(sess.ID)
	if len([]rune(got.Title)) != 41 { // 40 runes + ellipsis
		t.Fatalf("title rune count=%d: %q", len([]rune(got.Title)), got.Title)
	}
	if !strings.HasSuffix(got.Title, "…") {
		t.Fatalf("title should end with ellipsis: %q", got.Title)
	}
}

func TestAppendFillsTimestamp(t *testing.T) {
	st := newTestStore(t)
	sess, _ := st.Create("bot-a", "")
	_ = st.AppendMessages(sess.ID, Message{Role: "user", Content: "hi"})
	got := st.Get(sess.ID)
	if got.Messages[0].TS == "" {
		t.Fatal("expected auto-filled ts")
	}
}
