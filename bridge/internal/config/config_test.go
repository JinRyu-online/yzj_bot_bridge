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
			{"id": "c", "backend": "cursor_cli", "group": "g", "send_msg_url": "https://h/x?yzjtoken=tc"},
			{"id": "a", "backend": "claude_code", "group": "g", "send_msg_url": "https://h/x?yzjtoken=ta"},
			{"id": "o", "backend": "openai", "group": "g", "send_msg_url": "https://h/x?yzjtoken=to"},
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

func TestCursorDailyResetDefaultsOn(t *testing.T) {
	f := &File{
		Bots: []map[string]any{
			{"id": "c", "backend": "cursor_cli", "group": "g", "send_msg_url": "https://h/x?yzjtoken=t"},
			{"id": "off", "backend": "cursor_cli", "cursor_daily_reset": false, "group": "g", "send_msg_url": "https://h/x?yzjtoken=u"},
		},
	}
	bots, err := ExpandBots(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(bots) != 2 {
		t.Fatalf("len=%d", len(bots))
	}
	if !bots[0].CursorDailyReset {
		t.Fatalf("default should be true: %+v", bots[0])
	}
	if bots[1].CursorDailyReset {
		t.Fatalf("explicit false: %+v", bots[1])
	}
}

func TestJobQueueAndSharedSession(t *testing.T) {
	f := &File{
		Bots: []map[string]any{
			{"id": "s", "backend": "openai", "session_mode": "shared", "group": "g", "send_msg_url": "https://h/x?yzjtoken=ts"},
			{"id": "p", "backend": "openai", "job_queue": "channel", "group": "g", "send_msg_url": "https://h/x?yzjtoken=tp"},
		},
	}
	bots, err := ExpandBots(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(bots) != 2 {
		t.Fatalf("len=%d", len(bots))
	}
	if bots[0].SessionMode != "shared" {
		t.Fatalf("session=%q", bots[0].SessionMode)
	}
	if bots[1].JobQueue != "channel" {
		t.Fatalf("job_queue=%q", bots[1].JobQueue)
	}
}

func TestDefaultSessionModeShared(t *testing.T) {
	f := &File{
		Bots: []map[string]any{
			{"id": "b", "group": "g", "send_msg_url": "https://h/x?yzjtoken=t"},
		},
	}
	bots, err := ExpandBots(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(bots) != 1 || bots[0].SessionMode != "shared" {
		t.Fatalf("session_mode=%q want shared", bots[0].SessionMode)
	}
}

func TestExpandWorkspaceAutoRoleID(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	want := filepath.Join(home, ".yzj-bridge", "workspace", "fairy")
	f := &File{
		Bots: []map[string]any{
			{"id": "fairy", "backend": "cursor_cli", "group": "g", "send_msg_url": "https://h/x?yzjtoken=t"},
		},
	}
	bots, err := ExpandBots(f)
	if err != nil {
		t.Fatal(err)
	}
	if bots[0].Workspace != want {
		t.Fatalf("workspace=%q want %q", bots[0].Workspace, want)
	}
}

func TestExpandWorkspaceDeprecatedCursor(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	want := filepath.Join(home, ".yzj-bridge", "workspace", "cursor_cli")
	f := &File{
		Defaults: map[string]any{"cursor_workspace": "~/.yzj-bridge/workspace/cursor_cli"},
		Bots: []map[string]any{
			{"id": "fairy", "backend": "cursor_cli", "group": "g", "send_msg_url": "https://h/x?yzjtoken=t"},
		},
	}
	bots, err := ExpandBots(f)
	if err != nil {
		t.Fatal(err)
	}
	if bots[0].Workspace != want {
		t.Fatalf("workspace=%q want %q", bots[0].Workspace, want)
	}
}

func TestExpandWorkspaceExplicitTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	f := &File{
		Bots: []map[string]any{
			{"id": "fairy", "backend": "cursor_cli", "workspace": "~", "group": "g", "send_msg_url": "https://h/x?yzjtoken=t"},
		},
	}
	bots, err := ExpandBots(f)
	if err != nil {
		t.Fatal(err)
	}
	if bots[0].Workspace != home {
		t.Fatalf("workspace=%q want home %q", bots[0].Workspace, home)
	}
}

func TestExpandWorkspaceMultiChannelSameRoleID(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	want := filepath.Join(home, ".yzj-bridge", "workspace", "ghost")
	f := &File{
		Bots: []map[string]any{
			{
				"id": "ghost",
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
	if len(bots) != 2 {
		t.Fatalf("len=%d", len(bots))
	}
	for _, b := range bots {
		if b.Workspace != want {
			t.Fatalf("bot %s workspace=%q want %q", b.ID, b.Workspace, want)
		}
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

func TestExpandPollModeRequiresGroupID(t *testing.T) {
	f := &File{Bots: []map[string]any{{
		"id": "fairy", "name": "Fairy", "group": "g",
		"send_msg_url": "https://h/x?yzjtoken=t", "inbound_mode": "yzj_cli",
	}}}
	if _, err := ExpandBots(f); err == nil {
		t.Fatal("expected yzj_group_id validation error")
	}
}

func TestExpandPollModeFieldsAndChannelOverride(t *testing.T) {
	f := &File{
		Defaults: map[string]any{"yzj_cli": map[string]any{"profile": "work", "image_trigger": "any"}},
		Bots: []map[string]any{{
			"id": "fairy", "name": "Fairy", "inbound_mode": "yzj_cli",
			"yzj_group_id": "G-bot", "yzj_group_name": "bot-group",
			"yzj_bot_names": []any{"Fairy"},
			"channels": []any{
				map[string]any{"group": "a", "send_msg_url": "https://h/a?yzjtoken=t1", "yzj_group_id": "G-a"},
				map[string]any{"group": "b", "send_msg_url": "https://h/b?yzjtoken=t2", "yzj_group_id": "G-b", "yzj_group_name": "b-group"},
			},
		}},
	}
	bots, err := ExpandBots(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(bots) != 2 {
		t.Fatalf("bots = %d", len(bots))
	}
	if bots[0].YzjGroupID != "G-a" || bots[0].YzjGroupName != "bot-group" {
		t.Fatalf("channel a = %+v", bots[0])
	}
	if bots[1].YzjGroupID != "G-b" || bots[1].YzjGroupName != "b-group" {
		t.Fatalf("channel b = %+v", bots[1])
	}
	for _, b := range bots {
		if b.InboundMode != "yzj_cli" || b.YzjProfile != "work" || b.YzjImageTrigger != "any" {
			t.Fatalf("unexpected config: %+v", b)
		}
		if len(b.YzjBotNames) != 1 || b.YzjBotNames[0] != "Fairy" {
			t.Fatalf("bot names = %v", b.YzjBotNames)
		}
	}
}

func TestMergedDefaultsDeepMergesYzjCLI(t *testing.T) {
	f := &File{Defaults: map[string]any{"yzj_cli": map[string]any{"bin": "/custom/yzj-cli"}}}
	got := f.MergedDefaults()
	m, ok := got["yzj_cli"].(map[string]any)
	if !ok {
		t.Fatalf("yzj_cli not a map: %T", got["yzj_cli"])
	}
	if m["bin"] != "/custom/yzj-cli" {
		t.Fatalf("bin = %v", m["bin"])
	}
	if m["poll_interval_sec"] != 5 {
		t.Fatalf("defaults lost in merge: %v", m["poll_interval_sec"])
	}
	if m["version"] != "0.1.8" {
		t.Fatalf("version default lost: %v", m["version"])
	}
}

func TestExpandPollTrigger(t *testing.T) {
	base := map[string]any{
		"id": "fairy", "name": "Fairy", "group": "g",
		"send_msg_url": "https://h/x?yzjtoken=t",
		"inbound_mode": "yzj_cli", "yzj_group_id": "G1",
	}
	f := &File{Bots: []map[string]any{base}}
	bots, err := ExpandBots(f)
	if err != nil {
		t.Fatal(err)
	}
	if bots[0].YzjTrigger != "mention" {
		t.Fatalf("default trigger = %q", bots[0].YzjTrigger)
	}
	base["yzj_trigger"] = "direct"
	base["yzj_bot_open_id"] = "BOT-abc"
	bots, err = ExpandBots(&File{Bots: []map[string]any{base}})
	if err != nil {
		t.Fatal(err)
	}
	if bots[0].YzjTrigger != "direct" || bots[0].YzjBotOpenID != "BOT-abc" {
		t.Fatalf("unexpected: %+v", bots[0])
	}
}
