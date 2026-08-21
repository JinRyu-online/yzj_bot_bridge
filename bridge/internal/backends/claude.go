package backends

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"crypto/rand"
	"encoding/hex"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/processutil"
	"yzj-bridge/internal/sessions"
)

type Claude struct {
	cfg   bot.Config
	store *sessions.Store
}

func NewClaude(cfg bot.Config, store *sessions.Store) *Claude {
	return &Claude{cfg: cfg, store: store}
}

func (c *Claude) bin() string {
	return resolveWindowsBin(c.cfg.ClaudeBin, "claude.exe")
}

func (c *Claude) CreateSession() (string, error) {
	var b [16]byte
	_, _ = rand.Read(b[:])
	// UUID-like
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	s := hex.EncodeToString(b[:])
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:], nil
}

func (c *Claude) ClearSession(string) (string, error) {
	return c.CreateSession()
}

func (c *Claude) Run(prompt string, opts bot.RunOpts) bot.RunResult {
	mode := opts.Mode
	if mode == "" {
		mode = "agent"
	}
	model := opts.Model
	if model == "" {
		model = c.cfg.Model
	}
	sessionID := opts.SessionID
	entryKey, hasKey := sessions.ResolveSessionKey(c.cfg, opts.OperatorOpenID)
	var prevCWD string
	if hasKey && c.store != nil {
		e := c.store.GetEntry(c.cfg.ID, entryKey)
		sessionID = e.Current
		prevCWD = e.AgentCWD
	}
	if prevCWD != "" && opts.Workspace != "" && filepath.Clean(prevCWD) != filepath.Clean(opts.Workspace) {
		sessionID = ""
	}
	isNew := sessionID == ""
	if isNew {
		id, _ := c.CreateSession()
		sessionID = id
	}

	_ = os.MkdirAll(opts.Workspace, 0o755)
	if c.cfg.SystemPrompt != "" {
		_ = os.WriteFile(filepath.Join(opts.Workspace, "CLAUDE.md"), []byte(c.cfg.SystemPrompt), 0o644)
	}
	if opts.SkillPrompt != "" {
		prompt = prompt + opts.SkillPrompt
	} else if len(opts.Skills) > 0 {
		prompt = prompt + "\n\n优先使用这些 skills: " + strings.Join(opts.Skills, ", ")
	} else if len(c.cfg.Skills) > 0 {
		prompt = prompt + "\n\n优先使用这些 skills: " + strings.Join(c.cfg.Skills, ", ")
	}
	if opts.MemoryPrompt != "" {
		prompt = prompt + opts.MemoryPrompt
	}

	args := []string{"--print", "--output-format", "stream-json", "--verbose"}
	if c.cfg.CursorStreamPart {
		args = append(args, "--include-partial-messages")
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	perm := c.cfg.PermissionMode
	skipPerms := false
	switch mode {
	case "plan":
		perm = "plan"
	case "ask":
		perm = "default"
	default:
		if perm == "" || perm == "bypassPermissions" {
			perm = "bypassPermissions"
			skipPerms = true
		}
	}
	if perm != "" {
		args = append(args, "--permission-mode", perm)
	}
	if skipPerms {
		args = append(args, "--dangerously-skip-permissions")
	}
	if c.cfg.SystemPrompt != "" {
		args = append(args, "--system-prompt", c.cfg.SystemPrompt)
	}
	if len(c.cfg.AllowedTools) > 0 {
		args = append(args, "--allowed-tools", strings.Join(c.cfg.AllowedTools, ","))
	}
	if c.cfg.MaxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%g", c.cfg.MaxBudgetUSD))
	}
	if isNew {
		args = append(args, "--session-id", sessionID)
	} else {
		args = append(args, "--resume", sessionID)
	}
	args = appendCLIPrompt(args, prompt)

	timeout := c.cfg.CursorTimeout
	if timeout <= 0 {
		timeout = 600
	}
	ctx, cancel := withRunTimeout(opts, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.bin(), args...)
	processutil.HideWindow(cmd)
	cmd.Dir = opts.Workspace
	// 不注入 ANTHROPIC_API_KEY：Claude Code 使用本机 ~/.claude 已登录凭据。
	cmd.Env = os.Environ()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return bot.RunResult{Reply: err.Error(), Status: "start_error"}
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return bot.RunResult{Reply: "启动 claude 失败: " + err.Error(), Status: "agent_missing"}
	}

	var resultText, assistantText string
	var stream cliStreamCollect
	sc := bufio.NewScanner(stdout)
	buf := make([]byte, 0, 1024*64)
	sc.Buffer(buf, 1024*1024*8)
	for sc.Scan() {
		ev, ok := stream.readLine(sc.Text())
		if !ok {
			continue
		}
		switch fmt.Sprint(ev["type"]) {
		case "result":
			if t, ok := ev["result"].(string); ok {
				resultText = t
			}
		case "assistant":
			assistantText += extractClaudeText(ev)
		case "thinking", "reasoning":
			if t := strings.TrimSpace(extractClaudeText(ev)); t != "" {
				log.Printf("bot=%s thinking: %s", c.cfg.ID, clipClaude(compactClaude(t), 400))
			}
		case "stream_event":
			if inner, ok := ev["event"].(map[string]any); ok {
				if fmt.Sprint(inner["type"]) == "content_block_delta" {
					if delta, ok := inner["delta"].(map[string]any); ok {
						dt := fmt.Sprint(delta["type"])
						if dt == "text_delta" {
							assistantText += fmt.Sprint(delta["text"])
						} else if dt == "thinking_delta" || dt == "reasoning_delta" {
							raw := fmt.Sprint(delta["thinking"])
							if raw == "" || raw == "<nil>" {
								raw = fmt.Sprint(delta["text"])
							}
							if t := compactClaude(raw); t != "" {
								log.Printf("bot=%s thinking: %s", c.cfg.ID, clipClaude(t, 400))
							}
						}
					}
				}
			}
		}
	}
	waitErr := cmd.Wait()
	logCLINonJSON(c.cfg.ID, "claude", stream.nonJSON)
	if res, ok := resultFromCtx(ctx, "claude 超时"); ok {
		res.SessionID = sessionID
		if hasKey && c.store != nil {
			c.store.SetChatID(c.cfg.ID, entryKey, sessionID, opts.OperatorName)
			e := c.store.GetEntry(c.cfg.ID, entryKey)
			e.AgentCWD = opts.Workspace
			c.store.SetProject(c.cfg.ID, entryKey, e.ProjectName, e.ProjectPath)
			_ = c.store.Save()
		}
		return res
	}
	reply, status := cliReplyOrError(resultText, assistantText, stream.nonJSON, waitErr)
	if hasKey && c.store != nil {
		c.store.SetChatID(c.cfg.ID, entryKey, sessionID, opts.OperatorName)
		e := c.store.GetEntry(c.cfg.ID, entryKey)
		e.AgentCWD = opts.Workspace
		c.store.SetProject(c.cfg.ID, entryKey, e.ProjectName, e.ProjectPath)
		// preserve agent cwd via SetProject only sets project; update via SetChatID path + direct
		_ = c.store.Save()
	}
	return bot.RunResult{Reply: reply, Status: status, SessionID: sessionID}
}

