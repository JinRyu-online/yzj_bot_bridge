package skills

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func writeSample(t *testing.T, dir, id string) {
	t.Helper()
	root := filepath.Join(dir, id)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	md := "---\n" +
		"name: " + id + "\n" +
		"description: test skill\n" +
		"---\n\n# Sample\n\nBody for agent.\n"
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestValidateManifestRejectsBadName(t *testing.T) {
	err := ValidateManifest(&Manifest{Name: "bad id", Description: "x"})
	if err == nil {
		t.Fatal("expected error")
	}
	err = ValidateManifest(&Manifest{Name: "ok", Description: ""})
	if err == nil {
		t.Fatal("expected description required")
	}
}

func TestLoadDirFromSkillMD(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "hello-md")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := "---\nname: hello-md\ndescription: from md\n---\n\n# Hello\n\nDo the thing.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	pkg, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Manifest.ID != "hello-md" || pkg.Manifest.Description != "from md" {
		t.Fatalf("manifest=%+v", pkg.Manifest)
	}
	if !contains(pkg.SkillMD, "Do the thing") || contains(pkg.SkillMD, "---") {
		t.Fatalf("body=%q", pkg.SkillMD)
	}
}

func TestLoadDirRejectsYAMLOnly(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "legacy")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "SKILL.yaml"), []byte("name: legacy\ndescription: old\n"), 0o644)
	if _, err := LoadDir(dir); err == nil {
		t.Fatal("expected error for yaml-only package")
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
	tool, ok := OpenAISkillTool(pkgs)
	if !ok || tool.Name != "skill" {
		t.Fatalf("tool=%+v ok=%v", tool, ok)
	}
	if !contains(tool.Description, "demo") || !contains(tool.Description, "test skill") {
		t.Fatalf("desc=%q", tool.Description)
	}
	hint := OpenAISkillSystemHint()
	if hint == "" || !contains(hint, "skill") {
		t.Fatalf("hint=%q", hint)
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
	if _, err := os.Stat(filepath.Join(ws, ".cursor", "skills", "demo", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if err := Materialize(pkgs, ws, "openai"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws, ".agents", "skills", "demo", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerLoadSkillTool(t *testing.T) {
	tmp := t.TempDir()
	writeSample(t, tmp, "ponly")
	// overwrite description for this test
	dir := filepath.Join(tmp, "ponly")
	_ = os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(`---
name: ponly
description: d
---

# Ponly

Body detail.
`), 0o644)
	store := NewStore(filepath.Join(tmp, "inst"))
	if _, err := store.InstallFromDir(dir); err != nil {
		t.Fatal(err)
	}
	out := (&Runner{}).LoadSkillTool(store, `{"name":"ponly"}`)
	if !contains(out, "Body detail") || !contains(out, "Skill: ponly") {
		t.Fatalf("out=%q", out)
	}
	bad := (&Runner{}).LoadSkillTool(store, `{"name":"missing"}`)
	if !contains(bad, "not found") && !contains(bad, "missing") {
		t.Fatalf("bad=%q", bad)
	}
}

func TestInstallFromMarkdownAndTarGz(t *testing.T) {
	tmp := t.TempDir()
	store := NewStore(filepath.Join(tmp, "installed"))

	mdPath := filepath.Join(tmp, "hello-doc.md")
	if err := os.WriteFile(mdPath, []byte("# Hello\n\nimported md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pkg, err := store.InstallFromMarkdown(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Manifest.ID != "hello-doc" {
		t.Fatalf("md pkg=%+v", pkg.Manifest)
	}
	if DetectInstallSource(mdPath) != "md" {
		t.Fatalf("detect md")
	}

	srcRoot := filepath.Join(tmp, "src")
	writeSample(t, srcRoot, "demo-tgz")
	tgzPath := filepath.Join(tmp, "demo.tgz")
	if err := writeTarGz(tgzPath, filepath.Join(srcRoot, "demo-tgz"), "demo-tgz"); err != nil {
		t.Fatal(err)
	}
	if DetectInstallSource(tgzPath) != "tgz" {
		t.Fatalf("detect tgz")
	}
	pkg2, err := store.InstallFromTarGz(tgzPath)
	if err != nil {
		t.Fatal(err)
	}
	if pkg2.Manifest.ID != "demo-tgz" {
		t.Fatalf("tgz id=%s", pkg2.Manifest.ID)
	}
}

func TestInstallTarGzWithSkillMDAtRoot(t *testing.T) {
	tmp := t.TempDir()
	store := NewStore(filepath.Join(tmp, "installed"))

	// Archive root contains SKILL.md directly (folder name is temp, not "kdlog").
	rootContent := filepath.Join(tmp, "payload")
	if err := os.MkdirAll(rootContent, 0o755); err != nil {
		t.Fatal(err)
	}
	md := "---\nname: kdlog\ndescription: kdlog skill\n---\n\n# Kdlog\n"
	if err := os.WriteFile(filepath.Join(rootContent, "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	tgzPath := filepath.Join(tmp, "kdlog.tgz")
	if err := writeTarGzFlat(tgzPath, rootContent); err != nil {
		t.Fatal(err)
	}
	pkg, err := store.InstallFromTarGz(tgzPath)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Manifest.ID != "kdlog" {
		t.Fatalf("id=%s", pkg.Manifest.ID)
	}
	if _, err := os.Stat(filepath.Join(store.Root, "kdlog", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
}

func writeTarGzFlat(outPath, srcDir string) error {
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		name := filepath.ToSlash(rel)
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = name
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		_, err = io.Copy(tw, in)
		return err
	})
}

func writeTarGz(outPath, srcDir, rootName string) error {
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(filepath.Join(rootName, rel))
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = name
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		_, err = io.Copy(tw, in)
		return err
	})
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
