package sessions

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/paths"
)

const defaultLoadLimit = 80

// ResolveConversationsDir turns conversations_dir into an absolute path.
// Empty or relative values live under ~/.yzj-bridge.
func ResolveConversationsDir(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return filepath.Join(paths.UserDataDir(), "logs", "conversations")
	}
	if filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join(paths.UserDataDir(), dir)
}

// ConversationFileName is the jsonl basename used by AppendConversation.
func ConversationFileName(day, group, botID, openID string) string {
	return day + "_" + sanitize(group) + "_" + sanitize(botID) + "_" + sanitize(openID) + ".jsonl"
}

// LoadRecentTurns reads today's and yesterday's conversation jsonl for this
// bot/user and returns the latest turns (oldest first). limit<=0 uses 80.
func LoadRecentTurns(dir, group, botID, openID string, limit int) []bot.HistoryTurn {
	return LoadRecentTurnsAt(dir, group, botID, openID, limit, time.Now())
}

// LoadRecentTurnsAt is LoadRecentTurns with an explicit clock (for tests).
func LoadRecentTurnsAt(dir, group, botID, openID string, limit int, now time.Time) []bot.HistoryTurn {
	if openID == "" || botID == "" {
		return nil
	}
	if limit <= 0 {
		limit = defaultLoadLimit
	}
	dir = ResolveConversationsDir(dir)
	days := []string{
		now.AddDate(0, 0, -1).Format("2006-01-02"),
		now.Format("2006-01-02"),
	}
	var all []bot.HistoryTurn
	for _, day := range days {
		all = append(all, readJSONL(filepath.Join(dir, ConversationFileName(day, group, botID, openID)))...)
	}
	if len(all) > limit {
		all = all[len(all)-limit:]
	}
	return all
}

func readJSONL(path string) []bot.HistoryTurn {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []bot.HistoryTurn
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		if json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(rec.Role))
		if role != "user" && role != "assistant" {
			continue
		}
		content := strings.TrimSpace(rec.Content)
		if content == "" || strings.HasPrefix(content, "(空回复") {
			continue
		}
		out = append(out, bot.HistoryTurn{Role: role, Content: content})
	}
	return out
}
