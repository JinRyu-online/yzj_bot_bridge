package backends

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestAvailableBackends 构造 defaults map 断言各引擎 available 标志：
// cursor/claude 指向临时假可执行 → 可用；openai 有 base_url 无 key → 可用；
// dsh 无 node/dsh → 不可用；opencode 占位 → 固定不可用。
func TestAvailableBackends(t *testing.T) {
	dir := t.TempDir()
	fakeBin := func(name string) string {
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	defs := map[string]any{
		"cursor_bin":      fakeBin("agent"),
		"claude_bin":      fakeBin("claude"),
		"openai_base_url": "https://example.com/v1",
		"node_bin":        filepath.Join(dir, "no-node.exe"),
		"dsh_home":        filepath.Join(dir, "no-dsh"),
		// 强制 cli 模式：避免测试机装了 DSH 桌面客户端时默认 desktop 模式把它判为可用。
		"dsh_mode": "cli",
	}
	items := AvailableBackends(defs)
	byID := map[string]BackendAvailable{}
	for _, it := range items {
		if _, dup := byID[it.ID]; dup {
			t.Fatalf("duplicate id %s", it.ID)
		}
		byID[it.ID] = it
	}
	if len(byID) != 5 {
		t.Fatalf("len=%d want 5: %+v", len(byID), items)
	}
	if !byID["cursor_cli"].Available {
		t.Fatalf("cursor_cli should be available: %+v", byID["cursor_cli"])
	}
	if !byID["claude_code"].Available {
		t.Fatalf("claude_code should be available: %+v", byID["claude_code"])
	}
	if !byID["openai"].Available {
		t.Fatalf("openai with base_url only should be available: %+v", byID["openai"])
	}
	if byID["dsh"].Available {
		t.Fatalf("dsh without node/dsh should be unavailable: %+v", byID["dsh"])
	}
	if byID["dsh"].Reason == "" {
		t.Fatal("dsh reason required when unavailable")
	}
	if byID["opencode"].Available {
		t.Fatal("opencode placeholder must be unavailable")
	}
	if byID["opencode"].Reason != "占位后端，尚未实现" {
		t.Fatalf("opencode reason=%q", byID["opencode"].Reason)
	}
}

func TestAvailableBackendsOpenAIRequiresBaseURL(t *testing.T) {
	items := AvailableBackends(map[string]any{})
	byID := map[string]BackendAvailable{}
	for _, it := range items {
		byID[it.ID] = it
	}
	if byID["openai"].Available {
		t.Fatal("openai without base_url must be unavailable")
	}
	if byID["openai"].Reason == "" {
		t.Fatal("openai reason required when unavailable")
	}
}

// TestAvailableBackendsDSHDesktop 验证 desktop/headless 模式下探测桌面客户端即视为可用。
func TestAvailableBackendsDSHDesktop(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "resources"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "DeepSeek Harness.exe"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "resources", "app.asar"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	items := AvailableBackends(map[string]any{
		"dsh_mode":         "headless",
		"dsh_desktop_path": dir,
	})
	for _, it := range items {
		if it.ID == "dsh" {
			if !it.Available {
				t.Fatalf("dsh desktop should be available: %+v", it)
			}
			return
		}
	}
	t.Fatal("dsh item missing")
}
