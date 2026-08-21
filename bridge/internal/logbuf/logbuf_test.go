package logbuf

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMatchBotExactChannel(t *testing.T) {
	b := New(100)
	b.Append("info", "fairy__workAssistant", "bot=fairy__workAssistant ask: a")
	b.Append("info", "fairy__devlopment", "bot=fairy__devlopment ask: b")
	b.Append("info", "fairy", "bot=fairy ask: c")

	got := b.Since(0, "fairy__workAssistant")
	if len(got) != 1 || got[0].Bot != "fairy__workAssistant" {
		t.Fatalf("channel filter leaked: %+v", got)
	}

	gotRole := b.Since(0, "fairy")
	if len(gotRole) != 3 {
		t.Fatalf("role filter should match all fairy*, got %d", len(gotRole))
	}
}

func TestPersistRuntimeJSONL(t *testing.T) {
	dir := t.TempDir()
	b := NewWithDir(10, dir)
	defer b.Close()
	b.Append("INFO", "gui", "hello persist")
	b.Close()

	path := filepath.Join(dir, "runtime-"+time.Now().Format("2006-01-02")+".jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		t.Fatal("expected one jsonl line")
	}
	var line Line
	if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
		t.Fatal(err)
	}
	if line.Level != "INFO" || line.Bot != "gui" || line.Message != "hello persist" {
		t.Fatalf("unexpected line: %+v", line)
	}
	if sc.Scan() {
		t.Fatalf("unexpected extra line: %s", sc.Text())
	}
}

func TestNewDoesNotPersist(t *testing.T) {
	b := New(8)
	if b.PersistDir() != "" {
		t.Fatalf("New() persistDir=%q", b.PersistDir())
	}
}
