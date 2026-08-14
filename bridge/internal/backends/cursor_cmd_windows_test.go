//go:build windows

package backends

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"yzj-bridge/internal/processutil"
)

func TestCursorCommandRunsPS1WithHideWindow(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.txt")
	cmdPath := filepath.Join(dir, "agent.cmd")
	ps1Path := filepath.Join(dir, "agent.ps1")
	if err := os.WriteFile(cmdPath, []byte("@echo should-not-run\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ps1 := "$out = $args[0]\r\nSet-Content -LiteralPath $out -Value ($args[1..($args.Length-1)] -join '|')\r\n"
	if err := os.WriteFile(ps1Path, []byte(ps1), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := cursorCommand(nil, cmdPath, outPath, "create-chat", "say \"hi\" & echo PWNED", "line1\nline2")
	processutil.HideWindow(cmd)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run: %v (%s)", err, out)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.TrimSpace(string(got))
	if !strings.Contains(text, "create-chat") {
		t.Fatalf("missing create-chat: %q", text)
	}
	if !strings.Contains(text, `say "hi" & echo PWNED`) {
		t.Fatalf("& was eaten or executed: %q", text)
	}
	if !strings.Contains(text, "line1\nline2") && !strings.Contains(text, "line1|line2") {
		// join uses | between args; newline stays inside one arg
		t.Fatalf("newline arg lost: %q", text)
	}
	if strings.Contains(text, "PWNED") && !strings.Contains(text, "echo PWNED") {
		t.Fatalf("PWNED executed as a command: %q", text)
	}
}

func TestCursorCommandCmdFallbackCreateNoWindow(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.txt")
	cmdPath := filepath.Join(dir, "only.cmd")
	body := "@echo off\r\n> \"" + outPath + "\" echo %*\r\n"
	if err := os.WriteFile(cmdPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := cursorCommand(nil, cmdPath, "create-chat")
	processutil.HideWindow(cmd)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run: %v (%s)", err, out)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "create-chat") {
		t.Fatalf("output=%q", got)
	}
}

func TestCursorCommandExeNotWrapped(t *testing.T) {
	cmd := cursorCommand(nil, os.Getenv("ComSpec"), "/C", "echo ok")
	if filepath.Base(strings.ToLower(cmd.Path)) == "cmd.exe" && len(cmd.Args) >= 2 && cmd.Args[1] == "/S" {
		t.Fatalf("exe was wrapped: path=%q args=%q", cmd.Path, cmd.Args)
	}
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.CmdLine != "" {
		t.Fatalf("exe should not set CmdLine: %q", cmd.SysProcAttr.CmdLine)
	}
}

func TestHideWindowPreservesCmdLine(t *testing.T) {
	dir := t.TempDir()
	cmdPath := filepath.Join(dir, "only.cmd")
	if err := os.WriteFile(cmdPath, []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := cursorCommand(nil, cmdPath, "models")
	processutil.HideWindow(cmd)
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.CmdLine == "" {
		t.Fatal("CmdLine cleared or missing")
	}
	if cmd.SysProcAttr.CreationFlags&0x08000000 == 0 {
		t.Fatal("CREATE_NO_WINDOW not set")
	}
}

func TestCursorCommandLookPathAgentCmd(t *testing.T) {
	if _, err := exec.LookPath("agent"); err != nil {
		t.Skip("agent not on PATH")
	}
	cmd := cursorCommand(nil, "agent", "models")
	base := strings.ToLower(filepath.Base(cmd.Path))
	if base != "powershell.exe" && base != "cmd.exe" && base != "agent.exe" && base != "cursor-agent.exe" {
		t.Fatalf("unexpected launch path %q", cmd.Path)
	}
}

func TestEnsureCmdDirFixesMissingWorkspaceStart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "workspace", "fairy")
	ps := windowsPowerShellExe()
	missing := exec.Command(ps, "-NoProfile", "-Command", "exit 0")
	processutil.HideWindow(missing)
	missing.Dir = dir
	if err := missing.Start(); err == nil {
		_ = missing.Process.Kill()
		t.Fatal("expected start to fail when workspace is missing")
	} else if !strings.Contains(err.Error(), "The directory name is invalid") && !strings.Contains(strings.ToLower(err.Error()), "directory") {
		t.Fatalf("unexpected missing-dir error: %v", err)
	}

	ok := exec.Command(ps, "-NoProfile", "-Command", "exit 0")
	processutil.HideWindow(ok)
	ensureCmdDir(ok, dir)
	if err := ok.Run(); err != nil {
		t.Fatalf("after ensureCmdDir: %v", err)
	}
}
