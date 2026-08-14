package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/paths"
)

type File struct {
	Defaults map[string]any   `yaml:"defaults"`
	Groups   []map[string]any `yaml:"groups"`
	Bots     []map[string]any `yaml:"bots"`
	raw      map[string]any
}

func defaultMap() map[string]any {
	return map[string]any{
		"cursor_api_key": "", "anthropic_api_key": "", "workspace": "~",
		"cursor_bin": "agent", "cursor_model": "", "claude_model": "", "session_mode": "per_user", "job_queue": "",
		"allow_openids": []any{}, "allow_users": []any{},
		"cursor_timeout": 600, "ack_pending": true, "mention_on_reply": true,
		"cursor_sandbox": "disabled", "cursor_force": true,
		"cursor_stream": true, "cursor_stream_partial": true,
		"log_level": "info", "commands_enabled": true,
		"reconnect_delay_sec": 5, "reconnect_max_delay_sec": 60,
		"heartbeat_interval_sec": 30, "sessions_file": "sessions.json",
		"conversations_dir": "logs/conversations", "projects_root": "~",
		"projects": []any{}, "max_workers": 4, "shared_session_key": "__shared__",
		"claude_bin": "claude", "permission_mode": "bypassPermissions",
		"allowed_tools": []any{}, "max_budget_usd": 0, "opencode_bin": "opencode",
		"system_prompt": "", "skills": []any{}, "backend": "cursor_cli", "model": "",
		"inbound_mode": "websocket", "webhook_host": "0.0.0.0", "webhook_port": 8765,
		"stale_ms": 45000, "ws_invalid_frame_limit": 3,
		"openai_base_url": "", "openai_api_key": "", "openai_timeout": 120,
		"openai_max_tool_rounds": 8, "openai_model": "",
		"openai_compact": true, "openai_compact_keep": 6,
		"openai_compact_after_turns": 10, "openai_compact_after_runes": 8000,
		"cursor_workspace": "~/.yzj-bridge/workspace/cursor_cli",
		"claude_workspace": "~/.yzj-bridge/workspace/claude_code",
	}
}

func LoadFile(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseYAML(data)
}

// ParseYAML 从 YAML 字节解析配置（含 ExpandBots 所需结构）。
func ParseYAML(data []byte) (*File, error) {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	f := &File{raw: raw}
	if err := yaml.Unmarshal(data, f); err != nil {
		return nil, err
	}
	if f.Defaults == nil {
		f.Defaults = map[string]any{}
	}
	return f, nil
}

// ParseRaw 将 GUI/API 提交的 map 规范成 File，便于保存前校验。
func ParseRaw(raw map[string]any) (*File, error) {
	data, err := yaml.Marshal(raw)
	if err != nil {
		return nil, err
	}
	f, err := ParseYAML(data)
	if err != nil {
		return nil, err
	}
	// 保留原始 map，供后续 SaveRaw / GetRawConfig 使用。
	f.raw = raw
	return f, nil
}

func (f *File) Raw() map[string]any { return f.raw }

func (f *File) MergedDefaults() map[string]any {
	out := defaultMap()
	for k, v := range f.Defaults {
		out[k] = v
	}
	syncModel(out)
	for _, key := range []string{"workspace", "cursor_workspace", "claude_workspace", "projects_root"} {
		if s, ok := out[key].(string); ok {
			if strings.TrimSpace(s) == "" {
				out[key] = "~"
			}
			out[key] = ExpandHome(asString(out[key]))
		}
	}
	return out
}

// ExpandHome turns "~" / "~/x" into an absolute path under the user home directory.
func ExpandHome(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || p == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "."
		}
		return home
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return p[2:]
		}
		return filepath.Join(home, p[2:])
	}
	return p
}

// syncModel 仅兼容旧配置：历史上共用 model 字段时回填到 cursor_model。
// 若 model 与 openai_model 相同，视为 OpenAI 误写入，不污染 cursor_model。
func syncModel(m map[string]any) {
	cm, _ := m["cursor_model"].(string)
	md, _ := m["model"].(string)
	om, _ := m["openai_model"].(string)
	if cm == "" && md != "" {
		if om != "" && om == md {
			return
		}
		m["cursor_model"] = md
	}
}

