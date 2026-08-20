package orchestrator

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/registry"
	"yzj-bridge/internal/runlock"
)

type countingBackend struct {
	mu     sync.Mutex
	active int
	max    int
	hold   chan struct{}
}

func (c *countingBackend) Run(string, bot.RunOpts) bot.RunResult {
	c.mu.Lock()
	c.active++
	if c.active > c.max {
		c.max = c.active
	}
	hold := c.hold
	c.mu.Unlock()
	if hold != nil {
		<-hold
	} else {
		time.Sleep(40 * time.Millisecond)
	}
	c.mu.Lock()
	c.active--
	c.mu.Unlock()
	return bot.RunResult{Reply: "ok", Status: "ok"}
}

func (c *countingBackend) CreateSession() (string, error) { return "s", nil }
func (c *countingBackend) ClearSession(string) (string, error) { return "", nil }

func TestDispatchParallelDifferentSessionKeys(t *testing.T) {
	be := &countingBackend{}
	reg := registry.New()
	reg.Replace([]*bot.Bot{{Config: bot.Config{ID: "b1", Backend: "openai", SessionMode: "per_user"}, Backend: be}})
	orch := &Orchestrator{Reg: reg, Locks: runlock.New()}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		res := orch.DispatchWithContext(context.Background(), "b1", "a", "user-a", "A", nil)
		if res.Status != "ok" {
			t.Errorf("user-a status=%q", res.Status)
		}
	}()
	go func() {
		defer wg.Done()
		res := orch.DispatchWithContext(context.Background(), "b1", "b", "user-b", "B", nil)
		if res.Status != "ok" {
			t.Errorf("user-b status=%q", res.Status)
		}
	}()
	wg.Wait()
	if be.max <= 1 {
		t.Fatalf("max concurrent=%d want parallel different keys", be.max)
	}
}

func TestDispatchSerialSameSessionKey(t *testing.T) {
	be := &countingBackend{}
	reg := registry.New()
	reg.Replace([]*bot.Bot{{
		Config: bot.Config{ID: "b1", Backend: "openai", SessionMode: "shared", SharedSessionKey: "__shared__"},
		Backend: be,
	}})
	orch := &Orchestrator{Reg: reg, Locks: runlock.New()}

	var wg sync.WaitGroup
	var overlap atomic.Bool
	wg.Add(2)
	start := make(chan struct{})
	go func() {
		defer wg.Done()
		<-start
		orch.DispatchWithContext(context.Background(), "b1", "a", "user-a", "A", nil)
	}()
	go func() {
		defer wg.Done()
		<-start
		orch.DispatchWithContext(context.Background(), "b1", "b", "user-b", "B", nil)
	}()
	close(start)
	wg.Wait()
	if be.max > 1 {
		overlap.Store(true)
	}
	if overlap.Load() {
		t.Fatalf("shared session ran concurrently max=%d", be.max)
	}
}

func TestDispatchLockWaitContextCancel(t *testing.T) {
	hold := make(chan struct{})
	be := &countingBackend{hold: hold}
	reg := registry.New()
	reg.Replace([]*bot.Bot{{
		Config: bot.Config{ID: "b1", Backend: "openai", SessionMode: "shared"},
		Backend: be,
	}})
	orch := &Orchestrator{Reg: reg, Locks: runlock.New()}

	done := make(chan struct{})
	go func() {
		orch.DispatchWithContext(context.Background(), "b1", "hold", "user-a", "A", nil)
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	res := orch.DispatchWithContext(ctx, "b1", "wait", "user-b", "B", nil)
	if res.Status != "interrupted" {
		t.Fatalf("status=%q reply=%q", res.Status, res.Reply)
	}
	close(hold)
	<-done
}
