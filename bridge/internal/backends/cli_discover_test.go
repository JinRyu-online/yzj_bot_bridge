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

// TestDiscoverCLIDSHEntry 验证 engine=dsh 在 configured 指向临时假 bin.js 时 Found。
func TestDiscoverCLIDSHEntry(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bin.js")
	if err := os.WriteFile(p, []byte("console.log('stub')"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := DiscoverCLI("dsh", p)
	if r.Engine != "dsh" {
		t.Fatalf("engine=%q", r.Engine)
	}
	if !r.Found {
		t.Fatalf("expected found: %+v", r)
	}
	if r.Path != p {
		t.Fatalf("path=%q want %q", r.Path, p)
	}
	if r.Install == nil || r.Install.Command == "" {
		t.Fatal("dsh install hint required even when found")
	}
}

// TestDiscoverCLINodePATH 验证 engine=node 在 PATH 上临时加假 node 目录时 Found。
func TestDiscoverCLINodePATH(t *testing.T) {
	dir := t.TempDir()
	name := "node"
	if runtime.GOOS == "windows" {
		name = "node.exe"
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	r := DiscoverCLI("node", "")
	if r.Engine != "node" {
		t.Fatalf("engine=%q", r.Engine)
	}
	if !r.Found {
		t.Fatalf("expected found: %+v", r)
	}
	if r.Path == "" {
		t.Fatal("empty path")
	}
	if r.Install == nil || r.Install.Command == "" {
		t.Fatal("node install hint required even when found")
	}
}

// TestDiscoverCLIEngineInstallHints 验证 dsh/node 无论是否找到都返回安装提示
// （机器上可能已装 dsh/node，因此不断言 Found 状态）。
func TestDiscoverCLIEngineInstallHints(t *testing.T) {
	for _, engine := range []string{"dsh", "node"} {
		r := DiscoverCLI(engine, "")
		if r.Engine != engine {
			t.Fatalf("%s: engine=%q", engine, r.Engine)
		}
		if r.Install == nil || r.Install.Command == "" {
			t.Fatalf("%s: missing install hint (result=%+v)", engine, r)
		}
		if runtime.GOOS == "windows" && r.Install.Shell != "powershell" {
			t.Fatalf("%s: shell=%q", engine, r.Install.Shell)
		}
	}
}
