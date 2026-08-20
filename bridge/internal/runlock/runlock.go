package runlock

import (
	"context"
	"strings"
	"sync"
)

// Manager provides session-scoped mutual exclusion keyed by caller-defined strings.
type Manager struct {
	mu   sync.Mutex
	sems map[string]*semaphore
}

type semaphore struct {
	ch chan struct{}
}

// New returns a Manager backed by per-key binary semaphores.
func New() *Manager {
	return &Manager{sems: make(map[string]*semaphore)}
}

func (m *Manager) sem(key string) *semaphore {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sems[key]
	if !ok {
		s = &semaphore{ch: make(chan struct{}, 1)}
		s.ch <- struct{}{}
		m.sems[key] = s
	}
	return s
}

// Acquire waits for the lock on key until ctx is done or the lock is granted.
// The returned release function must be called exactly once when work finishes.
func (m *Manager) Acquire(ctx context.Context, key string) (release func(), err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	s := m.sem(key)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.ch:
		return func() { s.ch <- struct{}{} }, nil
	}
}

// NormalizeEngine maps backend names to lock-key engine segments.
func NormalizeEngine(backend string) string {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "cursor_cli", "cursor":
		return "cursor"
	case "claude_code", "claude":
		return "claude"
	case "openai":
		return "openai"
	default:
		return strings.ToLower(strings.TrimSpace(backend))
	}
}

// SessionKey builds the orchestrator run-lock key: engine:botID:sessionKey.
func SessionKey(engine, botID, sessionKey string) string {
	return engine + ":" + botID + ":" + sessionKey
}
