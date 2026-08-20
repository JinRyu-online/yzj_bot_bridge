package memory

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// AppendTurn appends one completed Q/A pair for openID.
func (s *Store) AppendTurn(openID string, t Turn) error {
	openID = strings.TrimSpace(openID)
	if openID == "" {
		return fmt.Errorf("empty open_id")
	}
	if strings.TrimSpace(t.User) == "" && strings.TrimSpace(t.Assistant) == "" {
		return fmt.Errorf("empty turn")
	}
	if t.TS == "" {
		t.TS = time.Now().UTC().Format(time.RFC3339)
	}
	t.User = StripPII(t.User)
	t.Assistant = StripPII(t.Assistant)

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.EnsureDirs(); err != nil {
		return err
	}
	path := turnsPath(s.root, openID)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

// ListTurns reads all turns for openID (oldest first).
func (s *Store) ListTurns(openID string) ([]Turn, error) {
	openID = strings.TrimSpace(openID)
	if openID == "" {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listTurnsLocked(openID)
}

func (s *Store) listTurnsLocked(openID string) ([]Turn, error) {
	path := turnsPath(s.root, openID)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []Turn
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var t Turn
		if json.Unmarshal([]byte(line), &t) != nil {
			continue
		}
		out = append(out, t)
	}
	return out, sc.Err()
}

// TurnCount returns number of completed pairs stored for openID.
func (s *Store) TurnCount(openID string) (int, error) {
	turns, err := s.ListTurns(openID)
	if err != nil {
		return 0, err
	}
	return len(turns), nil
}

// UnprofiledTurns returns turns after ProfiledCount.
func (s *Store) UnprofiledTurns(openID string, profiledCount int) ([]Turn, error) {
	turns, err := s.ListTurns(openID)
	if err != nil {
		return nil, err
	}
	if profiledCount < 0 {
		profiledCount = 0
	}
	if profiledCount >= len(turns) {
		return nil, nil
	}
	return turns[profiledCount:], nil
}

// ClearTurns removes the turns file for openID.
func (s *Store) ClearTurns(openID string) error {
	openID = strings.TrimSpace(openID)
	if openID == "" {
		return fmt.Errorf("empty open_id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(turnsPath(s.root, openID))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ListOpenIDsWithTurns returns openIDs that have a turns file.
func (s *Store) ListOpenIDsWithTurns() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := TurnsDir(s.root)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".jsonl")
		if name == "" || name == "_empty" {
			continue
		}
		// Prefer open_id from first turn if readable; else use sanitized name as hint.
		turns, err := s.listTurnsLocked(name)
		if err == nil && len(turns) > 0 {
			// We only have sanitized filename; profile open_id is authoritative.
			out = append(out, name)
			continue
		}
		out = append(out, name)
	}
	return out, nil
}
