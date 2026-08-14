package backends

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"yzj-bridge/internal/bot"
	"yzj-bridge/internal/config"
	"yzj-bridge/internal/paths"
)

// TestBackendSmokeAllEngines 是底层改动后的必跑冒烟：对 SupportedBackends 里
// 每一个引擎走一遍 CreateSession + Run（Cursor/Claude 用本地 stub，OpenAI 用 httptest，
// opencode 校验占位契约）。不访问真实模型，可在 go test ./... 里默认执行。
func TestBackendSmokeAllEngines(t *testing.T) {
	stub := buildSmokeStub(t)
	oaURL := startOpenAIPongServer(t)

	for _, name := range SupportedBackends() {
		name := name
		t.Run(name, func(t *testing.T) {
			ws := filepath.Join(t.TempDir(), "missing", name)
			switch name {
			case "cursor_cli":
				smokeCursor(t, stub, ws)
			case "claude_code":
				smokeClaude(t, stub, ws)
			case "openai":
				smokeOpenAI(t, oaURL, ws)
			case "opencode":
				smokeOpenCode(t, ws)
			default:
				t.Fatalf("未给后端 %s 编写冒烟用例", name)
			}
		})
	}
}

func TestSupportedBackendsFactory(t *testing.T) {
	for _, name := range SupportedBackends() {
		be, err := Create(bot.Config{ID: "x", Backend: name}, nil)
		if err != nil {
			t.Fatalf("Create(%s): %v", name, err)
		}
		if be == nil {
			t.Fatalf("Create(%s) returned nil", name)
		}
	}
	if _, err := Create(bot.Config{Backend: "nope"}, nil); err == nil {
		t.Fatal("unknown backend should fail")
	}
}

// TestLiveBackendSmoke 对本机 config.yaml 里已配置的真实引擎发一条最小 prompt。
// 默认跳过，避免 CI / 日常 go test 打到付费 API。
//
//	$env:YZJ_SMOKE="1"; go test ./internal/backends/ -count=1 -run TestLiveBackendSmoke
func TestLiveBackendSmoke(t *testing.T) {
	if os.Getenv("YZJ_SMOKE") != "1" {
		t.Skip("set YZJ_SMOKE=1 to hit real backend engines")
	}
	cfgPath := paths.ConfigPath()
	f, err := config.LoadFile(cfgPath)
	if err != nil {
		t.Skipf("load %s: %v", cfgPath, err)
	}
	bots, err := config.ExpandBots(f)
	if err != nil {
		t.Fatalf("ExpandBots: %v", err)
	}
	byEngine := map[string]bot.Config{}
	for _, cfg := range bots {
		key := canonicalBackend(cfg.Backend)
		if _, ok := byEngine[key]; !ok {
			byEngine[key] = cfg
		}
	}
	for _, name := range SupportedBackends() {
		name := name
		cfg, ok := byEngine[name]
		t.Run(name, func(t *testing.T) {
			if !ok {
				t.Skip("config.yaml 未配置该后端")
			}
			liveSmoke(t, cfg)
		})
	}
}

