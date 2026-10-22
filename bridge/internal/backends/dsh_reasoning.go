package backends

import (
	"fmt"
	"strings"

	"yzj-bridge/internal/bot"
)

// normalizeDSHReasoningEffort 只接受当前桥 UI 暴露的三档；空值表示交给 provider 默认。
func normalizeDSHReasoningEffort(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "low", "medium", "high":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return ""
	}
}

func dshReasoningEfforts() map[string]any {
	return map[string]any{"low": "low", "medium": "medium", "high": "high"}
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
