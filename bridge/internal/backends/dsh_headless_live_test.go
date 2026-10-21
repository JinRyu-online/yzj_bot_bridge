package backends

// dsh_headless_live_test.go — 真实 DSH 桌面客户端 headless CLI 联调。
//
//	$env:YZJ_DSH_HEADLESS="1"; go test ./internal/backends/ -count=1 -run TestLiveDSHHeadless -v
//
// 覆盖：自动探测桌面客户端 → 生成 yzj-headless profile → headless CLI 首轮创建 →
// 用返回的 session id 续跑第二轮（暗号记忆）→ 工作目录落在 workspace。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yzj-bridge/internal/bot"
)

func TestLiveDSHHeadless(t *testing.T) {
	if os.Getenv("YZJ_DSH_HEADLESS") != "1" {
		t.Skip("set YZJ_DSH_HEADLESS=1 to hit the real DSH desktop headless CLI")
	}
	cfg := bot.Config{
		ID:            "live-dsh-headless",
		Backend:       "dsh",
		DSHMode:       "headless",
		DSHProvider:   "kuaidi100",
		DSHModel:      "deepseek-flash",
		DSHTimeout:    180,
		DSHTTLSeconds: 120,
		DSHMaxWarm:    1,
	}
	rt, err := resolveDSHRuntime(cfg)
	if err != nil || !rt.Headless {
		t.Skipf("DSH 桌面客户端 headless runtime 不可用: rt=%+v err=%v", rt, err)
	}
	resetPoolForTest()
	t.Cleanup(resetPoolForTest)

	be, err := Create(cfg, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	ws := filepath.Join(t.TempDir(), "live-headless")
	res := be.Run("记住暗号 abc123，然后只回复 ok", bot.RunOpts{Workspace: ws, Mode: "ask"})
	if res.Status != "ok" || !strings.Contains(strings.ToLower(res.Reply), "ok") {
		t.Fatalf("首轮 status=%s reply=%q", res.Status, res.Reply)
	}
	sid := strings.TrimSpace(res.SessionID)
	if !strings.HasPrefix(sid, "session-") {
		t.Fatalf("headless 未返回 session id: %q", sid)
	}

	// 同 session 续跑：必须能召回暗号，证明 --session-id 生效。
	res = be.Run("暗号是什么？只回复暗号本身", bot.RunOpts{Workspace: ws, SessionID: sid, Mode: "ask"})
	if res.Status != "ok" || !strings.Contains(res.Reply, "abc123") {
		t.Fatalf("续跑 status=%s reply=%q", res.Status, res.Reply)
	}
}
