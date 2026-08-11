package backends

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	cfg    bot.Config
	store  *sessions.Store
	client *http.Client
	base   string
	apiKey string
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
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []oaToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
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

type oaRequest struct {
	Model    string      `json:"model"`
	Messages []oaMessage `json:"messages"`
	Tools    []oaTool    `json:"tools,omitempty"`
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
		maxRounds = 8
	}

	system := o.cfg.SystemPrompt
	skills := opts.Skills
	if len(skills) == 0 {
		skills = o.cfg.Skills
	}
	if len(skills) > 0 {
		system += "\n\n优先使用这些 skills: " + strings.Join(skills, ", ")
	}
	system += "\n工作区: " + opts.Workspace

	messages := []oaMessage{}
	if system != "" {
		messages = append(messages, oaMessage{Role: "system", Content: system})
	}
	messages = append(messages, oaMessage{Role: "user", Content: prompt})
	tools := buildOATools(mode == "agent")

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	var final string
	for round := 0; round < maxRounds; round++ {
		resp, err := o.chat(ctx, model, messages, tools)
		if err != nil {
			return bot.RunResult{Reply: "openai 调用失败: " + err.Error(), Status: "error"}
		}
		if len(resp.Choices) == 0 {
			return bot.RunResult{Reply: "(空回复)", Status: "empty"}
		}
		msg := resp.Choices[0].Message
		if len(msg.ToolCalls) == 0 {
			final = strings.TrimSpace(msg.Content)
			break
		}
		messages = append(messages, msg)
		for _, tc := range msg.ToolCalls {
			out := o.execTool(tc.Function.Name, tc.Function.Arguments, opts.Workspace, mode)
			messages = append(messages, oaMessage{
				Role: "tool", ToolCallID: tc.ID, Content: out, Name: tc.Function.Name,
			})
		}
	}
	if final == "" {
		final = "(空回复)"
	}
	if key, ok := sessions.ResolveSessionKey(o.cfg, opts.OperatorOpenID); ok {
		sessions.AppendConversation("", o.cfg.Group, o.cfg.ID, key, "user", prompt)
		sessions.AppendConversation("", o.cfg.Group, o.cfg.ID, key, "assistant", final)
	}
	return bot.RunResult{Reply: final, Status: "ok"}
}

func (o *OpenAIBackend) chat(ctx context.Context, model string, messages []oaMessage, tools []oaTool) (*oaResponse, error) {
	reqBody := oaRequest{Model: model, Messages: messages}
	if len(tools) > 0 {
		reqBody.Tools = tools
	}
	b, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.base+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var out oaResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decode: %w body=%s", err, truncate(string(data), 300))
	}
	if out.Error != nil {
		return nil, fmt.Errorf("%s", out.Error.Message)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, truncate(string(data), 300))
	}
	return &out, nil
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
				"type": "object",
				"properties": map[string]any{"command": map[string]any{"type": "string"}},
				"required":   []string{"command"},
			}),
		)
	}
	return tools
}

func (o *OpenAIBackend) execTool(name, argsJSON, workspace, mode string) string {
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
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "cmd", "/C", cmdLine)
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
	OK        bool             `json:"ok"`
	LatencyMS int64            `json:"latency_ms"`
	Models    []ModelInfo      `json:"models"`
	Error     string           `json:"error,omitempty"`
	Endpoint  string           `json:"endpoint"`
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
