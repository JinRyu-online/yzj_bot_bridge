package backends

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDiscoverCLIUnknown(t *testing.T) {
	r := DiscoverCLI("nope", "")
	if r.Found {
		t.Fatal("expected not found")
	}
	if r.Message == "" {
		t.Fatal("expected message")
	}
}

func TestDiscoverCLIFindsConfiguredAbs(t *testing.T) {
	dir := t.TempDir()
	name := "agent"
	if runtime.GOOS == "windows" {
		name = "agent.exe"
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := DiscoverCLI("cursor", p)
	if !r.Found {
		t.Fatalf("expected found: %+v", r)
	}
	if r.Path == "" {
		t.Fatal("empty path")
	}
	if r.Install == nil || r.Install.Command == "" {
		t.Fatal("install hint required even when found")
	}
}

func TestDiscoverCLIClaudeInstallHint(t *testing.T) {
	r := DiscoverCLI("claude", "")
	if r.Install == nil || r.Install.Command == "" {
		t.Fatal("missing install hint")
	}
	if runtime.GOOS == "windows" && r.Install.Shell != "powershell" {
		t.Fatalf("shell=%q", r.Install.Shell)
	}
}

func TestCursorInstallCandidatesNonEmpty(t *testing.T) {
	if len(cursorInstallCandidates()) == 0 && len(claudeInstallCandidates()) == 0 {
		// home may be empty in some CI; still ok if LOCALAPPDATA set
		if os.Getenv("LOCALAPPDATA") == "" && os.Getenv("HOME") == "" && os.Getenv("USERPROFILE") == "" {
			t.Skip("no home env")
		}
	}
}
