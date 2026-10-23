package backends

import (
	"fmt"
	"strings"

	"yzj-bridge/internal/bot"
)

// normalizeDSHReasoningEffort 接受桥 UI 暴露的四档；空值表示交给 provider 默认。
// "off"/"none"/"disabled" 统一归一成 "off"：DSH 侧 off 表示“不推理、不发送任何
// reasoning 参数”（见 dshReasoningEfforts 里 off 的 null 语义）。
func normalizeDSHReasoningEffort(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "off", "none", "disabled":
		return "off"
	case "low", "medium", "high":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return ""
	}
}

// dshReasoningEfforts 声明模型支持的推理档位。
//
// off 映射为 nil 是 DSH/pi-ai 的约定：声明的 off 不带值时，pi-ai 会把它当作
// “支持 off、但什么都不发送”——这正是“关闭思考”的正确下发方式；若写成
// off: "off" 反而会把 off 这个字面量发给 provider。其余层级仍是直接下发字符串。
func dshReasoningEfforts() map[string]any {
	return map[string]any{"off": nil, "low": "low", "medium": "medium", "high": "high"}
}

// dshSelectedModel 与 DSH.resolveModel 的默认优先级保持一致。
func dshSelectedModel(cfg bot.Config) string {
	for _, m := range []string{cfg.Model, cfg.DSHModel} {
		if m = strings.TrimSpace(m); m != "" {
			return m
		}
	}
	return "deepseek-v4-flash"
}

// applyDSHModelReasoning 给 provider 模型声明 reasoning 能力。
// 自定义 OpenAI 兼容网关默认没有 reasoning 元数据；只设置 agent 的
// reasoningEffort 会被 DSH 拒绝（UNSUPPORTED_REASONING_EFFORT），因此必须同时
// 声明 reasoningEfforts，并关闭 developer role（网关不支持该角色）。
func applyDSHModelReasoning(models []any, effort string) {
	effort = normalizeDSHReasoningEffort(effort)
	if effort == "" {
		return
	}
	for _, item := range models {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		m["reasoningEfforts"] = dshReasoningEfforts()
		compat, _ := m["compat"].(map[string]any)
		if compat == nil {
			compat = map[string]any{}
		}
		compat["supportsDeveloperRole"] = false
		compat["supportsReasoningEffort"] = true
		m["compat"] = compat
	}
}

func dshAgentModelConfig(provider, model, effort string) map[string]any {
	cfg := map[string]any{"provider": provider, "model": model}
	if e := normalizeDSHReasoningEffort(effort); e != "" {
		cfg["reasoningEffort"] = e
	}
	return cfg
}

func applyDSHAgentReasoning(entries []map[string]any, cfg bot.Config) {
	effort := normalizeDSHReasoningEffort(cfg.DSHReasoningEffort)
	if effort == "" {
		return
	}
	provider := strings.TrimSpace(cfg.DSHProvider)
	if provider == "" {
		provider = "kuaidi100"
	}
	model := dshSelectedModel(cfg)
	for _, e := range entries {
		if strings.TrimSpace(fmt.Sprint(e["id"])) != "agent-default-model" {
			continue
		}
		e["config"] = dshAgentModelConfig(provider, model, effort)
		return
	}
	entries = append(entries, map[string]any{
		"id":     "agent-default-model",
		"config": dshAgentModelConfig(provider, model, effort),
	})
}
