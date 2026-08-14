package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeriveWSURL(t *testing.T) {
	got, err := DeriveWSURL("https://www.yunzhijia.com/gateway/robot/webhook/send?yzjtype=0&yzjtoken=abc123")
	if err != nil {
		t.Fatal(err)
	}
	want := "wss://www.yunzhijia.com/xuntong/websocket?yzjtoken=abc123"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestExpandSingleChannel(t *testing.T) {
	f := &File{
		Defaults: map[string]any{"backend": "cursor_cli"},
		Bots: []map[string]any{
			{"id": "fairy", "name": "Fairy", "group": "g1", "send_msg_url": "https://h/x?yzjtoken=t"},
		},
	}
	bots, err := ExpandBots(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(bots) != 1 || bots[0].ID != "fairy" || bots[0].RoleID != "fairy" {
		t.Fatalf("%+v", bots)
	}
}

func TestExpandMultiChannel(t *testing.T) {
	f := &File{
		Bots: []map[string]any{
			{
				"id": "ghost", "name": "Ghost",
				"channels": []any{
					map[string]any{"group": "a", "send_msg_url": "https://h/x?yzjtoken=t1"},
					map[string]any{"group": "b", "send_msg_url": "https://h/x?yzjtoken=t2"},
				},
			},
		},
	}
	bots, err := ExpandBots(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(bots) != 2 || bots[0].ID != "ghost__a" || bots[1].ID != "ghost__b" {
		t.Fatalf("%+v", bots)
	}
}

func TestResolveModelByBackend(t *testing.T) {
	f := &File{
		Defaults: map[string]any{
			"cursor_model": "composer-2",
			"claude_model": "sonnet",
			"openai_model": "gpt-4o",
			"model":        "gpt-4o", // 旧共享字段，不应污染 cursor/claude
		},
		Bots: []map[string]any{
			{"id": "c", "backend": "cursor_cli", "group": "g", "send_msg_url": "https://h/x?yzjtoken=t"},
			{"id": "a", "backend": "claude_code", "group": "g", "send_msg_url": "https://h/x?yzjtoken=t"},
			{"id": "o", "backend": "openai", "group": "g", "send_msg_url": "https://h/x?yzjtoken=t"},
		},
	}
	bots, err := ExpandBots(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(bots) != 3 {
		t.Fatalf("len=%d", len(bots))
	}
	byID := map[string]string{}
	for _, b := range bots {
		byID[b.ID] = b.Model
	}
	if byID["c"] != "composer-2" {
		t.Fatalf("cursor model=%q", byID["c"])
	}
	if byID["a"] != "sonnet" {
		t.Fatalf("claude model=%q", byID["a"])
	}
	if byID["o"] != "gpt-4o" {
		t.Fatalf("openai model=%q", byID["o"])
	}
}

func TestOpenAICompactDefaultsOn(t *testing.T) {
	f := &File{
		Bots: []map[string]any{
			{"id": "o", "backend": "openai", "group": "g", "send_msg_url": "https://h/x?yzjtoken=t"},
		},
	}
	bots, err := ExpandBots(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(bots) != 1 || !bots[0].OpenAICompact {
		t.Fatalf("openai_compact default %+v", bots)
	}
	if bots[0].OpenAICompactKeep != 6 || bots[0].OpenAICompactAfterTurns != 10 || bots[0].OpenAICompactAfterRunes != 8000 {
		t.Fatalf("compact thresholds %+v", bots[0])
	}
	if bots[0].ConversationsDir != "logs/conversations" {
		t.Fatalf("conversations_dir=%q", bots[0].ConversationsDir)
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	if got := ExpandHome("~"); got != home {
		t.Fatalf("ExpandHome(~)=%q want %q", got, home)
	}
	if got := ExpandHome("~/demo"); got != filepath.Join(home, "demo") {
		t.Fatalf("ExpandHome(~/demo)=%q", got)
	}
	if got := ExpandHome(""); got != home {
		t.Fatalf("ExpandHome(\"\")=%q want home", got)
	}
}
