package instancelock_test

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"yzj-bridge/internal/instancelock"
)

func TestTryLockExclusive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "instance.lock")

	lock, err := instancelock.TryLock(path)
	if err != nil {
		t.Fatalf("first TryLock: %v", err)
	}
	defer lock.Close()

	_, err = instancelock.TryLock(path)
	if !errors.Is(err, instancelock.ErrAlreadyRunning) {
		t.Fatalf("second TryLock want ErrAlreadyRunning, got %v", err)
	}

	raw, err := os.ReadFile(path + ".pid")
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	got, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || got != os.Getpid() {
		t.Fatalf("pid file want %d, got %q err=%v", os.Getpid(), raw, err)
	}
}

func TestTryLockAfterClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "instance.lock")

	lock, err := instancelock.TryLock(path)
	if err != nil {
		t.Fatalf("first TryLock: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	lock2, err := instancelock.TryLock(path)
	if err != nil {
		t.Fatalf("TryLock after Close: %v", err)
	}
	defer lock2.Close()
}
