package bot

import (
	"sync"
	"time"
)

type Config struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Group            string   `json:"group"`
	SendMsgURL       string   `json:"send_msg_url"`
	RoleID           string   `json:"role_id"`
	ChannelKey       string   `json:"channel_key"`
	Backend          string   `json:"backend"`
	SystemPrompt     string   `json:"system_prompt"`
	Skills           []string `json:"skills"`
	Model            string   `json:"model"`
	Workspace        string   `json:"workspace"`
	AllowUsers       []string `json:"allow_users"`
	AllowOpenIDs     []string `json:"allow_openids"`
	CursorAPIKey     string   `json:"cursor_api_key"`
	CursorBin        string   `json:"cursor_bin"`
	CursorSandbox    string   `json:"cursor_sandbox"`
	CursorForce      bool     `json:"cursor_force"`
	CursorStream     bool     `json:"cursor_stream"`
	CursorStreamPart bool     `json:"cursor_stream_partial"`
	CursorTimeout    int      `json:"cursor_timeout"`
	ClaudeBin        string   `json:"claude_bin"`
	AnthropicAPIKey  string   `json:"anthropic_api_key"`
	PermissionMode   string   `json:"permission_mode"`
	AllowedTools     []string `json:"allowed_tools"`
	MaxBudgetUSD     float64  `json:"max_budget_usd"`
	OpenCodeBin      string   `json:"opencode_bin"`
	OpenAIBaseURL    string   `json:"openai_base_url"`
	OpenAIAPIKey     string   `json:"openai_api_key"`
	OpenAITimeout    int      `json:"openai_timeout"`
	OpenAIMaxRounds  int      `json:"openai_max_tool_rounds"`
	SessionMode      string   `json:"session_mode"`
	SharedSessionKey string   `json:"shared_session_key"`
	AckPending       bool     `json:"ack_pending"`
	MentionOnReply   bool     `json:"mention_on_reply"`
	CommandsEnabled  bool     `json:"commands_enabled"`
	InboundMode      string   `json:"inbound_mode"`
	WebhookPath      string   `json:"webhook_path"`
	Secret           string   `json:"secret"`
}

type RuntimeStatus struct {
	Connected  bool      `json:"connected"`
	WSEnabled  bool      `json:"ws_enabled"`
	LastError  string    `json:"last_error"`
	LastEvent  time.Time `json:"last_event,omitempty"`
	InboundMode string   `json:"inbound_mode"`
}

type Backend interface {
	Run(prompt string, opts RunOpts) RunResult
	CreateSession() (string, error)
	ClearSession(sessionID string) (string, error)
}

type RunOpts struct {
	Workspace       string
	SessionID       string
	Mode            string
	Skills          []string
	Model           string
	OperatorOpenID  string
	OperatorName    string
	Overrides       map[string]string
}

type RunResult struct {
	Reply     string         `json:"reply"`
	Status    string         `json:"status"`
	SessionID string         `json:"session_id"`
	Raw       map[string]any `json:"raw,omitempty"`
}

type Bot struct {
	Config Config
	Backend Backend
	mu     sync.Mutex
	Status RuntimeStatus
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