func extractClaudeText(ev map[string]any) string {
	msg, _ := ev["message"].(map[string]any)
	if msg == nil {
		return ""
	}
	content, _ := msg["content"].([]any)
	var b strings.Builder
	for _, c := range content {
		m, _ := c.(map[string]any)
		if m == nil {
			continue
		}
		typ := fmt.Sprint(m["type"])
		if typ == "text" {
			b.WriteString(fmt.Sprint(m["text"]))
		}
		if typ == "thinking" || typ == "reasoning" {
			b.WriteString(fmt.Sprint(m["thinking"]))
			b.WriteString(fmt.Sprint(m["text"]))
		}
	}
	return b.String()
}

func clipClaude(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + "…"
}

func compactClaude(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "<nil>" {
		return ""
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '\n' })
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
}

// Claude Code 可用的 --model 别名（不配 Anthropic API Key，仅展示 CLI 别名）。
func claudeCodeAliases() []ModelInfo {
	return []ModelInfo{
		{ID: "sonnet", Label: "Sonnet (latest)"},
		{ID: "opus", Label: "Opus (latest)"},
		{ID: "haiku", Label: "Haiku (latest)"},
		{ID: "fable", Label: "Fable (latest)"},
	}
}

// ListClaudeModels 返回 Claude Code 别名列表。
// GUI 用 claude_bin 决定是否请求；CLI 暂无独立 models 子命令，仅展示本地别名。
func ListClaudeModels(bin string) (models []ModelInfo, warning string) {
	_ = bin
	return claudeCodeAliases(), ""
}
