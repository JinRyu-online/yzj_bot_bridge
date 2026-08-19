package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yzj-bridge/internal/paths"
)

func TestParseDefaultYAMLTemplate(t *testing.T) {
	data := readDefaultTemplate(t)
	if _, err := ParseYAML(data); err != nil {
		t.Fatalf("default template must parse: %v", err)
	}
	if _, err := ExpandBots(mustParse(t, data)); err != nil {
		t.Fatalf("default template must expand bots: %v", err)
	}
}

func TestDefaultTemplateHasNoTabIndent(t *testing.T) {
	data := readDefaultTemplate(t)
	for i, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "\t") {
			t.Fatalf("line %d uses tab indent (YAML forbids tabs): %q", i+1, line)
		}
	}
}

func TestValidatedDefaultYAMLRejectsInvalid(t *testing.T) {
	bad := []byte("defaults:\n\tbroken: tab\n")
	got, err := ValidatedDefaultYAML(bad)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := ParseYAML(got); err != nil {
		t.Fatalf("fallback must parse: %v", err)
	}
}

func TestBootstrapIfNeededWritesValidConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("USERPROFILE", dir)
	t.Setenv("HOME", dir)
	_ = paths.EnsureUserData()

	valid := readDefaultTemplate(t)
	if err := BootstrapIfNeeded(valid); err != nil {
		t.Fatal(err)
	}
	cfgPath := paths.ConfigPath()
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseYAML(raw); err != nil {
		t.Fatalf("bootstrapped config invalid: %v", err)
	}
}

func TestBootstrapIfNeededRejectsInvalidTemplate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("USERPROFILE", dir)
	t.Setenv("HOME", dir)
	_ = paths.EnsureUserData()

	bad := []byte("defaults:\n\tbroken: tab\n")
	if err := BootstrapIfNeeded(bad); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(paths.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseYAML(raw); err != nil {
		t.Fatalf("should write embedded fallback, got: %v", err)
	}
}

func TestRepairConfigIfInvalidFixesBrokenFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("USERPROFILE", dir)
	t.Setenv("HOME", dir)
	_ = paths.EnsureUserData()

	broken := []byte("defaults:\n\tbroken: tab\n")
	if err := os.WriteFile(paths.ConfigPath(), broken, 0o644); err != nil {
		t.Fatal(err)
	}
	valid := readDefaultTemplate(t)
	if err := RepairConfigIfInvalid(valid); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(paths.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseYAML(raw); err != nil {
		t.Fatalf("repaired config invalid: %v", err)
	}
}

func readDefaultTemplate(t *testing.T) []byte {
	t.Helper()
	candidates := []string{
		filepath.Join("..", "..", "..", "config.default.yaml"),
		filepath.Join("..", "..", "..", "gui", "src-tauri", "binaries", "config.default.yaml"),
	}
	for _, c := range candidates {
		b, err := os.ReadFile(c)
		if err == nil {
			return b
		}
	}
	t.Fatal("config.default.yaml not found")
	return nil
}

func mustParse(t *testing.T, data []byte) *File {
	t.Helper()
	f, err := ParseYAML(data)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
