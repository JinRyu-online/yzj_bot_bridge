package backends

import (
	"fmt"
	"strings"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/sessions"
)

func Create(cfg bot.Config, store *sessions.Store) (bot.Backend, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Backend)) {
	case "cursor_cli", "cursor", "":
		return NewCursor(cfg, store), nil
	case "claude_code", "claude":
		return NewClaude(cfg, store), nil
	case "openai", "openai_compatible":
		return NewOpenAI(cfg, store), nil
	case "opencode", "open_code":
		return &OpenCode{cfg: cfg}, nil
	default:
		return nil, fmt.Errorf("unknown backend %q", cfg.Backend)
	}
}

type OpenCode struct{ cfg bot.Config }

func (o *OpenCode) CreateSession() (string, error) { return "", nil }
func (o *OpenCode) ClearSession(string) (string, error) {
	return "", nil
}
func (o *OpenCode) Run(string, bot.RunOpts) bot.RunResult {
	return bot.RunResult{Reply: "opencode 后端尚未实现", Status: "not_implemented"}
}
