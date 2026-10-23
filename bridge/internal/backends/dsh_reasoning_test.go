package backends

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
	"yzj-bridge/internal/bot"
)

func TestNormalizeDSHReasoningEffort(t *testing.T) {
	cases := map[string]string{
		"":         "",
		"default":  "",
		"low":      "low",
		"MEDIUM":   "medium",
		" high ":   "high",
		"xhigh":    "",
		"max":      "",
		"unknown":  "",
		"off":      "off",
		"OFF":      "off",
		"none":     "off",
		"disabled": "off",
	}
	for in, want := range cases {
		if got := normalizeDSHReasoningEffort(in); got != want {
			t.Fatalf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestApplyDSHModelReasoning(t *testing.T) {
	models := []any{
		map[string]any{"id": "deepseek-flash"},
		map[string]any{"id": "deepseek-v4-flash", "compat": map[string]any{"supportsStore": false}},
	}
	applyDSHModelReasoning(models, "high")
	for _, item := range models {
		m := item.(map[string]any)
		efforts, _ := m["reasoningEfforts"].(map[string]any)
		if efforts["high"] != "high" || efforts["low"] != "low" {
			t.Fatalf("reasoningEfforts not applied: %#v", m)
		}
		compat, _ := m["compat"].(map[string]any)
		if compat["supportsDeveloperRole"] != false || compat["supportsReasoningEffort"] != true {
			t.Fatalf("compat not applied: %#v", compat)
		}
	}
}

func TestApplyDSHModelReasoningEmptyIsNoop(t *testing.T) {
	m := map[string]any{"id": "deepseek-flash"}
	applyDSHModelReasoning([]any{m}, "default")
	if _, ok := m["reasoningEfforts"]; ok {
		t.Fatal("default effort must not modify model")
	}
}

func TestApplyDSHAgentReasoning(t *testing.T) {
	entries := []map[string]any{{
		"id": "agent-default-model",
		"config": map[string]any{
			"provider": "kuaidi100",
			"model":    "deepseek-flash",
		},
	}}
	cfg := bot.Config{DSHProvider: "kuaidi100", DSHModel: "deepseek-flash", DSHReasoningEffort: "high"}
	applyDSHAgentReasoning(entries, cfg)
	got := entries[0]["config"].(map[string]any)
	if got["reasoningEffort"] != "high" || got["model"] != "deepseek-flash" {
		t.Fatalf("agent config = %#v", got)
	}
}

func TestDSHAgentModelConfigDefault(t *testing.T) {
	cfg := dshAgentModelConfig("kuaidi100", "deepseek-flash", "")
	if _, ok := cfg["reasoningEffort"]; ok {
		t.Fatalf("empty effort must omit field: %#v", cfg)
	}
}

func TestEnsureHeadlessProfileAppliesReasoning(t *testing.T) {
	home := t.TempDir()
	desktop := filepath.Join(home, "profiles", "desktop")
	if err := os.MkdirAll(desktop, 0o755); err != nil {
		t.Fatal(err)
	}
	desktopPatch := `- id: llm-pi-ai
  name: "@deepseek-ai/dsh-llm-pi-ai"
  config:
    providers:
      kuaidi100:
        api: openai-completions
        baseURL: http://example.invalid/v1
        models:
          - id: deepseek-flash
            name: deepseek-flash
- id: agent-default-model
  config:
    provider: kuaidi100
    model: deepseek-flash
`
	if err := os.WriteFile(filepath.Join(desktop, "cordis.patch.yml"), []byte(desktopPatch), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := bot.Config{
		DSHHome: home, DSHProvider: "kuaidi100", DSHModel: "deepseek-flash",
		DSHReasoningEffort: "high",
	}
	if err := ensureHeadlessProfile(cfg, "yzj-headless"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "profiles", "yzj-headless", "cordis.patch.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []map[string]any
	if err := yaml.Unmarshal(raw, &entries); err != nil {
		t.Fatal(err)
	}
	var agent map[string]any
	var models []any
	for _, e := range entries {
		switch e["id"] {
		case "agent-default-model":
			agent, _ = e["config"].(map[string]any)
		case "llm-pi-ai":
			providers, _ := e["config"].(map[string]any)
			prov, _ := providers["providers"].(map[string]any)
			p, _ := prov["kuaidi100"].(map[string]any)
			models, _ = p["models"].([]any)
		}
	}
	if agent["reasoningEffort"] != "high" {
		t.Fatalf("agent reasoningEffort = %#v", agent["reasoningEffort"])
	}
	if len(models) == 0 {
		t.Fatal("models missing")
	}
	m, _ := models[0].(map[string]any)
	compat, _ := m["compat"].(map[string]any)
	if compat["supportsReasoningEffort"] != true || compat["supportsDeveloperRole"] != false {
		t.Fatalf("model compat = %#v", compat)
	}
	efforts, _ := m["reasoningEfforts"].(map[string]any)
	if efforts["high"] != "high" {
		t.Fatalf("reasoningEfforts = %#v", efforts)
	}
}

func TestDSHReasoningOff(t *testing.T) {
	models := []any{map[string]any{"id": "deepseek-flash"}}
	applyDSHModelReasoning(models, "off")
	m := models[0].(map[string]any)
	efforts, _ := m["reasoningEfforts"].(map[string]any)
	v, ok := efforts["off"]
	if !ok {
		t.Fatalf("reasoningEfforts 缺少 off: %#v", efforts)
	}
	if v != nil {
		t.Fatalf("off 必须为无值(null)，实际 %#v", v)
	}
	if efforts["low"] != "low" || efforts["high"] != "high" {
		t.Fatalf("其它档位不应受影响: %#v", efforts)
	}

	cfg := dshAgentModelConfig("kuaidi100", "deepseek-flash", "off")
	if cfg["reasoningEffort"] != "off" {
		t.Fatalf("agent reasoningEffort = %#v", cfg["reasoningEffort"])
	}
}

func TestEnsureHeadlessProfileReasoningOff(t *testing.T) {
	home := t.TempDir()
	desktop := filepath.Join(home, "profiles", "desktop")
	if err := os.MkdirAll(desktop, 0o755); err != nil {
		t.Fatal(err)
	}
	desktopPatch := `- id: llm-pi-ai
  name: "@deepseek-ai/dsh-llm-pi-ai"
  config:
    providers:
      kuaidi100:
        api: openai-completions
        baseURL: http://example.invalid/v1
        models:
          - id: deepseek-flash
            name: deepseek-flash
- id: agent-default-model
  config:
    provider: kuaidi100
    model: deepseek-flash
`
	if err := os.WriteFile(filepath.Join(desktop, "cordis.patch.yml"), []byte(desktopPatch), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := bot.Config{
		DSHHome: home, DSHProvider: "kuaidi100", DSHModel: "deepseek-flash",
		DSHReasoningEffort: "off",
	}
	if err := ensureHeadlessProfile(cfg, "yzj-headless"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "profiles", "yzj-headless", "cordis.patch.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []map[string]any
	if err := yaml.Unmarshal(raw, &entries); err != nil {
		t.Fatal(err)
	}
	var agent map[string]any
	for _, e := range entries {
		if e["id"] == "agent-default-model" {
			agent, _ = e["config"].(map[string]any)
		}
	}
	if agent["reasoningEffort"] != "off" {
		t.Fatalf("agent reasoningEffort = %#v", agent["reasoningEffort"])
	}
}
