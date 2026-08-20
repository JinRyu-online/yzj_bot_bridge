package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Store is a file-backed profile + turns repository under root
// (default ~/.yzj-bridge/memory).
type Store struct {
	mu   sync.Mutex
	root string
}

// NewStore creates a Store rooted at dir. Empty dir uses DefaultRoot().
func NewStore(root string) *Store {
	if strings.TrimSpace(root) == "" {
		root = DefaultRoot()
	}
	return &Store{root: root}
}

// DefaultRoot is ~/.yzj-bridge/memory.
func DefaultRoot() string {
	return filepath.Join(userDataDir(), "memory")
}

func userDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".yzj-bridge")
	}
	return filepath.Join(home, ".yzj-bridge")
}

// Root returns the memory root directory.
func (s *Store) Root() string { return s.root }

// EnsureDirs creates profiles/ and turns/ directories.
func (s *Store) EnsureDirs() error {
	if err := os.MkdirAll(ProfilesDir(s.root), 0o755); err != nil {
		return err
	}
	return os.MkdirAll(TurnsDir(s.root), 0o755)
}

// Get loads a profile by openID. Missing file returns (nil, nil).
func (s *Store) Get(openID string) (*Profile, error) {
	openID = strings.TrimSpace(openID)
	if openID == "" {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getLocked(openID)
}

func (s *Store) getLocked(openID string) (*Profile, error) {
	path := profilePath(s.root, openID)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var p Profile
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("memory profile parse %s: %w", path, err)
	}
	if p.OpenID == "" {
		p.OpenID = openID
	}
	return &p, nil
}

// GetOrCreate returns existing profile or a fresh one for openID.
func (s *Store) GetOrCreate(openID string) (*Profile, error) {
	openID = strings.TrimSpace(openID)
	if openID == "" {
		return nil, fmt.Errorf("empty open_id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.getLocked(openID)
	if err != nil {
		return nil, err
	}
	if p != nil {
		return p, nil
	}
	return &Profile{OpenID: openID}, nil
}

// Save writes profile atomically.
func (s *Store) Save(p *Profile) error {
	if p == nil || strings.TrimSpace(p.OpenID) == "" {
		return fmt.Errorf("profile open_id required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(p)
}

func (s *Store) saveLocked(p *Profile) error {
	if err := s.EnsureDirs(); err != nil {
		return err
	}
	p.UpdatedAt = nowRFC3339()
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	path := profilePath(s.root, p.OpenID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Delete removes the profile file (turns are left unless ClearTurns is called).
func (s *Store) Delete(openID string) error {
	openID = strings.TrimSpace(openID)
	if openID == "" {
		return fmt.Errorf("empty open_id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := profilePath(s.root, openID)
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ListProfiles returns all on-disk profiles (unsorted).
func (s *Store) ListProfiles() ([]*Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := ProfilesDir(s.root)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []*Profile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var p Profile
		if json.Unmarshal(b, &p) != nil || p.OpenID == "" {
			continue
		}
		cp := p
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].OpenID < out[j].OpenID
	})
	return out, nil
}

// Forget clears inferred+manual+locks and pending state, keeping open_id / bots_seen / cursor.
func (s *Store) Forget(openID string) (*Profile, error) {
	p, err := s.GetOrCreate(openID)
	if err != nil {
		return nil, err
	}
	p.HowToAddress = Field{}
	p.Role = Field{}
	p.AskStyle = Field{}
	p.ReplyStyle = Field{}
	p.Donts = StringListField{}
	p.Notes = Field{}
	p.FactCards = nil
	p.ForgetPendingUntil = ""
	p.LastError = ""
	p.DisplayName = ""
	if err := s.Save(p); err != nil {
		return nil, err
	}
	return p, nil
}

// ResetInferred clears only unlocked inferred values (and inferred fact cards).
func (s *Store) ResetInferred(openID string) (*Profile, error) {
	p, err := s.GetOrCreate(openID)
	if err != nil {
		return nil, err
	}
	clearInferredField(&p.HowToAddress)
	clearInferredField(&p.Role)
	clearInferredField(&p.AskStyle)
	clearInferredField(&p.ReplyStyle)
	clearInferredNotes(&p.Notes)
	if !p.Donts.Locked {
		p.Donts.Inferred = nil
	}
	var kept []FactCard
	for _, c := range p.FactCards {
		if c.Locked || c.Source == "manual" {
			kept = append(kept, c)
		}
	}
	p.FactCards = kept
	p.LastError = ""
	if err := s.Save(p); err != nil {
		return nil, err
	}
	return p, nil
}

func clearInferredField(f *Field) {
	if f.Locked {
		return
	}
	f.Inferred = ""
}

func clearInferredNotes(f *Field) {
	if f.Locked {
		return
	}
	f.Inferred = ""
}
