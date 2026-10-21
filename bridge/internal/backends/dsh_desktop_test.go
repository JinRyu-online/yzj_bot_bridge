package backends

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yzj-bridge/internal/bot"
)

// TestDetectDSHDesktopAtExplicitPath 验证桌面客户端探测：exe + resources/app.asar 才算命中。
func TestDetectDSHDesktopAtExplicitPath(t *testing.T) {
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
	exe, installDir, err := detectDSHDesktop(dir)
	if err != nil {
		t.Fatalf("detectDSHDesktop: %v", err)
	}
	if exe != filepath.Join(dir, "DeepSeek Harness.exe") || installDir != dir {
		t.Fatalf("exe=%q dir=%q", exe, installDir)
	}

	// 显式 exe 路径也能反推安装目录。
	exe2, dir2, err := detectDSHDesktop(filepath.Join(dir, "DeepSeek Harness.exe"))
	if err != nil || exe2 != exe || dir2 != dir {
		t.Fatalf("explicit exe: exe=%q dir=%q err=%v", exe2, dir2, err)
	}
}

// TestEnsureDesktopSDKProfile 验证桥自动生成的 profile：复制桌面 provider 配置、
// 使用官方 sdk-jsonrpc-server 行、并把桥默认模型补进模型列表。
func TestEnsureDesktopSDKProfile(t *testing.T) {
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
		ID:             "fairy",
		Backend:        "dsh",
		DSHMode:        "desktop",
		DSHHome:        home,
		DSHProvider:    "kuaidi100",
		DSHModel:       "deepseek-v4-flash",
		DSHProfile:     dshManagedSDKProfile,
		DSHDesktopPath: "",
	}
	if err := ensureDesktopSDKProfile(cfg, dshManagedSDKProfile); err != nil {
		t.Fatalf("ensureDesktopSDKProfile: %v", err)
	}
	patch, err := os.ReadFile(filepath.Join(home, "profiles", dshManagedSDKProfile, "cordis.patch.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(patch)
	for _, want := range []string{
		"@deepseek-ai/dsh-sdk-jsonrpc-server",
		"kuaidi100",
		"deepseek-flash",
		"deepseek-v4-flash",
		"danger-full-access",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("profile 缺少 %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "@bridge/dsh-jsonrpc-resume") {
		t.Fatalf("profile 不应再使用旧 resume 插件:\n%s", text)
	}

	pkg, err := os.ReadFile(filepath.Join(home, "profiles", dshManagedSDKProfile, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pkg), `"@deepseek-ai/dsh-base"`) {
		t.Fatalf("package.json 缺少 dsh-base: %s", pkg)
	}

	// 幂等：第二次调用不应改变已生成内容。
	if err := ensureDesktopSDKProfile(cfg, dshManagedSDKProfile); err != nil {
		t.Fatal(err)
	}
	patch2, _ := os.ReadFile(filepath.Join(home, "profiles", dshManagedSDKProfile, "cordis.patch.yml"))
	if string(patch2) != text {
		t.Fatal("ensureDesktopSDKProfile 非幂等")
	}
}

// TestEnsureDesktopSDKProfileMissingProvider 验证桌面客户端尚未配置模型时给出可操作错误。
func TestEnsureDesktopSDKProfileMissingProvider(t *testing.T) {
	home := t.TempDir()
	desktopDir := filepath.Join(home, "profiles", "desktop")
	if err := os.MkdirAll(desktopDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(desktopDir, "cordis.patch.yml"), []byte("- id: llm-pi-ai\n  config:\n    providers: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := bot.Config{DSHHome: home, DSHProvider: "kuaidi100"}
	if err := ensureDesktopSDKProfile(cfg, dshManagedSDKProfile); err == nil {
		t.Fatal("expected error when desktop provider is missing")
	}
}

// TestDshSessionMissing 验证“会话不存在”判定，避免把磁盘格式错误也当 not-found 静默重建。
func TestDshSessionMissing(t *testing.T) {
	if !dshSessionMissing(errString("unknown SDK session: \"x\"")) {
		t.Fatal("unknown SDK session 应判定为不存在")
	}
	if !dshSessionMissing(errString("session \"x\" not found")) {
		t.Fatal("not found 应判定为不存在")
	}
	if dshSessionMissing(errString("failed to parse session header")) {
		t.Fatal("解析错误不应静默重建会话")
	}
	if dshSessionMissing(nil) {
		t.Fatal("nil 不应判定为不存在")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