func smokeCursor(t *testing.T, stub, ws string) {
	t.Helper()
	cfg := bot.Config{
		ID:            "smoke-cursor",
		Backend:       "cursor_cli",
		CursorBin:     stub,
		Workspace:     ws,
		CursorTimeout: 20,
		CursorStream:  true,
		CursorForce:   true,
		CursorSandbox: "disabled",
	}
	be, err := Create(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	sid, err := be.CreateSession()
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if strings.TrimSpace(sid) == "" {
		t.Fatal("empty session id")
	}
	got := be.Run("ping", bot.RunOpts{Workspace: ws, SessionID: sid, Mode: "ask"})
	assertSmokeOK(t, got)
	if _, err := os.Stat(ws); err != nil {
		t.Fatalf("workspace should be created: %v", err)
	}
}

func smokeClaude(t *testing.T, stub, ws string) {
	t.Helper()
	cfg := bot.Config{
		ID:             "smoke-claude",
		Backend:        "claude_code",
		ClaudeBin:      stub,
		Workspace:      ws,
		CursorTimeout:  20,
		PermissionMode: "bypassPermissions",
	}
	be, err := Create(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := be.CreateSession(); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	got := be.Run("ping", bot.RunOpts{Workspace: ws, Mode: "ask"})
	assertSmokeOK(t, got)
}

func smokeOpenAI(t *testing.T, baseURL, ws string) {
	t.Helper()
	cfg := bot.Config{
		ID:            "smoke-openai",
		Backend:       "openai",
		Model:         "smoke-model",
		OpenAIBaseURL: baseURL,
		OpenAIAPIKey:  "test-key",
		OpenAITimeout: 10,
	}
	be, err := Create(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := be.CreateSession(); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	got := be.Run("ping", bot.RunOpts{Workspace: ws, Mode: "ask"})
	assertSmokeOK(t, got)
}

func smokeOpenCode(t *testing.T, ws string) {
	t.Helper()
	be, err := Create(bot.Config{ID: "smoke-opencode", Backend: "opencode"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := be.CreateSession(); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	got := be.Run("ping", bot.RunOpts{Workspace: ws})
	if got.Status != "not_implemented" {
		t.Fatalf("opencode status=%s reply=%s", got.Status, got.Reply)
	}
	if strings.TrimSpace(got.Reply) == "" {
		t.Fatal("opencode placeholder reply empty")
	}
}

func liveSmoke(t *testing.T, cfg bot.Config) {
	t.Helper()
	engine := canonicalBackend(cfg.Backend)
	ws := filepath.Join(t.TempDir(), "live", engine)
	cfg.Workspace = ws
	if cfg.CursorTimeout <= 0 || cfg.CursorTimeout > 90 {
		cfg.CursorTimeout = 90
	}
	if cfg.OpenAITimeout <= 0 || cfg.OpenAITimeout > 60 {
		cfg.OpenAITimeout = 60
	}
	switch engine {
	case "cursor_cli":
		if strings.TrimSpace(cfg.CursorBin) == "" {
			t.Skip("cursor_bin empty")
		}
	case "claude_code":
		if strings.TrimSpace(cfg.ClaudeBin) == "" {
			t.Skip("claude_bin empty")
		}
	case "openai":
		if strings.TrimSpace(cfg.OpenAIAPIKey) == "" || strings.TrimSpace(cfg.Model) == "" {
			t.Skip("openai key/model missing")
		}
	case "opencode":
		be, err := Create(cfg, nil)
		if err != nil {
			t.Fatal(err)
		}
		got := be.Run("ping", bot.RunOpts{Workspace: ws})
		if got.Status != "not_implemented" {
			t.Fatalf("status=%s reply=%s", got.Status, got.Reply)
		}
		return
	}
	be, err := Create(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := be.Run("只回复 pong 四个字符，不要解释。", bot.RunOpts{Workspace: ws, Mode: "ask"})
	if got.Status == "agent_missing" || got.Status == "start_error" {
		t.Fatalf("spawn failed status=%s reply=%s", got.Status, got.Reply)
	}
	if got.Status != "ok" {
		t.Fatalf("status=%s reply=%s", got.Status, got.Reply)
	}
	if strings.TrimSpace(got.Reply) == "" || strings.HasPrefix(got.Reply, "(空回复") {
		t.Fatalf("empty reply: %q", got.Reply)
	}
}

func assertSmokeOK(t *testing.T, r bot.RunResult) {
	t.Helper()
	switch r.Status {
	case "agent_missing", "start_error", "timeout":
		t.Fatalf("status=%s reply=%s", r.Status, r.Reply)
	}
	if r.Status != "ok" {
		t.Fatalf("status=%s reply=%s", r.Status, r.Reply)
	}
	if !strings.Contains(strings.ToLower(r.Reply), "pong") {
		t.Fatalf("reply %q does not contain pong", r.Reply)
	}
}

func canonicalBackend(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "cursor_cli", "cursor", "":
		return "cursor_cli"
	case "claude_code", "claude":
		return "claude_code"
	case "openai", "openai_compatible":
		return "openai"
	case "opencode", "open_code":
		return "opencode"
	default:
		return strings.ToLower(strings.TrimSpace(name))
	}
}

func buildSmokeStub(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	pkgDir := filepath.Dir(thisFile)
	out := filepath.Join(t.TempDir(), "smokestub")
	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", out, "./testdata/smokestub")
	cmd.Dir = pkgDir
	cmd.Env = os.Environ()
	if os.Getenv("GOTOOLCHAIN") == "" {
		cmd.Env = append(cmd.Env, "GOTOOLCHAIN=local")
	}
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build smokestub: %v (%s)", err, b)
	}
	return out
}

func startOpenAIPongServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": "pong"}},
			},
		})
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}
