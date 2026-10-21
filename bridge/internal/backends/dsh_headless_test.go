package backends

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yzj-bridge/internal/bot"
)

func TestParseDSHHeadlessOutput(t *testing.T) {
	data := []byte(`{"type":"session","sessionId":"session-abc","cwd":"C:\\ws"}
{"type":"status","phase":"turn_start","turn":1}
{"type":"text","text":"po"}
{"type":"text","text":"ng"}
{"type":"status","phase":"turn_end","turn":1,"reason":{"kind":"completed"}}
{"type":"final","text":"pong"}
`)
	out := parseDSHHeadlessOutput(data)
	if out.SessionID != "session-abc" || out.Cwd != `C:\ws` {
		t.Fatalf("session parse: %+v", out)
	}
	if out.Final != "pong" || strings.Join(out.Texts, "") != "pong" {
		t.Fatalf("text parse: %+v", out)
	}
	if out.Err != "" || out.Reason != "completed" {
		t.Fatalf("reason parse: %+v", out)
	}
}

func TestParseDSHHeadlessOutputError(t *testing.T) {
	data := []byte(`{"type":"status","phase":"turn_end","turn":1,"reason":{"kind":"error","error":{"message":"MISSING_CREDENTIAL: no key"}}}
{"type":"final","text":""}
`)
	out := parseDSHHeadlessOutput(data)
	if out.Reason != "error" || !strings.Contains(out.Err, "MISSING_CREDENTIAL") {
		t.Fatalf("error parse: %+v", out)
	}
}

func TestEnsureHeadlessProfile(t *testing.T) {
	home := t.TempDir()
	desktopDir := filepath.Join(home, "profiles", "desktop")
	if err := os.MkdirAll(desktopDir, 0o755); err != nil {
		t.Fatal(err)
	}
	desktopPatch := `- id: permission
  name: "@deepseek-ai/dsh-permission-presets"
  config:
    defaultPreset: danger-full-access
- id: llm-pi-ai
  name: "@deepseek-ai/dsh-llm-pi-ai"
  config:
    providers:
      kuaidi100:
        apiKeyEnv: KUAIDI100_API_KEY
        api: openai-completions
        baseURL: http://example/v1
        models:
          - id: deepseek-flash
            name: deepseek-flash
- id: agent-default-model
  name: "@deepseek-ai/dsh-agent-default-model"
  config:
    provider: kuaidi100
    model: deepseek-flash
`
	if err := os.WriteFile(filepath.Join(desktopDir, "cordis.patch.yml"), []byte(desktopPatch), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := bot.Config{
		DSHHome:     home,
		DSHProvider: "kuaidi100",
		DSHModel:    "deepseek-v4-flash",
	}
	if err := ensureHeadlessProfile(cfg, dshManagedHeadlessProfile); err != nil {
		t.Fatalf("ensureHeadlessProfile: %v", err)
	}
	dir := filepath.Join(home, "profiles", dshManagedHeadlessProfile)
	pkg, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pkg), `"@deepseek-ai/dsh-headless"`) {
		t.Fatalf("package.json 缺少 dsh-headless: %s", pkg)
	}
	patch, err := os.ReadFile(filepath.Join(dir, "cordis.patch.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(patch)
	for _, want := range []string{
		"id: credentials",
		"disabled: false",
		"kuaidi100",
		"deepseek-v4-flash",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("headless profile 缺少 %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "@deepseek-ai/dsh-sdk-jsonrpc-server") {
		t.Fatalf("headless profile 不应插入 SDK server:\n%s", text)
	}
	// 幂等
	before := text
	if err := ensureHeadlessProfile(cfg, dshManagedHeadlessProfile); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "cordis.patch.yml"))
	if string(after) != before {
		t.Fatal("ensureHeadlessProfile 非幂等")
	}
}
