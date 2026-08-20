package backends

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/processutil"
	"yzj-bridge/internal/sessions"
)

type Cursor struct {
	cfg   bot.Config
	store *sessions.Store
}

func NewCursor(cfg bot.Config, store *sessions.Store) *Cursor {
	return &Cursor{cfg: cfg, store: store}
}

func (c *Cursor) bin() string {
	return resolveWindowsBin(c.cfg.CursorBin, "agent.exe", "cursor-agent.exe")
}

func (c *Cursor) CreateSession() (string, error) {
	bin := c.bin()
	cmd := cursorCommand(nil, bin, "create-chat")
	processutil.HideWindow(cmd)
	env := os.Environ()
	if c.cfg.CursorAPIKey != "" {
		env = append(env, "CURSOR_API_KEY="+c.cfg.CursorAPIKey)
	}
	cmd.Env = env
	ensureCmdDir(cmd, c.cfg.Workspace)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("create-chat: %w (%s)", err, string(out))
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		cand := strings.TrimSpace(lines[i])
		if cand != "" {
			return cand, nil
		}
	}
	return "", fmt.Errorf("empty create-chat output")
}

func (c *Cursor) ClearSession(sessionID string) (string, error) {
	_ = sessionID
	return c.CreateSession()
}

func (c *Cursor) Run(prompt string, opts bot.RunOpts) bot.RunResult {
	started := time.Now()
	log.Printf("bot=%s cursor: run start", c.cfg.ID)

	mode := opts.Mode
	if mode == "" {
		mode = "agent"
	}
	model := opts.Model
	if model == "" {
		model = c.cfg.Model
	}
	sessionID := opts.SessionID
	if key, ok := sessions.ResolveSessionKey(c.cfg, opts.OperatorOpenID); ok && c.store != nil && sessionID == "" {
		sessionID = c.store.GetEntry(c.cfg.ID, key).Current
	}
	if sessionID == "" {
		t0 := time.Now()
		id, err := c.CreateSession()
		log.Printf("bot=%s cursor: create-chat %dms", c.cfg.ID, time.Since(t0).Milliseconds())
		if err != nil {
			return bot.RunResult{Reply: "创建会话失败: " + err.Error(), Status: "start_error"}
		}
		sessionID = id
		if key, ok := sessions.ResolveSessionKey(c.cfg, opts.OperatorOpenID); ok && c.store != nil {
			c.store.SetChatID(c.cfg.ID, key, sessionID, opts.OperatorName)
			_ = c.store.Save()
		}
	}

	if len(opts.Skills) > 0 || len(c.cfg.Skills) > 0 || opts.SkillPrompt != "" {
		if opts.SkillPrompt != "" {
			prompt = prompt + opts.SkillPrompt
		} else {
			skills := opts.Skills
			if len(skills) == 0 {
				skills = c.cfg.Skills
			}
			prompt = prompt + "\n\n优先使用这些 skills: " + strings.Join(skills, ", ")
		}
	}
	if opts.MemoryPrompt != "" {
		prompt = prompt + opts.MemoryPrompt
	}

	args := []string{"--print", "--workspace", opts.Workspace, "--trust"}
	if c.cfg.CursorSandbox != "" {
		args = append(args, "--sandbox", c.cfg.CursorSandbox)
	}
	if c.cfg.CursorForce {
		args = append(args, "--force")
	}
	if c.cfg.CursorStream {
		args = append(args, "--output-format", "stream-json")
		if c.cfg.CursorStreamPart {
			args = append(args, "--stream-partial-output")
		}
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if mode == "plan" || mode == "ask" {
		args = append(args, "--mode", mode)
	}
	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}
	args = appendCLIPrompt(args, prompt)

	timeout := c.cfg.CursorTimeout
	if timeout <= 0 {
		timeout = 600
	}
	ctx, cancel := withRunTimeout(opts, time.Duration(timeout)*time.Second)
	defer cancel()

	tSpawn := time.Now()
	cmd := cursorCommand(ctx, c.bin(), args...)
	processutil.HideWindow(cmd)
	ensureCmdDir(cmd, opts.Workspace)
	env := os.Environ()
	if c.cfg.CursorAPIKey != "" {
		env = append(env, "CURSOR_API_KEY="+c.cfg.CursorAPIKey)
	}
	cmd.Env = env
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return bot.RunResult{Reply: err.Error(), Status: "start_error"}
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return bot.RunResult{Reply: "启动 agent 失败: " + err.Error(), Status: "agent_missing"}
	}
	log.Printf("bot=%s cursor: agent spawned %dms model=%q", c.cfg.ID, time.Since(tSpawn).Milliseconds(), model)

	var resultText, assistantText string
	var stream cliStreamCollect
	firstEvent := true
	sc := bufio.NewScanner(stdout)
	buf := make([]byte, 0, 1024*64)
	sc.Buffer(buf, 1024*1024*8)
	for sc.Scan() {
		ev, ok := stream.readLine(sc.Text())
		if !ok {
			continue
		}
		if firstEvent {
			log.Printf("bot=%s cursor: first event +%dms type=%s", c.cfg.ID, time.Since(started).Milliseconds(), fmt.Sprint(ev["type"]))
			firstEvent = false
		}
		switch fmt.Sprint(ev["type"]) {
		case "assistant":
			if t := extractCursorText(ev); t != "" {
				assistantText += t
			}
		case "thinking", "reasoning":
			if t := strings.TrimSpace(extractCursorText(ev)); t != "" {
				log.Printf("bot=%s thinking: %s", c.cfg.ID, clipRunes(compactSpace(t), 400))
			}
		case "result":
			if t, ok := ev["result"].(string); ok {
				resultText = t
			}
		case "system":
			if fmt.Sprint(ev["subtype"]) == "init" {
				if sid, ok := ev["session_id"].(string); ok && sid != "" {
					sessionID = sid
				}
			}
		}
	}
	waitErr := cmd.Wait()
	logCLINonJSON(c.cfg.ID, "cursor", stream.nonJSON)
	log.Printf("bot=%s cursor: done total=%dms", c.cfg.ID, time.Since(started).Milliseconds())
	if res, ok := resultFromCtx(ctx, "cursor-cli 超时"); ok {
		res.SessionID = sessionID
		if key, ok := sessions.ResolveSessionKey(c.cfg, opts.OperatorOpenID); ok && c.store != nil && sessionID != "" {
			c.store.SetChatID(c.cfg.ID, key, sessionID, opts.OperatorName)
			_ = c.store.Save()
		}
		return res
	}
	reply, status := cliReplyOrError(resultText, assistantText, stream.nonJSON, waitErr)
	if key, ok := sessions.ResolveSessionKey(c.cfg, opts.OperatorOpenID); ok && c.store != nil && sessionID != "" {
		c.store.SetChatID(c.cfg.ID, key, sessionID, opts.OperatorName)
		_ = c.store.Save()
	}
	return bot.RunResult{Reply: reply, Status: status, SessionID: sessionID}
}

