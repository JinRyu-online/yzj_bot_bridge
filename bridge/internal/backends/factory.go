package backends

import (
	"fmt"
	"strings"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/sessions"
)

// SupportedBackends 是桥接支持的引擎列表（含尚未实现的占位）。
// 底层启动/工作目录等改动后，冒烟测试必须按此列表逐个跑基本 Run 流程。
func SupportedBackends() []string {
	return []string{"cursor_cli", "claude_code", "openai", "opencode", "dsh"}
}

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
	case "dsh", "dsh_jsonrpc":
		return NewDSH(cfg, store), nil
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
