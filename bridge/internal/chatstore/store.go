// Package chatstore persists GUI chat test sessions to a JSON file on disk.
//
// The store is intentionally small: a versioned slice of sessions written
// atomically (tmp + rename) so that a crash mid-write never leaves a
// truncated file. All public methods are safe for concurrent use.
package chatstore

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"yzj-bridge/internal/paths"
)

// Message is a single chat turn within a Session.
type Message struct {
	Role     string `json:"role"`             // user | assistant
	Content  string `json:"content"`          //
	BotID    string `json:"bot_id,omitempty"` // bot that produced/observed the turn
	Reasoning string `json:"reasoning,omitempty"` // optional assistant reasoning captured during streaming
	TS       string `json:"ts"`               // RFC3339
}

// Session groups a sequence of Messages for one GUI chat test conversation.
type Session struct {
	ID        string    `json:"id"`
	Title      string    `json:"title"`
	BotID      string    `json:"bot_id"`
	UpdatedAt string    `json:"updated_at"`
	Messages   []Message `json:"messages"`
}

// Summary is a List-friendly projection of Session: full messages are
// omitted so the list endpoint stays cheap regardless of history length.
type Summary struct {
	ID           string `json:"id"`
	Title         string `json:"title"`
	BotID         string `json:"bot_id"`
	UpdatedAt     string `json:"updated_at"`
	MessageCount  int    `json:"message_count"`
}

// Store is a file-backed collection of Sessions.
type Store struct {
	mu   sync.Mutex
	path string
	data struct {
		Version  int       `json:"version"`
		Sessions []Session `json:"sessions"`
	}
}

// Open creates or loads a Store at the given path.
// An empty path falls back to ~/.yzj-bridge/chat_sessions.json.
// If the file does not exist a fresh empty store is returned.
func Open(path string) (*Store, error) {
	if path == "" {
		path = filepath.Join(paths.UserDataDir(), "chat_sessions.json")
	}
	s := &Store{path: path}
	s.data.Version = 1
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if len(b) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, fmt.Errorf("chatstore: parse %s: %w", path, err)
	}
	if s.data.Version == 0 {
		s.data.Version = 1
	}
	if s.data.Sessions == nil {
		s.data.Sessions = []Session{}
	}
	return s, nil
}

// Save atomically persists the current state to disk.
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	s.data.Version = 1
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// List returns summaries of all sessions ordered by UpdatedAt desc
// (most recently touched first). Messages are not included.
func (s *Store) List() []Summary {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Summary, 0, len(s.data.Sessions))
	for i := range s.data.Sessions {
		sess := &s.data.Sessions[i]
		out = append(out, Summary{
			ID:           sess.ID,
			Title:         sess.Title,
			BotID:         sess.BotID,
			UpdatedAt:     sess.UpdatedAt,
			MessageCount:  len(sess.Messages),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	return out
}

// Get returns a copy of the session with the given id, or nil if absent.
func (s *Store) Get(id string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Sessions {
		if s.data.Sessions[i].ID == id {
			src := &s.data.Sessions[i]
			cp := *src
			cp.Messages = make([]Message, len(src.Messages))
			copy(cp.Messages, src.Messages)
			if cp.Messages == nil {
				cp.Messages = []Message{}
			}
			return &cp
		}
	}
	return nil
}

// Create inserts a new empty session and persists it. title may be empty;
// it is auto-derived from the first user message in AppendMessages.
func (s *Store) Create(botID, title string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := Session{
		ID:        newID(),
		Title:      title,
		BotID:      botID,
		UpdatedAt:  nowRFC3339(),
		Messages:   []Message{},
	}
	s.data.Sessions = append(s.data.Sessions, sess)
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return &sess, nil
}

// Update mutates title and/or bot_id for the session with the given id.
// Empty arguments are ignored (left unchanged). Returns the updated
// session copy or nil if the id was not found.
func (s *Store) Update(id, title, botID string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Sessions {
		if s.data.Sessions[i].ID == id {
			if title != "" {
				s.data.Sessions[i].Title = title
			}
			if botID != "" {
				s.data.Sessions[i].BotID = botID
			}
			s.data.Sessions[i].UpdatedAt = nowRFC3339()
			cp := s.data.Sessions[i]
			if err := s.saveLocked(); err != nil {
				return nil, err
			}
			return &cp, nil
		}
	}
	return nil, nil
}

// Delete removes the session with the given id. Missing ids are a no-op.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Sessions {
		if s.data.Sessions[i].ID == id {
			s.data.Sessions = append(s.data.Sessions[:i], s.data.Sessions[i+1:]...)
			return s.saveLocked()
		}
	}
	return nil
}

// AppendMessages appends msgs to the session with the given id, refreshing
// UpdatedAt. If the session's title is empty and any appended message is the
// first user turn, the title is auto-derived from that user content
// (truncated to ~40 runes). Missing id is a no-op.
func (s *Store) AppendMessages(id string, msgs ...Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Sessions {
		if s.data.Sessions[i].ID != id {
			continue
		}
		sess := &s.data.Sessions[i]
		for _, m := range msgs {
			if m.TS == "" {
				m.TS = nowRFC3339()
			}
			sess.Messages = append(sess.Messages, m)
			if m.Role == "user" && strings.TrimSpace(sess.Title) == "" {
				sess.Title = titleFromContent(m.Content)
			}
		}
		sess.UpdatedAt = nowRFC3339()
		return s.saveLocked()
	}
	return nil
}

// newID returns a 32-char hex id from crypto/rand, e.g.
// "8a1f...". This avoids adding a uuid dependency to go.mod.
func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	// RFC 4122 v4-ish variant bits, purely cosmetic.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[:])
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// titleFromContent collapses whitespace and truncates to ~40 runes.
func titleFromContent(content string) string {
	content = strings.TrimSpace(strings.Join(strings.Fields(content), " "))
	if content == "" {
		return ""
	}
	const max = 40
	if utf8.RuneCountInString(content) <= max {
		return content
	}
	r := []rune(content)
	return string(r[:max]) + "…"
}
