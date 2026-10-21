package backends

// dsh_desktop_live_test.go — 真实 DSH 桌面客户端 runtime 联调（桌面模式）。
//
//	$env:YZJ_DSH_DESKTOP="1"; go test ./internal/backends/ -count=1 -run TestLiveDSHDesktop -v
//
// 前置：本机安装并启动过 DeepSeek Harness（桌面客户端），且桌面里已配置模型 provider。
// 测试会：自动探测桌面客户端 → 生成 yzj-sdk profile → Electron-as-Node 启动
// @deepseek-ai/dsh-sdk-jsonrpc-server → 跑两轮（创建 + 热路径）→ 断言回复与 cwd。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yzj-bridge/internal/bot"
)

func TestLiveDSHDesktop(t *testing.T) {
	if os.Getenv("YZJ_DSH_DESKTOP") != "1" {
		t.Skip("set YZJ_DSH_DESKTOP=1 to hit the real DSH desktop runtime")
	}
	cfg := bot.Config{
		ID:            "live-dsh-desktop",
		Backend:       "dsh",
		DSHMode:       "desktop",
		DSHProvider:   "kuaidi100",
		DSHModel:      "deepseek-flash",
		DSHTimeout:    180,
		DSHTTLSeconds: 120,
		DSHMaxWarm:    3,
	}
	if !dshLiveReady(cfg) {
		t.Skip("DSH 桌面客户端 / 生成 profile 不可用，跳过 live 冒烟")
	}
	resetPoolForTest()
	t.Cleanup(resetPoolForTest)

	be, err := Create(cfg, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	sid, err := be.CreateSession()
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	ws := filepath.Join(t.TempDir(), "live-desktop")
	got := be.Run("只回复 pong，不要任何其他内容", bot.RunOpts{Workspace: ws, SessionID: sid, Mode: "ask"})
	if got.Status != "ok" || !strings.Contains(strings.ToLower(got.Reply), "pong") {
		t.Fatalf("首轮 status=%s reply=%q", got.Status, got.Reply)
	}

	// 热路径：同进程同会话第二条，验证 session/prompt 命中活 agent。
	got = be.Run("刚才你说的是什么？只回复 pong", bot.RunOpts{Workspace: ws, SessionID: sid, Mode: "ask"})
	if got.Status != "ok" || !strings.Contains(strings.ToLower(got.Reply), "pong") {
		t.Fatalf("热路径 status=%s reply=%q", got.Status, got.Reply)
	}

	// 跨进程 resume：杀池进程后同 sid 继续，验证官方 session/resume 可用。
	resetPoolForTest()
	got = be.Run("只回复 pong", bot.RunOpts{Workspace: ws, SessionID: sid, Mode: "ask"})
	if got.Status != "ok" || !strings.Contains(strings.ToLower(got.Reply), "pong") {
		t.Fatalf("resume status=%s reply=%q", got.Status, got.Reply)
	}
}
