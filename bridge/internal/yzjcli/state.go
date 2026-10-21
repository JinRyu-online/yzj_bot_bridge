package yzjcli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"yzj-bridge/internal/paths"
)

// DefaultStatePath returns ~/.yzj-bridge/yzj-poll-state.json.
func DefaultStatePath() string {
	return filepath.Join(paths.UserDataDir(), "yzj-poll-state.json")
}

// GroupState is the persisted polling cursor for one profile+group.
type GroupState struct {
	LastMsgID    string            `json:"last_msg_id"`
	LastSendTime string            `json:"last_send_time,omitempty"`
	RecentMsgIDs []string          `json:"recent_msg_ids,omitempty"`
	BotOpenIDs   []string          `json:"bot_open_ids,omitempty"`
	ContactNames map[string]string `json:"contact_names,omitempty"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

type stateFile struct {
	Version int                   `json:"version"`
	Groups  map[string]GroupState `json:"groups"`
}

// StateStore persists cursors atomically (tmp + rename), mirroring wssstate.
type StateStore struct {
	path string
	mu   sync.Mutex
	data map[string]GroupState
}

// OpenState loads (or initialises) the cursor store.
func OpenState(path string) (*StateStore, error) {
	if path == "" {
		path = DefaultStatePath()
	}
	s := &StateStore{path: path, data: map[string]GroupState{}}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	var f stateFile
	if err := json.Unmarshal(raw, &f); err == nil && f.Groups != nil {
		s.data = f.Groups
		return s, nil
	}
	// Tolerate a flat map written by older builds.
	var flat map[string]GroupState
	if err := json.Unmarshal(raw, &flat); err == nil {
		s.data = flat
	}
	return s, nil
}

// Get returns a copy of the stored state for key.
func (s *StateStore) Get(key string) (GroupState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.data[key]
	return st, ok
}

// Put stores st for key and persists the whole file.
func (s *StateStore) Put(key string, st GroupState) error {
	s.mu.Lock()
	st.UpdatedAt = time.Now()
	s.data[key] = st
	s.mu.Unlock()
	return s.Save()
}

// Update applies fn to the stored state (or a fresh one) and persists it.
func (s *StateStore) Update(key string, fn func(*GroupState)) error {
	s.mu.Lock()
	st := s.data[key]
	fn(&st)
	st.UpdatedAt = time.Now()
	s.data[key] = st
	s.mu.Unlock()
	return s.Save()
}

// AppendRecent keeps a bounded most-recent msg id ring (newest last).
func AppendRecent(list []string, id string, max int) []string {
	if max <= 0 {
		max = 200
	}
	for _, v := range list {
		if v == id {
			return list
		}
	}
	list = append(list, id)
	if len(list) > max {
		list = list[len(list)-max:]
	}
	return list
}

// HasRecent reports whether id is in the recent ring.
func HasRecent(list []string, id string) bool {
	for _, v := range list {
		if v == id {
			return true
		}
	}
	return false
}

// Save writes the state file atomically.
func (s *StateStore) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := json.MarshalIndent(stateFile{Version: 1, Groups: s.data}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Path exposes the backing file path (used by control API/status).
func (s *StateStore) Path() string { return s.path }
