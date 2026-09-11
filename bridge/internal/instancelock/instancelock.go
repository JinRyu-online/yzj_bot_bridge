package instancelock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gofrs/flock"
)

var ErrAlreadyRunning = errors.New("another yzj-bridge instance is already running")

type Lock struct {
	fl      *flock.Flock
	pidPath string
}

func TryLock(path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	fl := flock.New(path)
	ok, err := fl.TryLock()
	if err != nil {
		return nil, err
	}
	if !ok {
		holder := readHolderPID(pidPathFor(path))
		if holder > 0 {
			return nil, fmt.Errorf("%w (pid=%d)", ErrAlreadyRunning, holder)
		}
		return nil, ErrAlreadyRunning
	}
	pidPath := pidPathFor(path)
	pid := os.Getpid()
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		_ = fl.Unlock()
		return nil, err
	}
	return &Lock{fl: fl, pidPath: pidPath}, nil
}

func (l *Lock) Close() error {
	if l == nil || l.fl == nil {
		return nil
	}
	_ = os.Remove(l.pidPath)
	err := l.fl.Unlock()
	l.fl = nil
	return err
}

func pidPathFor(lockPath string) string {
	return lockPath + ".pid"
}

func readHolderPID(path string) int {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return 0
	}
	pid, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return pid
}
