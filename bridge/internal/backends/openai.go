package backends

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/processutil"
	"yzj-bridge/internal/sessions"
)

type OpenAIBackend struct {
	cfg       bot.Config
	store     *sessions.Store
	client    *http.Client
	base      string
	apiKey    string
	summarize func(oldSummary string, prefix []bot.HistoryTurn) (string, error)
}

func NewOpenAI(cfg bot.Config, store *sessions.Store) *OpenAIBackend {
	base := strings.TrimRight(cfg.OpenAIBaseURL, "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	return &OpenAIBackend{
		cfg: cfg, store: store, base: base, apiKey: cfg.OpenAIAPIKey,
		client: &http.Client{},
	}
}

func (o *OpenAIBackend) CreateSession() (string, error) { return "openai-session", nil }
func (o *OpenAIBackend) ClearSession(string) (string, error) {
	return o.CreateSession()
}

type oaMessage struct {
	Role             string       `json:"role"`
	Content          string       `json:"content"`
	ReasoningContent string       `json:"reasoning_content,omitempty"`
	ToolCalls        []oaToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string       `json:"tool_call_id,omitempty"`
	Name             string       `json:"name,omitempty"`
}

type oaToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type oaTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

type oaThinking struct {
	Type string `json:"type"`
}

type oaRequest struct {
	Model    string      `json:"model"`
	Messages []oaMessage `json:"messages"`
	Tools    []oaTool    `json:"tools,omitempty"`
	Stream   bool        `json:"stream,omitempty"`
	Thinking *oaThinking `json:"thinking,omitempty"`
}

