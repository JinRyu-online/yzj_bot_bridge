package jobs

import "sync"

type Manager struct {
	mu   sync.Mutex
	busy map[string]bool
	extra map[string]string
}

func New() *Manager {
	return &Manager{busy: map[string]bool{}, extra: map[string]string{}}
}

func key(botID, openID string) string { return botID + "\x00" + openID }

// TryAccept returns ("accepted"|"merged"|"busy")
func (m *Manager) TryAccept(botID, openID, content string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := key(botID, openID)
	if m.busy[k] {
		if prev, ok := m.extra[k]; ok && prev != "" {
			m.extra[k] = prev + "\n" + content
		} else {
			m.extra[k] = content
		}
		return "merged"
	}
	m.busy[k] = true
	return "accepted"
}

func (m *Manager) DrainExtra(botID, openID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := key(botID, openID)
	extra := m.extra[k]
	delete(m.extra, k)
	return extra
}

func (m *Manager) Done(botID, openID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := key(botID, openID)
	delete(m.busy, k)
}
