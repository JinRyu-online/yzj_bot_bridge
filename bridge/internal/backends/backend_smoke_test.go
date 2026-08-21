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
	"sync"
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
			case "dsh":
				smokeDSH(t, dshStubPath(t), ws)
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
				if name != "dsh" {
					t.Skip("config.yaml 未配置该后端")
				}
				// dsh 无现成机器人时用默认配置合成（live 冒烟专用，仅当 YZJ_SMOKE=1 才会走到）。
				cfg = bot.Config{
					ID:             "smoke-live-dsh",
					Backend:        "dsh",
					DSHProfile:     "jsonrpc",
					DSHProvider:    "kuaidi100",
					DSHModel:       "",
					DSHTimeout:     60,
					DSHTTLSeconds:  300,
					DSHMaxWarm:     3,
				}
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

// smokeDSH 覆盖：首条创建（cwd 断言）→ 热路径 → 双会话并发交错 → resume 已知/未知两分支
// → ClearSession evict。全部走离线 dsh_stub.mjs（node 直调，poolKey 含入口路径与 live 隔离）。
func smokeDSH(t *testing.T, stubEntry, ws string) {
	t.Helper()
	// stub 用 node 直调跑（模拟真实 DSH 入口）：无 node 的机器/CI 直接跳过，不硬失败。
	if _, err := exec.LookPath(resolveNodeBin("")); err != nil {
		t.Skip("node 不可用，跳过 dsh stub 冒烟")
	}
	resetPoolForTest()
	t.Setenv("DSH_STUB_STATE_FILE", filepath.Join(t.TempDir(), "stub-state.jsonl"))

	cfg := bot.Config{
		ID:            "smoke-dsh",
		Backend:       "dsh",
		NodeBin:       resolveNodeBin(""),
		DSHEntry:      stubEntry,
		DSHProfile:    "jsonrpc",
		DSHProvider:   "kuaidi100",
		DSHModel:      "deepseek-v4-flash",
		DSHTimeout:    30,
		DSHTTLSeconds: 300,
		DSHMaxWarm:    3,
	}
	be, err := Create(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}

	// 1) 首条创建：未知 sid → resume(not found) → 回退 session/prompt 创建，cwd 断言。
	sidA, err := be.CreateSession()
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	got := be.Run("ping", bot.RunOpts{Workspace: ws, SessionID: sidA, Mode: "ask"})
	assertSmokeOK(t, got)
	if !strings.Contains(got.Reply, ws) {
		t.Fatalf("首条创建 cwd 断言失败: reply=%q 应包含 workspace=%q", got.Reply, ws)
	}

	// 2) 热路径：同会话第二条走 session/prompt，仍 ok。
	got = be.Run("ping", bot.RunOpts{Workspace: ws, SessionID: sidA, Mode: "ask"})
	assertSmokeOK(t, got)
	if !strings.Contains(got.Reply, ws) {
		t.Fatalf("热路径 cwd 断言失败: reply=%q 应包含 %q", got.Reply, ws)
	}

	// 3) 双会话并发交错：A/B 各自 workspace，回复不串扰。
	sidB, err := be.CreateSession()
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	wsB := filepath.Join(t.TempDir(), "wsB")
	var wg sync.WaitGroup
	results := make(chan bot.RunResult, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		results <- be.Run("ping", bot.RunOpts{Workspace: ws, SessionID: sidA})
	}()
	go func() {
		defer wg.Done()
		results <- be.Run("ping", bot.RunOpts{Workspace: wsB, SessionID: sidB})
	}()
	wg.Wait()
	close(results)
	var ra, rb bot.RunResult
	for r := range results {
		if r.Status != "ok" {
			t.Fatalf("并发轮 status=%s reply=%q", r.Status, r.Reply)
		}
		if strings.Contains(r.Reply, wsB) {
			rb = r
		} else if strings.Contains(r.Reply, ws) {
			ra = r
		} else {
			t.Fatalf("并发轮回复无 workspace 特征: %q", r.Reply)
		}
	}
	if ra.Reply == "" || rb.Reply == "" {
		t.Fatalf("双会话未成对完成: A=%q B=%q", ra.Reply, rb.Reply)
	}
	if strings.Contains(ra.Reply, wsB) || strings.Contains(rb.Reply, ws) {
		t.Fatalf("双会话回复串扰: A=%q B=%q", ra.Reply, rb.Reply)
	}

	// 4) resume 已知：换新池进程后同 sid 冷载（从状态文件恢复），回复带 resumed 标记。
	resetPoolForTest()
	got = be.Run("ping", bot.RunOpts{Workspace: ws, SessionID: sidA, Mode: "ask"})
	if got.Status != "ok" || !strings.Contains(got.Reply, "resumed") {
		t.Fatalf("resume 已知分支失败: status=%s reply=%q", got.Status, got.Reply)
	}
	if !strings.Contains(got.Reply, ws) {
		t.Fatalf("resume 后 cwd 恢复断言失败: reply=%q 应包含 %q", got.Reply, ws)
	}

	// 5) resume 未知：全新 sid → not found → 回退 session/prompt 创建。
	sidC, err := be.CreateSession()
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	got = be.Run("ping", bot.RunOpts{Workspace: ws, SessionID: sidC, Mode: "ask"})
	assertSmokeOK(t, got)
	if !strings.Contains(got.Reply, ws) {
		t.Fatalf("resume 未知回退创建 cwd 断言失败: reply=%q", got.Reply)
	}

	// 6) ClearSession evict：旧 sid 从池 evict 后走 resume（而非热 prompt）。
	sidD, err := be.CreateSession()
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	got = be.Run("ping", bot.RunOpts{Workspace: ws, SessionID: sidD, Mode: "ask"})
	assertSmokeOK(t, got)
	newSid, err := be.ClearSession(sidD)
	if err != nil {
		t.Fatalf("ClearSession: %v", err)
	}
	if newSid == "" || newSid == sidD {
		t.Fatalf("ClearSession 应返回新 sid: old=%q new=%q", sidD, newSid)
	}
	got = be.Run("ping", bot.RunOpts{Workspace: ws, SessionID: sidD, Mode: "ask"})
	if got.Status != "ok" || !strings.Contains(got.Reply, "resumed") {
		t.Fatalf("ClearSession evict 后旧 sid 应走 resume: status=%s reply=%q", got.Status, got.Reply)
	}
	// 新 sid 走创建路径。
	got = be.Run("ping", bot.RunOpts{Workspace: ws, SessionID: newSid, Mode: "ask"})
	assertSmokeOK(t, got)

	resetPoolForTest() // 用例结束清场，避免影响后续用例
}