func DeriveWSURL(sendMsgURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(sendMsgURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid send_msg_url")
	}
	q := u.Query()
	token := q.Get("yzjtoken")
	if token == "" || token == "REPLACE_ME_YZJTOKEN" || token == "REPLACE_ME" {
		return "", fmt.Errorf("missing or placeholder yzjtoken")
	}
	wsScheme := "ws"
	if strings.EqualFold(u.Scheme, "https") {
		wsScheme = "wss"
	}
	out := &url.URL{
		Scheme:   wsScheme,
		Host:     u.Host,
		Path:     "/xuntong/websocket",
		RawQuery: "yzjtoken=" + url.QueryEscape(token),
	}
	return out.String(), nil
}

func ExpandBots(f *File) ([]bot.Config, error) {
	defs := f.MergedDefaults()
	seen := map[string]struct{}{}
	seenToken := map[string]string{}
	seenURL := map[string]string{}
	seenPath := map[string]string{}
	var out []bot.Config
	for _, item := range f.Bots {
		roleID := asString(item["id"])
		if roleID == "" {
			return nil, fmt.Errorf("bots[].id required")
		}
		channels, hasCh := item["channels"].([]any)
		if !hasCh || len(channels) == 0 {
			ch := map[string]any{
				"group":        firstNonEmpty(asString(item["group"]), "default"),
				"send_msg_url": asString(item["send_msg_url"]),
			}
			channels = []any{ch}
		}
		multi := len(channels) > 1
		for _, raw := range channels {
			ch, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("invalid channel for bot %s", roleID)
			}
			group := firstNonEmpty(asString(ch["group"]), "default")
			sendURL := asString(ch["send_msg_url"])
			if sendURL == "" {
				return nil, fmt.Errorf("bot %s channel missing send_msg_url", roleID)
			}
			runtimeID := asString(ch["id"])
			if runtimeID == "" {
				if multi {
					runtimeID = roleID + "__" + group
				} else {
					runtimeID = roleID
				}
			}
			if _, dup := seen[runtimeID]; dup {
				return nil, fmt.Errorf("duplicate bot id %s", runtimeID)
			}
			seen[runtimeID] = struct{}{}

			merged := map[string]any{}
			for k, v := range defs {
				merged[k] = v
			}
			for k, v := range item {
				if k == "channels" || k == "id" || k == "group" || k == "send_msg_url" {
					continue
				}
				merged[k] = v
			}
			for k, v := range ch {
				if k == "id" || k == "group" || k == "send_msg_url" {
					continue
				}
				merged[k] = v
			}
			// 仅通道/机器人显式 model 作为覆盖；忽略 defaults.model，防止跨引擎串味。
			modelOverride := firstNonEmpty(asString(ch["model"]), asString(item["model"]))
			cfg := mapToBotConfig(merged, runtimeID, roleID, group, sendURL, modelOverride)
			if cfg.WebhookPath == "" {
				cfg.WebhookPath = "/yzj/webhook/" + cfg.ID
			}
			mode := strings.ToLower(cfg.InboundMode)
			if mode != "websocket" && mode != "webhook" && mode != "both" {
				return nil, fmt.Errorf("invalid inbound_mode for %s", runtimeID)
			}
			cfg.InboundMode = mode
			if err := checkWebhookUnique(runtimeID, sendURL, cfg.WebhookPath, seenToken, seenURL, seenPath); err != nil {
				return nil, err
			}
			out = append(out, cfg)
		}
	}
	return out, nil
}

func checkWebhookUnique(runtimeID, sendURL, webhookPath string, seenToken, seenURL, seenPath map[string]string) error {
	urlKey := normalizeSendURL(sendURL)
	if owner, ok := seenURL[urlKey]; ok {
		return fmt.Errorf("send_msg_url 已被通道 %s 使用，不能再分配给 %s", owner, runtimeID)
	}
	seenURL[urlKey] = runtimeID

	tok := extractYZJToken(sendURL)
	if !placeholderToken(tok) {
		if owner, ok := seenToken[tok]; ok {
			return fmt.Errorf("yzjtoken 已被通道 %s 使用，不能再用于 %s", owner, runtimeID)
		}
		seenToken[tok] = runtimeID
	}

	pathKey := normalizeWebhookPath(webhookPath)
	if pathKey != "" {
		if owner, ok := seenPath[pathKey]; ok {
			return fmt.Errorf("webhook_path %s 已被 %s 使用，不能再用于 %s", pathKey, owner, runtimeID)
		}
		seenPath[pathKey] = runtimeID
	}
	return nil
}

