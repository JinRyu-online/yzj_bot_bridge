package backends

// dsh_live_test.go — DSH 后端真实环境冒烟（计划「验收标准」第 2 条）。
//
//	$env:YZJ_SMOKE="1"; go test ./internal/backends/ -count=1 -run TestLiveDSH -v
//
// 覆盖五场景：
//
//	a) 首条创建：未知 sid → resume(not found) → 回退 session/prompt 创建，cwd=workspace 生效（写文件验证落点）
//	b) 同会话热路径：第二条 Run 正常 ok
//	c) 杀池进程后 resume：记忆（暗号）跨进程保持
//	d) 双 bot 不同 workspace 并发：同一池进程双会话，回复不串扰、文件各落各的
//	e) idle 超 TTL 回收：dshReap() 强制一轮回收，进程退出、池 map 清空、内存归还
//
// 测试与实现同包（package backends），可直接调用 dshReap()/dshPoolGet()/resetPoolForTest() 等
// 非导出符号；对外只走公开 API（Create + CreateSession/Run）。

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"yzj-bridge/internal/bot"
)

// TestLiveDSH 是 DSH 真实环境冒烟主入口：YZJ_SMOKE=1 且 node/dsh/profile 齐备才跑，否则 t.Skip。
//
// 注意：ttl_reap 必须最先执行——同一 poolKey（provider|model|profile|node|entry）只会 spawn 一个
// 池进程，其 ttl 取首个创建者的 cfg.DSHTTLSeconds；先跑 ttl 用例才能让短 TTL 生效于它自己独占的
// 进程，不影响后面共用同一池进程的其他场景。
func TestLiveDSH(t *testing.T) {
	if os.Getenv("YZJ_SMOKE") != "1" {
		t.Skip("set YZJ_SMOKE=1 to hit the real DSH backend")
	}
	base := liveDSHBase("live-dsh")
	if !dshLiveReady(base) {
		t.Skip("node / dsh 入口 / profile 缺失，跳过 live 冒烟")
	}
	t.Cleanup(resetPoolForTest)

	var sidA string // 场景 a/b/c 共用同一会话（同进程热路径 + 跨进程 resume）
	t.Run("ttl_reap", func(t *testing.T) {
		liveDSHTTLReap(t, liveDSHBase("live-dsh-ttl"))
	})
	t.Run("first_create_cwd", func(t *testing.T) {
		sidA = liveFirstCreateCWD(t, base)
	})
	t.Run("hot_path", func(t *testing.T) {
		liveHotPath(t, base, sidA)
	})
	t.Run("kill_resume", func(t *testing.T) {
		liveKillResume(t, base, sidA)
	})
	t.Run("dual_bot_concurrent", func(t *testing.T) {
		liveDualBotConcurrent(t, base)
	})
}

// liveDSHBase 构造真实环境 DSH 后端配置（kuaidi100 + deepseek-v4-flash，见环境事实）。
func liveDSHBase(id string) bot.Config {
	return bot.Config{
		ID:            id,
		Backend:       "dsh",
		DSHProfile:    "jsonrpc",
		DSHProvider:   "kuaidi100",
		DSHModel:      "deepseek-v4-flash",
		DSHTimeout:    120,
		DSHTTLSeconds: 60,
		DSHMaxWarm:    3,
	}
}

func newLiveDSH(t *testing.T, cfg bot.Config) bot.Backend {
	t.Helper()
	be, err := Create(cfg, nil)
	if err != nil {
		t.Fatalf("Create(%s): %v", cfg.ID, err)
	}
	return be
}

// liveRunOK 跑一轮并断言 status=ok（agent_missing/start_error 视为 spawn 失败）。
func liveRunOK(t *testing.T, be bot.Backend, prompt, ws, sid string) bot.RunResult {
	t.Helper()
	got := be.Run(prompt, bot.RunOpts{Workspace: ws, SessionID: sid, Mode: "ask"})
	switch got.Status {
	case "ok":
		return got
	case "agent_missing", "start_error":
		t.Fatalf("spawn 失败 status=%s reply=%q", got.Status, got.Reply)
	default:
		t.Fatalf("status=%s reply=%q", got.Status, got.Reply)
	}
	return got
}

