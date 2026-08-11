package backends

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafePath(t *testing.T) {
	dir := t.TempDir()
	got, err := SafePath(dir, "a/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "a", "b.txt")
	if got != want {
		t.Fatalf("%s != %s", got, want)
	}
	_, err = SafePath(dir, "../outside")
	if err == nil {
		t.Fatal("expected escape error")
	}
	_ = os.WriteFile(filepath.Join(dir, "f.txt"), []byte("hi"), 0o644)
}