type oaResponse struct {
	Choices []struct {
		Message oaMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (o *OpenAIBackend) Run(prompt string, opts bot.RunOpts) bot.RunResult {
	mode := opts.Mode
	if mode == "" {
		mode = "agent"
	}
	model := opts.Model
	if model == "" {
		model = o.cfg.Model
	}
	if model == "" {
		return bot.RunResult{Reply: "未配置 openai model", Status: "bad_config"}
	}
	timeout := o.cfg.OpenAITimeout
	if timeout <= 0 {
		timeout = 120
	}
	maxRounds := o.cfg.OpenAIMaxRounds
	if maxRounds <= 0 {
		maxRounds = 24
	}

	system := o.cfg.SystemPrompt
	if opts.SkillPrompt != "" {
		system += opts.SkillPrompt
	} else if len(opts.Skills) > 0 {
		system += "\n\n优先使用这些 skills: " + strings.Join(opts.Skills, ", ")
	} else if len(o.cfg.Skills) > 0 {
		system += "\n\n优先使用这些 skills: " + strings.Join(o.cfg.Skills, ", ")
	}
	if opts.MemoryPrompt != "" {
		system += opts.MemoryPrompt
	}
	system += "\n工作区: " + opts.Workspace

	messages := []oaMessage{}
	if system != "" {
		messages = append(messages, oaMessage{Role: "system", Content: system})
	}
	sessionKey := ""
	if key, ok := sessions.ResolveSessionKey(o.cfg, opts.OperatorOpenID); ok {
		sessionKey = key
	}
	hist := o.resolveHistory(opts)
	summary, recent := o.applyCompact(hist, sessionKey, model)
	messages = appendHistoryMessages(messages, summary, recent)
	messages = append(messages, oaMessage{Role: "user", Content: prompt})
	tools := buildOATools(mode == "agent")
	for _, st := range opts.SkillTools {
		tools = append(tools, oaToolFromSpec(st))
	}

	ctx, cancel := withRunTimeout(opts, time.Duration(timeout)*time.Second)
	defer cancel()

	stream := opts.OnStream
	emit := func(ev bot.StreamEvent) {
		if stream != nil {
			stream(ev)
		}
	}

	var final string
	var lastHadTools bool
	roundsUsed := 0
	for round := 0; round < maxRounds; round++ {
		if res, ok := resultFromCtx(ctx, "openai 超时"); ok {
			emit(bot.StreamEvent{Type: "error", Text: res.Reply})
			return res
		}
		roundsUsed = round + 1
		emit(bot.StreamEvent{Type: "status", Text: "round", Round: roundsUsed})
		var msg oaMessage
		var err error
		if stream != nil {
			msg, err = o.chatStream(ctx, model, messages, tools, opts.OnStream, roundsUsed)
		} else {
			msg, err = o.chatOnce(ctx, model, messages, tools)
		}
		if err != nil {
			if res, ok := resultFromCtx(ctx, "openai 超时"); ok {
				emit(bot.StreamEvent{Type: "error", Text: res.Reply})
				return res
			}
			emit(bot.StreamEvent{Type: "error", Text: err.Error()})
			return bot.RunResult{Reply: "openai 调用失败: " + err.Error(), Status: "error"}
		}
		if reasoning := strings.TrimSpace(msg.ReasoningContent); reasoning != "" {
			log.Printf("openai reasoning bot=%s round=%d: %s", o.cfg.ID, roundsUsed, clipRunes(reasoning, 600))
		}
		if len(msg.ToolCalls) == 0 {
			final = strings.TrimSpace(msg.Content)
			lastHadTools = false
			break
		}
		lastHadTools = true
		// Strip reasoning from the stored assistant tool-call message — some
		// gateways reject reasoning_content on subsequent requests.
		messages = append(messages, oaMessage{Role: msg.Role, Content: msg.Content, ToolCalls: msg.ToolCalls})
		for _, tc := range msg.ToolCalls {
			log.Printf("openai tool bot=%s round=%d name=%s args=%s", o.cfg.ID, roundsUsed, tc.Function.Name, clipRunes(tc.Function.Arguments, 300))
			emit(bot.StreamEvent{Type: "tool_start", Name: tc.Function.Name, Round: roundsUsed})
			out := o.execTool(ctx, tc.Function.Name, tc.Function.Arguments, opts.Workspace, mode, opts.SkillDispatch)
			log.Printf("openai tool-result bot=%s name=%s: %s", o.cfg.ID, tc.Function.Name, clipRunes(out, 500))
			emit(bot.StreamEvent{Type: "tool_result", Name: tc.Function.Name, Text: clipRunes(out, 800), Round: roundsUsed})
			messages = append(messages, oaMessage{
				Role: "tool", ToolCallID: tc.ID, Content: out, Name: tc.Function.Name,
			})
		}
	}
	// If we burned tool rounds without a final answer, force one text-only turn.
	if final == "" && lastHadTools {
		messages = append(messages, oaMessage{
			Role:    "user",
			Content: "请根据以上工具调用结果，用中文给出最终答复。不要再调用任何工具；若仍缺少信息，说明已完成的步骤与下一步需要用户做什么。",
		})
		emit(bot.StreamEvent{Type: "status", Text: "finalize", Round: roundsUsed})
		var msg oaMessage
		var err error
		if stream != nil {
			msg, err = o.chatStream(ctx, model, messages, nil, opts.OnStream, roundsUsed+1)
		} else {
			msg, err = o.chatOnce(ctx, model, messages, nil)
		}
		if err != nil {
			if res, ok := resultFromCtx(ctx, "openai 超时"); ok {
				emit(bot.StreamEvent{Type: "error", Text: res.Reply})
				return res
			}
			log.Printf("openai finalize failed bot=%s: %v", o.cfg.ID, err)
			emit(bot.StreamEvent{Type: "error", Text: err.Error()})
		} else {
			final = strings.TrimSpace(msg.Content)
			if final == "" {
				if r := strings.TrimSpace(msg.ReasoningContent); r != "" {
					final = strings.TrimSpace(r)
				}
			}
		}
	}
	if final == "" {
		if lastHadTools || roundsUsed >= maxRounds {
			final = fmt.Sprintf(
				"(空回复: 已执行最多 %d 轮工具调用仍无最终文本。可在配置中增大 openai_max_tool_rounds，或到「运行日志」查看 tool / reasoning 记录)",
				maxRounds,
			)
			log.Printf("openai empty after %d tool rounds bot=%s", maxRounds, o.cfg.ID)
		} else {
			final = "(空回复: 模型返回了空内容)"
			log.Printf("openai empty content bot=%s", o.cfg.ID)
		}
	}
	if key, ok := sessions.ResolveSessionKey(o.cfg, opts.OperatorOpenID); ok {
		sessions.AppendConversation(o.cfg.ConversationsDir, o.cfg.Group, o.cfg.ID, key, "user", prompt)
		sessions.AppendConversation(o.cfg.ConversationsDir, o.cfg.Group, o.cfg.ID, key, "assistant", final)
	}
	emit(bot.StreamEvent{Type: "done", Text: final})
	return bot.RunResult{Reply: final, Status: "ok"}
}

// chatOnce is the non-streaming path used by IM-style dispatch (no OnStream).
func (o *OpenAIBackend) chatOnce(ctx context.Context, model string, messages []oaMessage, tools []oaTool) (oaMessage, error) {
	reqBody := oaRequest{Model: model, Messages: messages}
	if len(tools) > 0 {
		reqBody.Tools = tools
	}
	return o.postChat(ctx, reqBody)
}

func (o *OpenAIBackend) postChat(ctx context.Context, reqBody oaRequest) (oaMessage, error) {
	b, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.base+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return oaMessage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return oaMessage{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var out oaResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return oaMessage{}, fmt.Errorf("decode: %w body=%s", err, truncate(string(data), 300))
	}
	if out.Error != nil {
		return oaMessage{}, fmt.Errorf("%s", out.Error.Message)
	}
	if resp.StatusCode >= 400 {
		return oaMessage{}, fmt.Errorf("http %d: %s", resp.StatusCode, truncate(string(data), 300))
	}
	if len(out.Choices) == 0 {
		return oaMessage{}, nil
	}
	return out.Choices[0].Message, nil
}

// chatStream POSTs with stream=true and parses the SSE response, calling
// onStream for each reasoning/content delta. It returns the fully
// assembled assistant message (Content, ReasoningContent, ToolCalls) in
// the same shape chatOnce would have produced.
//
// Tool-call deltas are merged by index, matching the OpenAI streaming
// tool_calls convention: the first delta carries id+function.name, and
// subsequent deltas append to function.arguments.
func (o *OpenAIBackend) chatStream(ctx context.Context, model string, messages []oaMessage, tools []oaTool, onStream func(bot.StreamEvent), round int) (oaMessage, error) {
	reqBody := oaRequest{Model: model, Messages: messages, Stream: true}
	if len(tools) > 0 {
		reqBody.Tools = tools
	}
	b, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.base+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return oaMessage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if o.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return oaMessage{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		return oaMessage{}, fmt.Errorf("http %d: %s", resp.StatusCode, truncate(string(data), 300))
	}

	var assembled oaMessage
	toolCallsByIndex := map[int]*oaToolCall{}
	toolCallOrder := []int{}
	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				if len(bytes.TrimSpace(line)) > 0 {
					_ = parseStreamLine(line, &assembled, toolCallsByIndex, &toolCallOrder, onStream, round)
				}
				break
			}
			return oaMessage{}, err
		}
		if err := parseStreamLine(line, &assembled, toolCallsByIndex, &toolCallOrder, onStream, round); err != nil {
			return oaMessage{}, err
		}
	}
	if assembled.Role == "" {
		assembled.Role = "assistant"
	}
	if len(toolCallOrder) > 0 {
		assembled.ToolCalls = make([]oaToolCall, 0, len(toolCallOrder))
		for _, i := range toolCallOrder {
			assembled.ToolCalls = append(assembled.ToolCalls, *toolCallsByIndex[i])
		}
	}
	return assembled, nil
}

