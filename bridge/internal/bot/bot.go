package bot

import (
	"context"
	"sync"
	"time"
)

type Config struct {
	ID                      string   `json:"id"`
	Name                    string   `json:"name"`
	Group                   string   `json:"group"`
	SendMsgURL              string   `json:"send_msg_url"`
	RoleID                  string   `json:"role_id"`
	ChannelKey              string   `json:"channel_key"`
	Backend                 string   `json:"backend"`
	SystemPrompt            string   `json:"system_prompt"`
	Skills                  []string `json:"skills"`
	Model                   string   `json:"model"`
	Workspace               string   `json:"workspace"`
	AllowUsers              []string `json:"allow_users"`
	AllowOpenIDs            []string `json:"allow_openids"`
	CursorAPIKey            string   `json:"cursor_api_key"`
	CursorBin               string   `json:"cursor_bin"`
	CursorSandbox           string   `json:"cursor_sandbox"`
	CursorForce             bool     `json:"cursor_force"`
	CursorStream            bool     `json:"cursor_stream"`
	CursorStreamPart        bool     `json:"cursor_stream_partial"`
	CursorTimeout           int      `json:"cursor_timeout"`
	ClaudeBin               string   `json:"claude_bin"`
	PermissionMode          string   `json:"permission_mode"`
	AllowedTools            []string `json:"allowed_tools"`
	MaxBudgetUSD            float64  `json:"max_budget_usd"`
	OpenCodeBin             string   `json:"opencode_bin"`
	// DSH 后端：每模型共享进程池 + resume 插件（@bridge/dsh-jsonrpc-resume）。
	NodeBin       string `json:"node_bin"`
	DSHEntry      string `json:"dsh_entry"`
	DSHProfile    string `json:"dsh_profile"`
	DSHProvider   string `json:"dsh_provider"`
	DSHModel      string `json:"dsh_model"`
	DSHTimeout    int    `json:"dsh_timeout"`
	DSHTTLSeconds int    `json:"dsh_ttl_seconds"`
	DSHMaxWarm    int    `json:"dsh_max_warm"`
	DSHHome       string `json:"dsh_home"`
	OpenAIBaseURL           string   `json:"openai_base_url"`
	OpenAIAPIKey            string   `json:"openai_api_key"`
	OpenAITimeout           int      `json:"openai_timeout"`
	OpenAIMaxRounds         int      `json:"openai_max_tool_rounds"`
	ConversationsDir        string   `json:"conversations_dir"`
	OpenAICompact           bool     `json:"openai_compact"`
	OpenAICompactKeep       int      `json:"openai_compact_keep"`
	OpenAICompactAfterTurns int      `json:"openai_compact_after_turns"`
	OpenAICompactAfterRunes int      `json:"openai_compact_after_runes"`
	SessionMode             string   `json:"session_mode"`
	JobQueue                string   `json:"job_queue"`
	SharedSessionKey        string   `json:"shared_session_key"`
	AckPending              bool     `json:"ack_pending"`
	MentionOnReply          bool     `json:"mention_on_reply"`
	CommandsEnabled         bool     `json:"commands_enabled"`
	InboundMode             string   `json:"inbound_mode"`
	WebhookPath             string   `json:"webhook_path"`
	Secret                  string   `json:"secret"`
	// MemoryEnabled defaults true; only effective when defaults.memory.enabled is on.
	MemoryEnabled bool `json:"memory_enabled"`
}

type RuntimeStatus struct {
	Connected   bool      `json:"connected"`
	WSEnabled   bool      `json:"ws_enabled"`
	LastError   string    `json:"last_error"`
	LastEvent   time.Time `json:"last_event,omitempty"`
	InboundMode string    `json:"inbound_mode"`
}

type Backend interface {
	Run(prompt string, opts RunOpts) RunResult
	CreateSession() (string, error)
	ClearSession(sessionID string) (string, error)
}

// ToolSpec is a function-tool schema passed into backends (e.g. OpenAI).
type ToolSpec struct {
	Name        string
	Description string
	Parameters  map[string]any
}

type RunOpts struct {
	Workspace      string
	SessionID      string
	Mode           string
	Skills         []string
	SkillPrompt    string
	SkillTools     []ToolSpec
	SkillDispatch  func(toolName, argsJSON, workspace string) string
	// MemoryPrompt is the user-profile appendix; backends append it after SkillPrompt.
	MemoryPrompt   string
	Model          string
	OperatorOpenID string
	OperatorName   string
	Overrides      map[string]string
	// Context, when set, is cancelled to abort the current engine run
	// (--stop / --abort / --interrupt). Backends should derive timeouts from it.
	Context context.Context
	// History optionally overrides backend session-store / jsonl resolution
	// (tests or explicit injection). IM and GUI leave empty; backends load context.
	History []HistoryTurn
	// OnStream, when non-nil, lets a backend emit incremental updates
	// (reasoning/content deltas, tool round markers, status, errors) as the
	// model produces them. Backends that do not support streaming simply
	// ignore this callback. The callback may be invoked from the same
	// goroutine that calls Run, so it must not block Run indefinitely.
	OnStream func(StreamEvent)
}

// StreamEvent is a single incremental update emitted through RunOpts.OnStream.
// Type is one of: reasoning | content | tool_start | tool_result | status |
// error | done. Text carries the human-readable delta for reasoning/content
// or a clipped payload for tool_result. Name identifies a tool for
// tool_start/tool_result. Round is the 1-based tool-loop iteration index.
type StreamEvent struct {
	Type  string `json:"type"` // reasoning | content | tool_start | tool_result | status | error | done
	Text  string `json:"text,omitempty"`
	Name  string `json:"name,omitempty"`
	Round int    `json:"round,omitempty"`
}

// HistoryTurn is one prior chat message for multi-turn backends.
type HistoryTurn struct {
	Role    string
	Content string
}

type RunResult struct {
	Reply     string         `json:"reply"`
	Status    string         `json:"status"`
	SessionID string         `json:"session_id"`
	Raw       map[string]any `json:"raw,omitempty"`
}

type Bot struct {
	Config  Config
	Backend Backend
	mu      sync.Mutex
	Status  RuntimeStatus
}

func (b *Bot) SetConnected(v bool) {
	b.mu.Lock()
	b.Status.Connected = v
	b.mu.Unlock()
}

func (b *Bot) SetWSEnabled(v bool) {
	b.mu.Lock()
	b.Status.WSEnabled = v
	b.mu.Unlock()
}

func (b *Bot) SetLastError(err string) {
	b.mu.Lock()
	b.Status.LastError = err
	b.mu.Unlock()
}

func (b *Bot) SnapshotStatus() RuntimeStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Status
}

func ModeUsesWS(mode string) bool {
	return mode == "" || mode == "websocket" || mode == "both"
}

func ModeUsesWebhook(mode string) bool {
	return mode == "webhook" || mode == "both"
}
