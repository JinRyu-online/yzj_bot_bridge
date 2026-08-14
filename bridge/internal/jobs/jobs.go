package jobs

import (
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
	busyOpenID string
	extra      string
	queue      []Item
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
	s.extra = ""
	if len(s.queue) == 0 {
		delete(m.slots, scope)
		return nil, nil
	}
	item := s.queue[0]
	s.queue = s.queue[1:]
	s.busyOpenID = item.OpenID
	for i, q := range s.queue {
		notices = append(notices, Notice{OpenID: q.OpenID, Name: q.Name, Position: i + 1})
	}
	return &item, notices
}
