package yzjcli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveBinaryExplicit(t *testing.T) {
	dir := t.TempDir()
	name := "yzj-cli"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(dir, name)
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveBinary(Config{Bin: bin})
	if err != nil || got != bin {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestResolveBinaryMissingExplicit(t *testing.T) {
	if _, err := ResolveBinary(Config{Bin: filepath.Join(t.TempDir(), "nope")}); err == nil {
		t.Fatal("expected error for missing explicit bin")
	}
}

func TestSanitizeVersion(t *testing.T) {
	cases := map[string]string{
		"0.1.8": "0.1.8", "": "latest", "../x": "..x", "v1.2.3": "v1.2.3",
	}
	for in, want := range cases {
		if got := sanitizeVersion(in); got != want {
			t.Errorf("sanitizeVersion(%q)=%q want %q", in, got, want)
		}
	}
}

func TestManagedBinPathIncludesVersion(t *testing.T) {
	p := managedBinPath("0.1.8")
	if !strings.Contains(p, "yzj-cli-0.1.8") {
		t.Fatalf("unexpected managed path: %s", p)
	}
	if runtime.GOOS == "windows" && !strings.HasSuffix(p, ".exe") {
		t.Fatalf("expected .exe: %s", p)
	}
}

func TestExtractAPICode(t *testing.T) {
	cases := map[string]string{
		"API error: code=10000429, 请求过于频繁": "10000429",
		"code: 40001 bad":                "40001",
		"plain message":                  "",
	}
	for in, want := range cases {
		if got := extractAPICode(in); got != want {
			t.Errorf("extractAPICode(%q)=%q want %q", in, got, want)
		}
	}
}