// parseStreamLine consumes one SSE line, mutating the assembled message
// and tool-call slots. Returns a non-nil error only on a fatal stream
// error payload; unparseable lines are ignored.
func parseStreamLine(
	line []byte,
	assembled *oaMessage,
	toolCallsByIndex map[int]*oaToolCall,
	toolCallOrder *[]int,
	onStream func(bot.StreamEvent),
	round int,
) error {
	line = bytes.TrimRight(line, "\r\n")
	if len(line) == 0 {
		return nil
	}
	data := line
	if bytes.HasPrefix(data, []byte("data:")) {
		data = bytes.TrimPrefix(data, []byte("data:"))
	} else {
		// Non-data lines (event:, id:, comments ":stream") are ignored.
		return nil
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil
	}
	if bytes.Equal(data, []byte("[DONE]")) {
		return nil
	}
	var chunk struct {
		Choices []struct {
			Delta struct {
				Role             string `json:"role"`
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
				ToolCalls        []struct {
					Index    int    `json:"index"`
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &chunk); err != nil {
		// Skip unparseable keep-alive/comment lines rather than failing.
		return nil
	}
	if chunk.Error != nil {
		return fmt.Errorf("%s", chunk.Error.Message)
	}
	if len(chunk.Choices) == 0 {
		return nil
	}
	d := chunk.Choices[0].Delta
	if d.Role != "" && assembled.Role == "" {
		assembled.Role = d.Role
	}
	if d.ReasoningContent != "" {
		assembled.ReasoningContent += d.ReasoningContent
		if onStream != nil {
			onStream(bot.StreamEvent{Type: "reasoning", Text: d.ReasoningContent, Round: round})
		}
	}
	if d.Content != "" {
		assembled.Content += d.Content
		if onStream != nil {
			onStream(bot.StreamEvent{Type: "content", Text: d.Content, Round: round})
		}
	}
	for _, tc := range d.ToolCalls {
		slot, ok := toolCallsByIndex[tc.Index]
		if !ok {
			slot = &oaToolCall{}
			toolCallsByIndex[tc.Index] = slot
			*toolCallOrder = append(*toolCallOrder, tc.Index)
		}
		if tc.ID != "" {
			slot.ID = tc.ID
		}
		if tc.Type == "" {
			slot.Type = "function"
		} else {
			slot.Type = tc.Type
		}
		if tc.Function.Name != "" {
			slot.Function.Name = tc.Function.Name
		}
		slot.Function.Arguments += tc.Function.Arguments
	}
	return nil
}

func buildOATools(full bool) []oaTool {
	mk := func(name, desc string, params map[string]any) oaTool {
		var t oaTool
		t.Type = "function"
		t.Function.Name = name
		t.Function.Description = desc
		t.Function.Parameters = params
		return t
	}
	tools := []oaTool{
		mk("list_dir", "List files in a directory under workspace", map[string]any{
			"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}},
		}),
		mk("read_file", "Read a text file under workspace (max 256KB)", map[string]any{
			"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}},
			"required": []string{"path"},
		}),
		mk("grep", "Search text in workspace files", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{"type": "string"},
				"path":    map[string]any{"type": "string"},
			},
			"required": []string{"pattern"},
		}),
	}
	if full {
		tools = append(tools,
			mk("write_file", "Write a text file under workspace", map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"},
				},
				"required": []string{"path", "content"},
			}),
			mk("run_command", "Run a shell command in workspace (timeout 60s)", map[string]any{
				"type":       "object",
				"properties": map[string]any{"command": map[string]any{"type": "string"}},
				"required":   []string{"command"},
			}),
		)
	}
	return tools
}

