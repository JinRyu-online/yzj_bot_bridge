package jobs

import (
	"context"
	"strings"
	"sync"
)

const (
	StatusAccepted = "accepted"
	StatusMerged   = "merged"
	StatusQueued   = "queued"
)

type Result struct {
	Status   string
	Position int  // 1-based place in the waiting line; 0 if not queued
	Updated  bool // already waiting; new text was appended
}

type Notice struct {
	OpenID   string
	Name     string
	Position int
}

type Item struct {
	OpenID  string
	Name    string
	Content string
	Payload any
}

type slot struct {
	busyOpenID  string
	busyName    string
	busyContent string
	extra       string
	queue       []Item
	ctx         context.Context
	cancel      context.CancelFunc
}

// Snapshot is a point-in-time view of the running job and waiters in one scope.
type Snapshot struct {
	Current Item // OpenID empty if idle
	Extra   string
	Queue   []Item
}

type Manager struct {
	mu    sync.Mutex
	slots map[string]*slot
}

func New() *Manager {
	return &Manager{slots: map[string]*slot{}}
}

// UseChannelQueue is true when this bot should serialize the whole channel
// (shared session_mode defaults on; job_queue: user/channel overrides).
func UseChannelQueue(sessionMode, jobQueue string) bool {
	switch strings.ToLower(strings.TrimSpace(jobQueue)) {
	case "channel":
		return true
	case "user":
		return false
	default:
		return strings.EqualFold(strings.TrimSpace(sessionMode), "shared")
	}
}

// Scope is the queue key: one per bot for channel queues, one per user otherwise.
func Scope(botID, openID string, channel bool) string {
	if channel {
		return "ch:" + botID
	}
	return "u:" + botID + "\x00" + openID
}

func (m *Manager) slot(scope string) *slot {
	s, ok := m.slots[scope]
	if !ok {
		s = &slot{}
		m.slots[scope] = s
	}
	return s
}

func (m *Manager) TryAccept(scope, openID, name, content string, payload any) Result {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.slot(scope)
	if s.busyOpenID == "" {
		s.busyOpenID = openID
		s.busyName = name
		s.busyContent = content
		s.armCancel()
		return Result{Status: StatusAccepted}
	}
	if s.busyOpenID == openID {
		if s.extra != "" {
			s.extra += "\n" + content
		} else {
			s.extra = content
		}
		return Result{Status: StatusMerged}
	}
	for i := range s.queue {
		if s.queue[i].OpenID == openID {
			s.queue[i].Content = s.queue[i].Content + "\n" + content
			s.queue[i].Name = name
			s.queue[i].Payload = payload
			return Result{Status: StatusQueued, Position: i + 1, Updated: true}
		}
	}
	s.queue = append(s.queue, Item{OpenID: openID, Name: name, Content: content, Payload: payload})
	return Result{Status: StatusQueued, Position: len(s.queue)}
}

func (m *Manager) DrainExtra(scope, openID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.slots[scope]
	if s == nil || s.busyOpenID != openID {
		return ""
	}
	extra := s.extra
	s.extra = ""
	return extra
}

// Finish clears the current runner and pops the next waiter.
// notices are 1-based positions for everyone still waiting after the pop.
func (m *Manager) Finish(scope, openID string) (next *Item, notices []Notice) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.slots[scope]
	if s == nil {
		return nil, nil
	}
	if s.busyOpenID != "" && s.busyOpenID != openID {
		return nil, nil
	}
	s.busyOpenID = ""
	s.busyName = ""
	s.busyContent = ""
	s.extra = ""
	s.clearCancel()
	if len(s.queue) == 0 {
		delete(m.slots, scope)
		return nil, nil
	}
	item := s.queue[0]
	s.queue = s.queue[1:]
	s.busyOpenID = item.OpenID
	s.busyName = item.Name
	s.busyContent = item.Content
	s.armCancel()
	for i, q := range s.queue {
		notices = append(notices, Notice{OpenID: q.OpenID, Name: q.Name, Position: i + 1})
	}
	return &item, notices
}

func (s *slot) armCancel() {
	s.clearCancel()
	s.ctx, s.cancel = context.WithCancel(context.Background())
}

func (s *slot) clearCancel() {
	if s.cancel != nil {
		s.cancel = nil
	}
	s.ctx = nil
}

// Context is the cancellable context for the job currently running in scope.
func (m *Manager) Context(scope string) context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.slots[scope]
	if s == nil || s.ctx == nil {
		return context.Background()
	}
	return s.ctx
}

// Cancel aborts the engine run currently occupying scope.
// It does not dequeue waiters; Finish still runs after the backend returns.
func (m *Manager) Cancel(scope string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.slots[scope]
	if s == nil || s.cancel == nil {
		return false
	}
	s.cancel()
	s.cancel = nil
	return true
}

// Busy reports whether scope has a running job.
func (m *Manager) Busy(scope string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.slots[scope]
	return s != nil && s.busyOpenID != ""
}

// Snapshot copies the running job and waiters for scope.
func (m *Manager) Snapshot(scope string) Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.slots[scope]
	if s == nil || s.busyOpenID == "" {
		return Snapshot{}
	}
	out := Snapshot{
		Current: Item{OpenID: s.busyOpenID, Name: s.busyName, Content: s.busyContent},
		Extra:   s.extra,
	}
	if len(s.queue) > 0 {
		out.Queue = append([]Item(nil), s.queue...)
	}
	return out
}