// dshStubPath 返回 testdata/dsh_stub.mjs 的绝对路径（node 直调，作为 dsh_entry 注入）。
func dshStubPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(thisFile), "testdata", "dsh_stub.mjs")
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
	case "dsh":
		if !dshLiveReady(cfg) {
			t.Skip("node / dsh 入口 / profile 缺失")
		}
		if cfg.DSHTimeout <= 0 || cfg.DSHTimeout > 60 {
			cfg.DSHTimeout = 60
		}
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
	case "dsh", "dsh_jsonrpc":
		return "dsh"
	default:
		return strings.ToLower(strings.TrimSpace(name))
	}
}

// dshLiveReady 判断 live 冒烟所需环境：node 可执行、dsh 入口可解析、profile 目录存在。
func dshLiveReady(cfg bot.Config) bool {
	if _, err := exec.LookPath(resolveNodeBin(cfg.NodeBin)); err != nil {
		return false
	}
	entry := resolveDSHEntry(cfg)
	if entry == "" {
		return false
	}
	if st, err := os.Stat(entry); err != nil || st.IsDir() {
		return false
	}
	profile := filepath.Join(dshHome(cfg), "profiles", firstNonEmptyStr(cfg.DSHProfile, "jsonrpc"))
	if st, err := os.Stat(profile); err != nil || !st.IsDir() {
		return false
	}
	return true
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
