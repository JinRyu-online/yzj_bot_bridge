package logbuf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Line struct {
	Seq     int64  `json:"seq"`
	Time    string `json:"time"`
	Level   string `json:"level"`
	Bot     string `json:"bot,omitempty"`
	Message string `json:"message"`
}

type Buffer struct {
	mu         sync.Mutex
	cap        int
	seq        int64
	lines      []Line
	persistDir string
	file       *os.File
	fileDay    string
}

func New(capacity int) *Buffer {
	return NewWithDir(capacity, "")
}

// NewWithDir keeps a live ring buffer and appends each line as jsonl under dir.
// Daily files are named runtime-YYYY-MM-DD.jsonl. Empty dir disables persist.
func NewWithDir(capacity int, persistDir string) *Buffer {
	if capacity <= 0 {
		capacity = 2000
	}
	return &Buffer{cap: capacity, lines: make([]Line, 0, capacity), persistDir: strings.TrimSpace(persistDir)}
}

func (b *Buffer) PersistDir() string {
	if b == nil {
		return ""
	}
	return b.persistDir
}

func (b *Buffer) Close() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.file != nil {
		_ = b.file.Close()
		b.file = nil
		b.fileDay = ""
	}
}

func (b *Buffer) Append(level, bot, message string) Line {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	if bot == "" {
		bot = guessBot(message)
	}
	now := time.Now()
	line := Line{
		Seq:     b.seq,
		Time:    now.Format("2006-01-02 15:04:05"),
		Level:   level,
		Bot:     bot,
		Message: message,
	}
	b.lines = append(b.lines, line)
	if len(b.lines) > b.cap {
		b.lines = b.lines[len(b.lines)-b.cap:]
	}
	b.persistLocked(now, line)
	return line
}

func (b *Buffer) persistLocked(now time.Time, line Line) {
	if b.persistDir == "" {
		return
	}
	day := now.Format("2006-01-02")
	if b.file == nil || b.fileDay != day {
		if b.file != nil {
			_ = b.file.Close()
			b.file = nil
		}
		if err := os.MkdirAll(b.persistDir, 0o755); err != nil {
			return
		}
		path := filepath.Join(b.persistDir, "runtime-"+day+".jsonl")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return
		}
		b.file = f
		b.fileDay = day
	}
	raw, err := json.Marshal(line)
	if err != nil {
		return
	}
	_, _ = b.file.Write(append(raw, '\n'))
}

func (b *Buffer) Since(seq int64, botFilter string) []Line {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Line, 0)
	for _, l := range b.lines {
		if l.Seq <= seq {
			continue
		}
		if botFilter != "" && !matchBot(l, botFilter) {
			continue
		}
		out = append(out, l)
	}
	return out
}

func matchBot(l Line, botFilter string) bool {
	if botFilter == "" {
		return true
	}
	msgBot := l.Bot
	if msgBot == "" {
		msgBot = guessBot(l.Message)
	}
	// Channel-level filter (role__group): exact match only — do not broaden to role.
	if strings.Contains(botFilter, "__") {
		return msgBot == botFilter
	}
	// GUI panel events.
	if botFilter == "gui" {
		return msgBot == "gui" || strings.HasPrefix(l.Message, "[GUI]")
	}
	// Role-level filter: role itself or any of its channels.
	if msgBot == botFilter || strings.HasPrefix(msgBot, botFilter+"__") {
		return true
	}
	return false
}

func guessBot(message string) string {
	for _, prefix := range []string{"bot=", "[cursor:", "[claude:"} {
		if i := strings.Index(message, prefix); i >= 0 {
			rest := message[i+len(prefix):]
			end := strings.IndexAny(rest, " ]:\n\t")
			if end < 0 {
				return strings.TrimSpace(rest)
			}
			return strings.TrimSpace(rest[:end])
		}
	}
	return ""
}