func extractCursorText(ev map[string]any) string {
	if msg, ok := ev["message"].(map[string]any); ok {
		if content, ok := msg["content"].([]any); ok {
			var b strings.Builder
			for _, c := range content {
				if m, ok := c.(map[string]any); ok {
					typ := fmt.Sprint(m["type"])
					if typ == "text" || typ == "thinking" || typ == "reasoning" {
						b.WriteString(fmt.Sprint(m["text"]))
						if t := fmt.Sprint(m["thinking"]); t != "" && t != "<nil>" {
							b.WriteString(t)
						}
					}
				}
			}
			return b.String()
		}
	}
	if t, ok := ev["text"].(string); ok {
		return t
	}
	if t, ok := ev["thinking"].(string); ok {
		return t
	}
	return ""
}

func clipRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + "…"
}

func compactSpace(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '\n' })
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
}

type ModelInfo struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// parseAgentModelsOutput 从 `agent models` 文本中提取 id/label（兼容换行与 Windows 压扁输出）。
func parseAgentModelsOutput(raw string) []ModelInfo {
	s := strings.ReplaceAll(raw, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lower := strings.ToLower(s)
	if i := strings.Index(lower, "tip:"); i >= 0 {
		s = s[:i]
		lower = strings.ToLower(s)
	}
	s = strings.TrimSpace(s)
	if strings.HasPrefix(lower, "available models") {
		s = strings.TrimSpace(s[len("Available models"):])
	}

	var lineModels []ModelInfo
	lineSeen := map[string]struct{}{}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		id, label, ok := strings.Cut(line, " - ")
		if !ok {
			continue
		}
		id = strings.TrimSpace(id)
		label = strings.TrimSpace(label)
		if id == "" {
			continue
		}
		if label == "" {
			label = id
		}
		if _, ok := lineSeen[id]; ok {
			continue
		}
		lineSeen[id] = struct{}{}
		lineModels = append(lineModels, ModelInfo{ID: id, Label: label})
	}

	// Flattened output: "id - Label With Spaces next-id - Next Label ..."
	var flatModels []ModelInfo
	flatSeen := map[string]struct{}{}
	re := regexp.MustCompile(`([a-zA-Z0-9][\w./:@+-]*)\s+-\s+`)
	idxs := re.FindAllStringSubmatchIndex(s, -1)
	for i, m := range idxs {
		id := strings.TrimSpace(s[m[2]:m[3]])
		if id == "" {
			continue
		}
		labelStart := m[1]
		labelEnd := len(s)
		if i+1 < len(idxs) {
			labelEnd = idxs[i+1][0]
		}
		label := strings.TrimSpace(s[labelStart:labelEnd])
		if label == "" {
			label = id
		}
		if _, ok := flatSeen[id]; ok {
			continue
		}
		flatSeen[id] = struct{}{}
		flatModels = append(flatModels, ModelInfo{ID: id, Label: label})
	}

	if len(flatModels) > len(lineModels) {
		return flatModels
	}
	return lineModels
}

// ListCursorModels 执行 `agent models` 并解析 id/label；Windows 非零退出时若已有输出仍视为成功。
func ListCursorModels(bin, apiKey, workDir string) ([]ModelInfo, error) {
	if bin == "" {
		bin = "agent"
	}
	bin = resolveWindowsBin(bin, "agent.exe", "cursor-agent.exe")
	cmd := cursorCommand(nil, bin, "models")
	processutil.HideWindow(cmd)
	env := os.Environ()
	if apiKey != "" {
		env = append(env, "CURSOR_API_KEY="+apiKey)
	}
	cmd.Env = env
	ensureCmdDir(cmd, workDir)
	out, err := cmd.CombinedOutput()
	models := parseAgentModelsOutput(string(out))
	if len(models) > 0 {
		// agent on Windows may exit non-zero (e.g. 0xc0000409) after printing models.
		return models, nil
	}
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		if len(msg) > 400 {
			msg = msg[:400] + "…"
		}
		return nil, fmt.Errorf("agent models: %v (%s)", err, msg)
	}
	return nil, fmt.Errorf("no models parsed from agent models output")
}