func oaToolFromSpec(st bot.ToolSpec) oaTool {
	var t oaTool
	t.Type = "function"
	t.Function.Name = st.Name
	t.Function.Description = st.Description
	t.Function.Parameters = st.Parameters
	if t.Function.Parameters == nil {
		t.Function.Parameters = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return t
}

func (o *OpenAIBackend) execTool(ctx context.Context, name, argsJSON, workspace, mode string, skillDispatch func(string, string, string) string) string {
	if name == "skill" && skillDispatch != nil {
		return skillDispatch(name, argsJSON, workspace)
	}
	return o.execBuiltinTool(ctx, name, argsJSON, workspace, mode)
}

func (o *OpenAIBackend) execBuiltinTool(ctx context.Context, name, argsJSON, workspace, mode string) string {
	var args map[string]any
	_ = json.Unmarshal([]byte(argsJSON), &args)
	switch name {
	case "list_dir":
		p, err := safePath(workspace, fmt.Sprint(args["path"]))
		if err != nil {
			return err.Error()
		}
		if strings.TrimSpace(fmt.Sprint(args["path"])) == "" {
			p = workspace
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			return err.Error()
		}
		var b strings.Builder
		for _, e := range entries {
			suffix := ""
			if e.IsDir() {
				suffix = "/"
			}
			b.WriteString(e.Name() + suffix + "\n")
		}
		return b.String()
	case "read_file":
		p, err := safePath(workspace, fmt.Sprint(args["path"]))
		if err != nil {
			return err.Error()
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err.Error()
		}
		if len(data) > 256*1024 {
			data = data[:256*1024]
		}
		return string(data)
	case "write_file":
		if mode != "agent" {
			return "write_file disabled in ask/plan mode"
		}
		p, err := safePath(workspace, fmt.Sprint(args["path"]))
		if err != nil {
			return err.Error()
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err.Error()
		}
		if err := os.WriteFile(p, []byte(fmt.Sprint(args["content"])), 0o644); err != nil {
			return err.Error()
		}
		return "ok"
	case "grep":
		root := workspace
		if sub := fmt.Sprint(args["path"]); sub != "" && sub != "<nil>" {
			p, err := safePath(workspace, sub)
			if err != nil {
				return err.Error()
			}
			root = p
		}
		pattern := fmt.Sprint(args["pattern"])
		var hits []string
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || len(hits) >= 50 {
				return nil
			}
			if info.Size() > 1024*1024 {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			if strings.Contains(string(b), pattern) {
				rel, _ := filepath.Rel(workspace, path)
				hits = append(hits, rel)
			}
			return nil
		})
		if len(hits) == 0 {
			return "(no matches)"
		}
		return strings.Join(hits, "\n")
	case "run_command":
		if mode != "agent" {
			return "run_command disabled in ask/plan mode"
		}
		cmdLine := fmt.Sprint(args["command"])
		if ctx == nil {
			ctx = context.Background()
		}
		cmdCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(cmdCtx, "cmd", "/C", cmdLine)
		processutil.HideWindow(cmd)
		cmd.Dir = workspace
		out, err := cmd.CombinedOutput()
		text := string(out)
		if len(text) > 32*1024 {
			text = text[:32*1024] + "\n...(truncated)"
		}
		if err != nil {
			return text + "\nERROR: " + err.Error()
		}
		return text
	default:
		return "unknown tool: " + name
	}
}