// assertProbeFile 断言 probe.txt 真实落在 workspace 且内容为 want（cwd 生效验证）。
// 模型写完文件才回复，正常情况下回复时文件已就绪；留 10s 轮询兜底消除时序抖动。
func assertProbeFile(t *testing.T, ws, want string) {
	t.Helper()
	p := filepath.Join(ws, "probe.txt")
	deadline := time.Now().Add(10 * time.Second)
	var b []byte
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(p); err == nil {
			b = data
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if b == nil {
		t.Fatalf("probe.txt 未落在 workspace %s（cwd 未生效？）", ws)
	}
	if got := strings.TrimSpace(string(b)); !strings.EqualFold(got, want) {
		t.Fatalf("probe.txt 内容 = %q, want %q", got, want)
	}
}

// a) 首条创建：cwd 生效（写文件验证落点）。prompt 单行控制真实 API 成本。
func liveFirstCreateCWD(t *testing.T, base bot.Config) string {
	t.Helper()
	be := newLiveDSH(t, base)
	ws := filepath.Join(t.TempDir(), "live-a")
	sid, err := be.CreateSession()
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if strings.TrimSpace(sid) == "" {
		t.Fatal("空 session id")
	}
	got := liveRunOK(t, be, "在 "+slash(ws)+" 下写文件 probe.txt，内容 pong，然后只回复 pong", ws, sid)
	if !strings.Contains(got.Reply, "pong") {
		t.Fatalf("首条创建回复不含 pong: %q", got.Reply)
	}
	assertProbeFile(t, ws, "pong")
	return sid
}

// b) 热路径：同会话第二条走 session/prompt（进程内 known 会话），正常 ok。
func liveHotPath(t *testing.T, base bot.Config, sid string) {
	t.Helper()
	be := newLiveDSH(t, base)
	got := liveRunOK(t, be, "只回复 pong", filepath.Join(t.TempDir(), "live-b"), sid)
	if !strings.Contains(got.Reply, "pong") {
		t.Fatalf("热路径回复不含 pong: %q", got.Reply)
	}
}

// c) 杀进程 resume：先让模型记住暗号 → resetPoolForTest() 杀掉池进程（服务端会话留在磁盘）
// → 同 sid 提问，暗号须跨进程召回（记忆保持）。
func liveKillResume(t *testing.T, base bot.Config, sid string) {
	t.Helper()
	be := newLiveDSH(t, base)
	got := liveRunOK(t, be, "记住暗号：abc123，然后只回复 ok", filepath.Join(t.TempDir(), "live-c"), sid)
	if !strings.Contains(got.Reply, "ok") {
		t.Fatalf("记忆轮回复异常: %q", got.Reply)
	}
	// 杀掉池进程（优雅 shutdown + 兜底 kill 进程树）。
	resetPoolForTest()
	got = liveRunOK(t, be, "暗号是什么？只回复暗号本身", filepath.Join(t.TempDir(), "live-c2"), sid)
	if !strings.Contains(got.Reply, "abc123") {
		t.Fatalf("resume 后暗号未召回（记忆未保持）: %q", got.Reply)
	}
}

// d) 双 bot 并发：不同 cfg（不同 ID、不同 Workspace），同一池进程内两个会话并发 Run。
// 断言两路都 ok、回复不串扰、各自 probe.txt 落在各自 workspace。
func liveDualBotConcurrent(t *testing.T, base bot.Config) {
	t.Helper()
	wsA := filepath.Join(t.TempDir(), "live-dA")
	wsB := filepath.Join(t.TempDir(), "live-dB")
	cfgA, cfgB := base, base
	cfgA.ID = "live-dsh-A"
	cfgB.ID = "live-dsh-B"
	beA := newLiveDSH(t, cfgA)
	beB := newLiveDSH(t, cfgB)
	sidA, err := beA.CreateSession()
	if err != nil {
		t.Fatalf("CreateSession A: %v", err)
	}
	sidB, err := beB.CreateSession()
	if err != nil {
		t.Fatalf("CreateSession B: %v", err)
	}

	var wg sync.WaitGroup
	results := make(chan bot.RunResult, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		results <- beA.Run("在 "+slash(wsA)+" 下写文件 probe.txt，内容 AAA，然后只回复 AAA", bot.RunOpts{Workspace: wsA, SessionID: sidA, Mode: "ask"})
	}()
	go func() {
		defer wg.Done()
		results <- beB.Run("在 "+slash(wsB)+" 下写文件 probe.txt，内容 BBB，然后只回复 BBB", bot.RunOpts{Workspace: wsB, SessionID: sidB, Mode: "ask"})
	}()
	wg.Wait()
	close(results)

	var ra, rb bot.RunResult
	for r := range results {
		switch {
		case strings.Contains(r.Reply, "AAA"):
			ra = r
		case strings.Contains(r.Reply, "BBB"):
			rb = r
		default:
			t.Fatalf("并发轮回复无特征标记: status=%s reply=%q", r.Status, r.Reply)
		}
	}
	if ra.Status != "ok" || rb.Status != "ok" {
		t.Fatalf("并发轮 status: A=%s B=%s", ra.Status, rb.Status)
	}
	if strings.Contains(ra.Reply, "BBB") || strings.Contains(rb.Reply, "AAA") {
		t.Fatalf("双 bot 回复串扰: A=%q B=%q", ra.Reply, rb.Reply)
	}
	assertProbeFile(t, wsA, "AAA")
	assertProbeFile(t, wsB, "BBB")
}

// e) TTL 回收：短 TTL 进程 idle 超 TTL 后，dshReap() 强制一轮回收 → 池 map 清空、进程退出。
// 必须最先运行（见 TestLiveDSH 注释）。maxWarm 只做存在性轻验（深测跳过）。
//
// 注意：真实 DSH agent 在 turn 结束后还会异步生成 session/title（一次额外 LLM 调用，
// 3~8s 后到帧），期间 lastActive 会被刷新。idle 计时必须等这些尾部事件全部结束、进程
// 真正静止后才开始，否则「idle 超 TTL」是假象。这里轮询等进程连续 idle >= TTL 再 reap。
func liveDSHTTLReap(t *testing.T, cfg bot.Config) {
	t.Helper()
	resetPoolForTest() // 隔离：本用例独占池，让短 TTL 生效于它 spawn 的进程
	cfg.DSHTTLSeconds = 10
	be := newLiveDSH(t, cfg)
	ws := filepath.Join(t.TempDir(), "live-ttl")
	sid, err := be.CreateSession()
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	d := be.(*DSH)
	key := d.poolKey(d.resolveProvider(), d.resolveModel(""))
	got := liveRunOK(t, be, "只回复 pong", ws, sid)
	if !strings.Contains(got.Reply, "pong") {
		t.Fatalf("回复不含 pong: %q", got.Reply)
	}
	p := dshPoolGet(key)
	if p == nil {
		t.Fatalf("池中找不到进程 %s", key)
	}

	// maxWarm 存在性轻验：spawn 后池级 maxWarm 应为配置值。
	dshPoolMu.Lock()
	warm := dshPoolWarm
	dshPoolMu.Unlock()
	if warm < cfg.DSHMaxWarm {
		t.Fatalf("dshPoolWarm=%d < cfg.DSHMaxWarm=%d", warm, cfg.DSHMaxWarm)
	}

	// 等进程连续 idle 超过 TTL（reaper 周期 30s，测试直接调同包 dshReap() 强制一轮回收）。
	ttl := time.Duration(cfg.DSHTTLSeconds) * time.Second
	deadline := time.Now().Add(90 * time.Second)
	for {
		p.mu.Lock()
		idle := time.Since(p.lastActive)
		busy := len(p.inflight) > 0
		p.mu.Unlock()
		if !busy && idle >= ttl {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("等进程 idle 超 TTL 超时: busy=%v idle=%v ttl=%v", busy, idle, ttl)
		}
		time.Sleep(500 * time.Millisecond)
	}
	dshReap()

	if dshPoolGet(key) != nil {
		t.Fatalf("reap 后进程仍在池中（key=%s）", key)
	}
	select {
	case <-p.deadCh:
		// 进程已退出（readLoop 已 cmd.Wait 回收资源）
	case <-time.After(10 * time.Second):
		t.Fatal("reap 后进程未退出（内存未归还？）")
	}
	if !p.isDead() {
		t.Fatal("reap 后进程未标记 dead")
	}
}

// slash 把 Windows 路径转正斜杠，避免模型在 bash 里把反斜杠当转义符。
func slash(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}
