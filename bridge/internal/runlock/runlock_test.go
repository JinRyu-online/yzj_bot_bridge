package runlock_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"yzj-bridge/internal/runlock"
)

func TestAcquireReleaseSerial(t *testing.T) {
	m := runlock.New()
	ctx := context.Background()

	release, err := m.Acquire(ctx, "k1")
	if err != nil {
		t.Fatal(err)
	}
	blocked := make(chan struct{})
	go func() {
		release2, err := m.Acquire(ctx, "k1")
		if err != nil {
			t.Errorf("unexpected acquire error: %v", err)
			close(blocked)
			return
		}
		release2()
		close(blocked)
	}()
	select {
	case <-blocked:
		t.Fatal("second acquire returned before first release")
	case <-time.After(30 * time.Millisecond):
	}
	release()
	select {
	case <-blocked:
	case <-time.After(time.Second):
		t.Fatal("second acquire did not complete after release")
	}
}

func TestAcquireParallelDifferentKeys(t *testing.T) {
	m := runlock.New()
	ctx := context.Background()
	var n atomic.Int32

	done := make(chan struct{}, 2)
	acquire := func(key string) {
		release, err := m.Acquire(ctx, key)
		if err != nil {
			t.Errorf("acquire %s: %v", key, err)
			done <- struct{}{}
			return
		}
		defer release()
		n.Add(1)
		time.Sleep(30 * time.Millisecond)
		n.Add(-1)
		done <- struct{}{}
	}
	go acquire("a")
	go acquire("b")
	<-done
	<-done
	if n.Load() != 0 {
		t.Fatalf("counter=%d", n.Load())
	}
}

func TestAcquireContextCancel(t *testing.T) {
	m := runlock.New()
	release, err := m.Acquire(context.Background(), "busy")
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = m.Acquire(ctx, "busy")
	if err == nil {
		t.Fatal("expected cancel while waiting")
	}
}

func TestNormalizeEngine(t *testing.T) {
	cases := map[string]string{
		"cursor_cli": "cursor", "cursor": "cursor",
		"claude_code": "claude", "claude": "claude",
		"openai": "openai", "OpenAI": "openai",
	}
	for in, want := range cases {
		if got := runlock.NormalizeEngine(in); got != want {
			t.Fatalf("%q => %q want %q", in, got, want)
		}
	}
}

func TestSessionKeyFormat(t *testing.T) {
	got := runlock.SessionKey("cursor", "bot-a", "__shared__")
	if got != "cursor:bot-a:__shared__" {
		t.Fatalf("got %q", got)
	}
}
