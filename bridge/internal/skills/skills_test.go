package skills

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeSample(t *testing.T, dir, id string) {
	t.Helper()
	root := filepath.Join(dir, id)
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := "id: " + id + "\nname: Sample\nversion: \"1.0.0\"\ndescription: test skill\n" +
		"entry:\n  type: shell\n  command: " + shellEchoCmd() + "\n  args: []\n  timeout_sec: 10\n" +
		"tools:\n  - name: echo\n    description: echo\n    parameters:\n      type: object\n      properties: {}\n"
	if err := os.WriteFile(filepath.Join(root, "SKILL.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("# Sample\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func shellEchoCmd() string {
	if runtime.GOOS == "windows" {
		return "cmd"
	}
	return "echo"
}

func TestValidateManifestRejectsBadID(t *testing.T) {
	err := ValidateManifest(&Manifest{ID: "", Name: "x", Entry: Entry{Type: "prompt_only"}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadDirAndStore(t *testing.T) {
	tmp := t.TempDir()
	srcRoot := filepath.Join(tmp, "src")
	writeSample(t, srcRoot, "demo")
	store := NewStore(filepath.Join(tmp, "installed"))
	pkg, err := store.InstallFromDir(filepath.Join(srcRoot, "demo"))
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Manifest.ID != "demo" {
		t.Fatalf("id=%s", pkg.Manifest.ID)
	}
	list, err := store.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v", list, err)
	}
	if err := store.ValidateIDs([]string{"demo"}); err != nil {
		t.Fatal(err)
	}
	if err := store.ValidateIDs([]string{"missing"}); err == nil {
		t.Fatal("expected missing skill error")
	}
	if err := store.Uninstall("demo"); err != nil {
		t.Fatal(err)
	}
}

func TestAdapterToolsAndMaterialize(t *testing.T) {
	tmp := t.TempDir()
	writeSample(t, tmp, "demo")
	store := NewStore(filepath.Join(tmp, "inst"))
	if _, err := store.InstallFromDir(filepath.Join(tmp, "demo")); err != nil {
		t.Fatal(err)
	}
	pkgs, err := Resolve(store, []string{"demo"})
	if err != nil {
		t.Fatal(err)
	}
	tools := OpenAITools(pkgs)
	if len(tools) != 1 || tools[0].Name != "skill_demo__echo" {
		t.Fatalf("tools=%+v", tools)
	}
	appendix := PromptAppendix(pkgs)
	if appendix == "" || !contains(appendix, "demo") {
		t.Fatalf("appendix=%q", appendix)
	}
	ws := filepath.Join(tmp, "ws")
	_ = os.MkdirAll(ws, 0o755)
	if err := Materialize(pkgs, ws, "cursor_cli"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws, ".cursor", "skills", "demo", "SKILL.yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerPromptOnly(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "ponly")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "SKILL.yaml"), []byte(`
id: ponly
name: P
version: "1"
description: d
entry:
  type: prompt_only
tools:
  - name: invoke
    description: i
    parameters: {type: object, properties: {}}
`), 0o644)
	pkg, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := (&Runner{}).Exec(context.Background(), pkg, "invoke", `{"x":1}`, tmp)
	if !contains(out, "d") {
		t.Fatalf("out=%q", out)
	}
}

func TestParseToolExportName(t *testing.T) {
	id, tool, ok := ParseToolExportName("skill_hello-workspace__hello_echo")
	if !ok || id != "hello-workspace" || tool != "hello_echo" {
		t.Fatalf("%s %s %v", id, tool, ok)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		})())
}
