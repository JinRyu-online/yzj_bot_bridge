package logbuf

import (
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
	mu    sync.Mutex
	cap   int
	seq   int64
	lines []Line
}

func New(capacity int) *Buffer {
	if capacity <= 0 {
		capacity = 2000
	}
	return &Buffer{cap: capacity, lines: make([]Line, 0, capacity)}
}

func (b *Buffer) Append(level, bot, message string) Line {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	if bot == "" {
		bot = guessBot(message)
	}
	line := Line{
		Seq:     b.seq,
		Time:    time.Now().Format("2006-01-02 15:04:05"),
		Level:   level,
		Bot:     bot,
		Message: message,
	}
	b.lines = append(b.lines, line)
	if len(b.lines) > b.cap {
		b.lines = b.lines[len(b.lines)-b.cap:]
	}
	return line
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