func SafePath(workspace, p string) (string, error) { return safePath(workspace, p) }

func safePath(workspace, p string) (string, error) {
	workspace, err := filepath.Abs(workspace)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(p) == "" || p == "." || p == "<nil>" {
		return workspace, nil
	}
	var target string
	if filepath.IsAbs(p) {
		target = filepath.Clean(p)
	} else {
		target = filepath.Clean(filepath.Join(workspace, p))
	}
	rel, err := filepath.Rel(workspace, target)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path escapes workspace: %s", p)
	}
	return target, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

type OpenAIProbeResult struct {
	OK        bool        `json:"ok"`
	LatencyMS int64       `json:"latency_ms"`
	Models    []ModelInfo `json:"models"`
	Error     string      `json:"error,omitempty"`
	Endpoint  string      `json:"endpoint"`
}

// ProbeOpenAI 请求 GET {base}/models，校验连通性并返回模型列表。
func ProbeOpenAI(baseURL, apiKey string) OpenAIProbeResult {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	url := base + "/models"
	out := OpenAIProbeResult{Endpoint: url}
	if strings.TrimSpace(apiKey) == "" {
		out.Error = "api_key 为空"
		return out
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	started := time.Now()
	resp, err := http.DefaultClient.Do(req)
	out.LatencyMS = time.Since(started).Milliseconds()
	if err != nil {
		out.Error = err.Error()
		return out
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := string(body)
		if len(msg) > 300 {
			msg = msg[:300]
		}
		out.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, msg)
		return out
	}
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		out.Error = "解析 models 响应失败: " + err.Error()
		return out
	}
	for _, m := range parsed.Data {
		id := strings.TrimSpace(m.ID)
		if id == "" {
			continue
		}
		out.Models = append(out.Models, ModelInfo{ID: id, Label: id})
	}
	out.OK = true
	return out
}