func mapToBotConfig(m map[string]any, id, roleID, group, sendURL, modelOverride string) bot.Config {
	backend := firstNonEmpty(asString(m["backend"]), "cursor_cli")
	workspace := ExpandHome(asString(m["workspace"]))
	if workspace == "" {
		switch backend {
		case "cursor_cli", "cursor":
			workspace = ExpandHome(asString(m["cursor_workspace"]))
		case "claude_code", "claude":
			workspace = ExpandHome(asString(m["claude_workspace"]))
		}
	}
	if workspace == "" {
		workspace = ExpandHome("~")
	}
	// 模型优先级：通道/机器人覆盖 > 引擎默认字段。
	model := strings.TrimSpace(modelOverride)
	switch backend {
	case "openai":
		model = firstNonEmpty(model, asString(m["openai_model"]))
	case "claude_code", "claude":
		model = firstNonEmpty(model, asString(m["claude_model"]))
	default:
		model = firstNonEmpty(model, asString(m["cursor_model"]))
	}
	return bot.Config{
		ID: id, Name: asString(m["name"]), Group: group, SendMsgURL: sendURL,
		RoleID: roleID, ChannelKey: group,
		Backend:      backend,
		SystemPrompt: asString(m["system_prompt"]), Skills: asStringSlice(m["skills"]),
		Model:      model,
		Workspace:  workspace,
		AllowUsers: asStringSlice(m["allow_users"]), AllowOpenIDs: asStringSlice(m["allow_openids"]),
		CursorAPIKey: asString(m["cursor_api_key"]), CursorBin: firstNonEmpty(asString(m["cursor_bin"]), "agent"),
		CursorSandbox: firstNonEmpty(asString(m["cursor_sandbox"]), "disabled"),
		CursorForce:   asBool(m["cursor_force"], true), CursorStream: asBool(m["cursor_stream"], true),
		CursorStreamPart: asBool(m["cursor_stream_partial"], true),
		CursorTimeout:    asInt(m["cursor_timeout"], 600),
		ClaudeBin:        firstNonEmpty(asString(m["claude_bin"]), "claude"),
		AnthropicAPIKey:  asString(m["anthropic_api_key"]),
		PermissionMode:   firstNonEmpty(asString(m["permission_mode"]), "bypassPermissions"),
		AllowedTools:     asStringSlice(m["allowed_tools"]), MaxBudgetUSD: asFloat(m["max_budget_usd"], 0),
		OpenCodeBin:   firstNonEmpty(asString(m["opencode_bin"]), "opencode"),
		OpenAIBaseURL: asString(m["openai_base_url"]), OpenAIAPIKey: asString(m["openai_api_key"]),
		OpenAITimeout: asInt(m["openai_timeout"], 120), OpenAIMaxRounds: asInt(m["openai_max_tool_rounds"], 8),
		ConversationsDir:        asString(m["conversations_dir"]),
		OpenAICompact:           asBool(m["openai_compact"], true),
		OpenAICompactKeep:       asInt(m["openai_compact_keep"], 0),
		OpenAICompactAfterTurns: asInt(m["openai_compact_after_turns"], 0),
		OpenAICompactAfterRunes: asInt(m["openai_compact_after_runes"], 0),
		SessionMode:             firstNonEmpty(asString(m["session_mode"]), "per_user"),
		JobQueue:                strings.ToLower(strings.TrimSpace(asString(m["job_queue"]))),
		SharedSessionKey:        firstNonEmpty(asString(m["shared_session_key"]), "__shared__"),
		AckPending:              asBool(m["ack_pending"], true), MentionOnReply: asBool(m["mention_on_reply"], true),
		CommandsEnabled: asBool(m["commands_enabled"], true),
		InboundMode:     firstNonEmpty(asString(m["inbound_mode"]), "websocket"),
		WebhookPath:     asString(m["webhook_path"]), Secret: asString(m["secret"]),
	}
}

func SaveRaw(path string, raw map[string]any) error {
	data, err := yaml.Marshal(raw)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func BootstrapIfNeeded(defaultYAML []byte) error {
	if err := paths.EnsureUserData(); err != nil {
		return err
	}
	p := paths.ConfigPath()
	if _, err := os.Stat(p); err == nil {
		return nil
	}
	return os.WriteFile(p, defaultYAML, 0o644)
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

func asStringSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			s := strings.TrimSpace(asString(x))
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func asBool(v any, def bool) bool {
	if v == nil {
		return def
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(t, "true") || t == "1"
	default:
		return def
	}
}

func asInt(v any, def int) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	default:
		return def
	}
}

func asFloat(v any, def float64) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	default:
		return def
	}
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
