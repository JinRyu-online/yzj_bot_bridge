package backends

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIsWindowsBatch(t *testing.T) {
	cases := []struct {
		bin  string
		want bool
	}{
		{`C:\Users\x\AppData\Local\cursor-agent\agent.cmd`, true},
		{`C:\Users\x\AppData\Local\cursor-agent\agent.CMD`, true},
		{`C:\tools\run.bat`, true},
		{`C:\Users\x\AppData\Local\cursor-agent\agent.exe`, false},
		{"agent", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isWindowsBatch(tc.bin); got != tc.want {
			t.Fatalf("isWindowsBatch(%q)=%v want %v", tc.bin, got, tc.want)
		}
	}
}

func TestPlanCursorLaunchKeepsExe(t *testing.T) {
	bin := `C:\Users\x\AppData\Local\cursor-agent\agent.exe`
	args := []string{"-p", "hello", "--trust"}
	got := planCursorLaunch(bin, args)
	if got.name != bin {
		t.Fatalf("name=%q want %q", got.name, bin)
	}
	if strings.Join(got.args, "\x00") != strings.Join(args, "\x00") {
		t.Fatalf("args=%q want %q", got.args, args)
	}
	if got.cmdLine != "" {
		t.Fatalf("cmdLine should be empty for exe, got %q", got.cmdLine)
	}
}

func TestPlanCursorLaunchPrefersSiblingPS1(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows launch plan")
	}
	dir := t.TempDir()
	cmdPath := filepath.Join(dir, "agent.cmd")
	ps1Path := filepath.Join(dir, "agent.ps1")
	if err := os.WriteFile(cmdPath, []byte("rem unused\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ps1Path, []byte("# unused\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := planCursorLaunch(cmdPath, []string{"-p", "hello\nworld", "--trust"})
	if !strings.EqualFold(filepath.Base(got.name), "powershell.exe") {
		t.Fatalf("name=%q want powershell.exe", got.name)
	}
	if got.cmdLine != "" {
		t.Fatalf("powershell path should not set CmdLine: %q", got.cmdLine)
	}
	joined := strings.Join(got.args, "\x00")
	if !strings.Contains(joined, ps1Path) || !strings.Contains(joined, "hello\nworld") {
		t.Fatalf("args=%q", got.args)
	}
}

func TestPlanCursorLaunchCmdFallback(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows launch plan")
	}
	dir := t.TempDir()
	cmdPath := filepath.Join(dir, "only.cmd")
	if err := os.WriteFile(cmdPath, []byte("@echo off\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := planCursorLaunch(cmdPath, []string{"create-chat"})
	if !strings.EqualFold(filepath.Base(got.name), "cmd.exe") {
		t.Fatalf("name=%q want cmd.exe", got.name)
	}
	if !strings.Contains(got.cmdLine, "/S /C") || !strings.Contains(got.cmdLine, cmdPath) {
		t.Fatalf("cmdLine=%q", got.cmdLine)
	}
	if strings.Contains(got.cmdLine, `\"`) {
		t.Fatalf("cmdLine still has Go-style escapes: %q", got.cmdLine)
	}
}

func TestQuoteCmdArg(t *testing.T) {
	if got := quoteCmdArg(`say "hi"`); got != `"say ""hi"""` {
		t.Fatalf("quotes: got %q", got)
	}
	if got := quoteCmdArg(`use %PATH%`); got != `"use %%PATH%%"` {
		t.Fatalf("percent: got %q", got)
	}
}

func TestResolveCursorLaunchBinLookPath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("LookPath PATHEXT")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "yzj-cursor-probe.cmd")
	if err := os.WriteFile(script, []byte("@echo off\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	got := resolveCursorLaunchBin("yzj-cursor-probe")
	if !strings.EqualFold(got, script) {
		t.Fatalf("got %q want %q", got, script)
	}
}

func TestEnsureCmdDirCreatesMissingWorkspace(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing", "fairy")
	cmd := cursorCommand(nil, "echo", "ok")
	ensureCmdDir(cmd, dir)
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		t.Fatalf("dir not created: %v", err)
	}
	if cmd.Dir != dir {
		t.Fatalf("cmd.Dir=%q want %q", cmd.Dir, dir)
	}
}

func TestEnsureCmdDirEmptyNoop(t *testing.T) {
	cmd := cursorCommand(nil, "echo", "ok")
	ensureCmdDir(cmd, "  ")
	if cmd.Dir != "" {
		t.Fatalf("empty dir should not set cmd.Dir, got %q", cmd.Dir)
	}
}
