package sessions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/paths"
)

// GUIChatOpenIDPrefix marks control-api GUI test chats. Keys with this prefix
// always resolve to their own session store entry, independent of session_mode.
const GUIChatOpenIDPrefix = "gui-chat:"

// GUIChatOpenID builds the operator openID used by the GUI chat API.
func GUIChatOpenID(sessionID string) string {
	return GUIChatOpenIDPrefix + sessionID
}

// IsGUIChatOpenID reports whether openID belongs to a GUI chat session.
func IsGUIChatOpenID(openID string) bool {
	return strings.HasPrefix(openID, GUIChatOpenIDPrefix) && openID != GUIChatOpenIDPrefix
}

type HistoryItem struct {
	ChatID    string `json:"chat_id"`
	ClearedAt string `json:"cleared_at,omitempty"`
	Name      string `json:"name,omitempty"`
}

// CompactState is the persisted rolling summary for OpenAI multi-turn chats.
type CompactState struct {
	Summary string `json:"summary,omitempty"`
	Count   int    `json:"count,omitempty"`
	Hash    string `json:"hash,omitempty"`
}

type Entry struct {
	Current       string        `json:"current"`
	Name          string        `json:"name,omitempty"`
	History       []HistoryItem `json:"history,omitempty"`
	ProjectName   string        `json:"project_name,omitempty"`
	ProjectPath   string        `json:"project_path,omitempty"`
	AgentCWD      string        `json:"agent_cwd,omitempty"`
	OpenAICompact CompactState  `json:"openai_compact,omitempty"`
}

type Store struct {
	mu   sync.Mutex
	path string
	data struct {
		Version int                       `json:"version"`
		Bots    map[string]map[string]any `json:"bots"`
	}
}

func Open(path string) (*Store, error) {
	if path == "" {
		path = paths.SessionsPath("sessions.json")
	} else if !filepath.IsAbs(path) {
		path = paths.SessionsPath(path)
	}
	s := &Store{path: path}
	s.data.Version = 3
	s.data.Bots = map[string]map[string]any{}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	_ = json.Unmarshal(b, &s.data)
	if s.data.Bots == nil {
		s.data.Bots = map[string]map[string]any{}
	}
	if s.data.Version == 0 {
		s.data.Version = 3
	}
	return s, nil
}

func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Version = 3
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func ResolveSessionKey(cfg bot.Config, openID string) (string, bool) {
	// GUI test chats always get per-session agent context, even when the bot
	// uses shared session_mode for Yunzhijia group chat.
	if IsGUIChatOpenID(openID) {
		return openID, true
	}
	switch cfg.SessionMode {
	case "oneshot":
		return "", false
	case "shared":
		key := cfg.SharedSessionKey
		if key == "" {
			key = "__shared__"
		}
		return key, true
	default:
		if openID == "" {
			return "", false
		}
		return openID, true
	}
}

func (s *Store) getUsers(botID string) map[string]any {
	b, ok := s.data.Bots[botID]
	if !ok {
		b = map[string]any{"users": map[string]any{}}
		s.data.Bots[botID] = b
	}
	users, _ := b["users"].(map[string]any)
	if users == nil {
		users = map[string]any{}
		b["users"] = users
	}
	return users
}

func entryFromAny(v any) Entry {
	switch t := v.(type) {
	case Entry:
		return t
	case string:
		return Entry{Current: t}
	case map[string]any:
		e := Entry{
			Current: asStr(t["current"]), Name: asStr(t["name"]),
			ProjectName: asStr(t["project_name"]), ProjectPath: asStr(t["project_path"]),
			AgentCWD:      asStr(t["agent_cwd"]),
			OpenAICompact: compactFromAny(t["openai_compact"]),
		}
		if e.Current == "" {
			e.Current = asStr(t["chat_id"])
		}
		return e
	default:
		return Entry{}
	}
}

func compactFromAny(v any) CompactState {
	switch t := v.(type) {
	case CompactState:
		return t
	case map[string]any:
		c := CompactState{Summary: asStr(t["summary"]), Hash: asStr(t["hash"])}
		switch n := t["count"].(type) {
		case int:
			c.Count = n
		case int64:
			c.Count = int(n)
		case float64:
			c.Count = int(n)
		}
		return c
	default:
		return CompactState{}
	}
}

func (s *Store) GetEntry(botID, userKey string) Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	users := s.getUsers(botID)
	return entryFromAny(users[userKey])
}

func (s *Store) SetChatID(botID, userKey, chatID, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	users := s.getUsers(botID)
	e := entryFromAny(users[userKey])
	e.Current = chatID
	if name != "" {
		e.Name = name
	}
	users[userKey] = e
}

func (s *Store) ClearSession(botID, userKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	users := s.getUsers(botID)
	e := entryFromAny(users[userKey])
	if e.Current != "" {
		e.History = append(e.History, HistoryItem{
			ChatID: e.Current, ClearedAt: time.Now().UTC().Format(time.RFC3339), Name: e.Name,
		})
	}
	e.Current = ""
	e.OpenAICompact = CompactState{}
	users[userKey] = e
}

func (s *Store) OpenAICompact(botID, userKey string) CompactState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return entryFromAny(s.getUsers(botID)[userKey]).OpenAICompact
}

func (s *Store) SetOpenAICompact(botID, userKey string, c CompactState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	users := s.getUsers(botID)
	e := entryFromAny(users[userKey])
	e.OpenAICompact = c
	users[userKey] = e
}

func (s *Store) SetProject(botID, userKey, name, path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	users := s.getUsers(botID)
	e := entryFromAny(users[userKey])
	e.ProjectName = name
	e.ProjectPath = path
	e.AgentCWD = path
	users[userKey] = e
}

func ResolveAgentWorkspace(cfg bot.Config, openID string, store *Store, globalWorkspace string) string {
	if key, ok := ResolveSessionKey(cfg, openID); ok && store != nil {
		e := store.GetEntry(cfg.ID, key)
		if e.ProjectPath != "" {
			if st, err := os.Stat(e.ProjectPath); err == nil && st.IsDir() {
				return e.ProjectPath
			}
		}
		if e.AgentCWD != "" {
			if st, err := os.Stat(e.AgentCWD); err == nil && st.IsDir() {
				return e.AgentCWD
			}
		}
	}
	if cfg.Workspace != "" {
		return cfg.Workspace
	}
	if globalWorkspace != "" {
		return globalWorkspace
	}
	return filepath.Join(paths.UserDataDir(), "workspace")
}

func AppendConversation(dir, group, botID, openID, role, content string) {
	dir = ResolveConversationsDir(dir)
	_ = os.MkdirAll(dir, 0o755)
	day := time.Now().Format("2006-01-02")
	name := ConversationFileName(day, group, botID, openID)
	path := filepath.Join(dir, name)
	rec, _ := json.Marshal(map[string]any{
		"ts": time.Now().Format(time.RFC3339), "role": role, "content": content,
	})
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(rec, '\n'))
}

func sanitize(s string) string {
	b := make([]rune, 0, len(s))
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b = append(b, r)
		} else {
			b = append(b, '_')
		}
	}
	if len(b) == 0 {
		return "x"
	}
	return string(b)
}

func asStr(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
