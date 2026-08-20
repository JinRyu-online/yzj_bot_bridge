package backends

import (
	"errors"
	"strings"
	"testing"

	"yzj-bridge/internal/bot"
)

func TestAppendCLIPrompt(t *testing.T) {
	got := appendCLIPrompt([]string{"--print", "--trust"}, "- hello")
	want := []string{"--print", "--trust", "--", "- hello"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestExtractCLIPrompt(t *testing.T) {
	args := []string{"--print", "--workspace", "/tmp", "--", "- 上海允屹", "第二段"}
	if got := extractCLIPrompt(args); got != "- 上海允屹 第二段" {
		t.Fatalf("got %q", got)
	}
	if got := extractCLIPrompt([]string{"--print", "no-separator"}); got != "" {
		t.Fatalf("got %q want empty", got)
	}
}

func TestFormatCLINonJSONError(t *testing.T) {
	msg := formatCLINonJSONError([]string{"error: unknown option '- x'"}, errors.New("exit status 1"))
	if !strings.HasPrefix(msg, "(空回复: ") {
		t.Fatalf("got %q", msg)
	}
	if !strings.Contains(msg, "unknown option") {
		t.Fatalf("missing error text: %q", msg)
	}
	if formatCLINonJSONError(nil, nil) != "" {
		t.Fatal("expected empty for no errors")
	}
}

func TestCliReplyOrError(t *testing.T) {
	reply, status := cliReplyOrError("ok", "", nil, nil)
	if reply != "ok" || status != "ok" {
		t.Fatalf("got reply=%q status=%q", reply, status)
	}
	reply, status = cliReplyOrError("", "fallback", nil, nil)
	if reply != "fallback" || status != "ok" {
		t.Fatalf("got reply=%q status=%q", reply, status)
	}
	reply, status = cliReplyOrError("", "", []string{"error: boom"}, nil)
	if status != "cli_error" || !strings.Contains(reply, "boom") {
		t.Fatalf("got reply=%q status=%q", reply, status)
	}
	reply, status = cliReplyOrError("", "", nil, nil)
	if reply != "(空回复)" || status != "empty" {
		t.Fatalf("got reply=%q status=%q", reply, status)
	}
}

func TestCursorDashPrefixedPrompt(t *testing.T) {
	stub := buildSmokeStub(t)
	ws := t.TempDir()
	cfg := bot.Config{
		ID:            "dash-cursor",
		Backend:       "cursor_cli",
		CursorBin:     stub,
		Workspace:     ws,
		CursorTimeout: 20,
		CursorStream:  true,
		CursorForce:   true,
		CursorSandbox: "disabled",
	}
	be, err := Create(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	prompt := "- 上海允屹信息技术有限公司：下单失败 199 单"
	got := be.Run(prompt, bot.RunOpts{Workspace: ws, SessionID: "smoke-session", Mode: "ask"})
	if got.Status != "ok" {
		t.Fatalf("status=%s reply=%s", got.Status, got.Reply)
	}
	want := "echo:" + prompt
	if got.Reply != want {
		t.Fatalf("reply=%q want %q", got.Reply, want)
	}
}

func TestClaudeDashPrefixedPrompt(t *testing.T) {
	stub := buildSmokeStub(t)
	ws := t.TempDir()
	cfg := bot.Config{
		ID:             "dash-claude",
		Backend:        "claude_code",
		ClaudeBin:      stub,
		Workspace:      ws,
		CursorTimeout:  20,
		PermissionMode: "bypassPermissions",
	}
	be, err := Create(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	prompt := "- 列表项开头"
	got := be.Run(prompt, bot.RunOpts{Workspace: ws, Mode: "ask"})
	if got.Status != "ok" {
		t.Fatalf("status=%s reply=%s", got.Status, got.Reply)
	}
	want := "echo:" + prompt
	if got.Reply != want {
		t.Fatalf("reply=%q want %q", got.Reply, want)
	}
}

func TestCursorPlainCLIErrorSurfaces(t *testing.T) {
	stub := buildSmokeStub(t)
	ws := t.TempDir()
	t.Setenv("SMOKE_PLAIN_ERROR", "1")
	cfg := bot.Config{
		ID:            "err-cursor",
		Backend:       "cursor_cli",
		CursorBin:     stub,
		Workspace:     ws,
		CursorTimeout: 20,
		CursorStream:  true,
	}
	be, err := Create(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := be.Run("hello", bot.RunOpts{Workspace: ws, SessionID: "smoke-session", Mode: "ask"})
	if got.Status != "cli_error" {
		t.Fatalf("status=%s reply=%s", got.Status, got.Reply)
	}
	if !strings.HasPrefix(got.Reply, "(空回复: ") {
		t.Fatalf("reply=%q", got.Reply)
	}
	if !strings.Contains(got.Reply, "unknown option") {
		t.Fatalf("reply=%q", got.Reply)
	}
}
