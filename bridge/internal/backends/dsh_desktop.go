package backends

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"yzj-bridge/internal/bot"
)

// DSH 桌面客户端（DeepSeek Harness Electron 应用）集成。
//
// 桌面客户端自带完整 dsh runtime（app.asar/dsh/node_modules），桥不再要求本机
// 单独安装 node + dsh CLI。两种使用方式：
//   - headless（默认）：直接执行桌面客户端的 headless CLI（dsh --profile headless），
//     一条消息一次进程，输出 NDJSON（session / text / final / status）；会话用
//     --session-id 续跑。这是真正“用桌面客户端自带的 headless cli 执行”。
//   - sdk：常驻 @deepseek-ai/dsh-sdk-jsonrpc-server 进程池（stdio JSON-RPC），
//     适合需要共享进程/低延迟的多轮场景。
//
// 两种方式都由桥自动生成并维护 profile（yzj-headless / yzj-sdk）：复制桌面
// profile 的模型 provider 配置，不需要用户手工部署。

const (
	dshModeHeadless = "headless"
	dshModeSDK      = "sdk"
	dshModeCLI      = "cli"

	// 桥自动生成并维护的 profile 名。
	dshManagedHeadlessProfile = "yzj-headless"
	dshManagedSDKProfile      = "yzj-sdk"
)

// dshRuntime 是一次 DSH 运行的实际启动方式。
type dshRuntime struct {
	NodeBin        string
	Entry          string
	Profile        string
	ElectronAsNode bool
	Desktop        bool
	Headless       bool
}

// resolveDSHRuntime 根据 dsh_mode 选择桌面 headless CLI / 桌面 SDK server / 旧 CLI 入口。
func resolveDSHRuntime(cfg bot.Config) (dshRuntime, error) {
	mode := strings.ToLower(strings.TrimSpace(cfg.DSHMode))
	switch mode {
	case "", "auto", "desktop", dshModeHeadless:
		mode = dshModeHeadless
	case dshModeSDK:
	case dshModeCLI:
	default:
		return dshRuntime{}, fmt.Errorf("dsh_mode=%q 无效（可选 headless / sdk / cli）", cfg.DSHMode)
	}
	profile := strings.TrimSpace(cfg.DSHProfile)

	if mode == dshModeCLI {
		return dshRuntime{
			NodeBin: resolveNodeBin(cfg.NodeBin),
			Entry:   resolveDSHEntry(cfg),
			Profile: defaultCLIProfile(profile),
		}, nil
	}

	exe, dir, err := detectDSHDesktop(cfg.DSHDesktopPath)
	if err != nil {
		// 桌面客户端没装时保留旧入口回退，避免已有部署完全不可用。
		if strings.TrimSpace(cfg.DSHEntry) != "" {
			log.Printf("dsh: %v；回退到 CLI 入口 %s（dsh_mode=cli 可消除该告警）", err, cfg.DSHEntry)
			return dshRuntime{
				NodeBin: resolveNodeBin(cfg.NodeBin),
				Entry:   resolveDSHEntry(cfg),
				Profile: defaultCLIProfile(profile),
			}, nil
		}
		return dshRuntime{}, err
	}

	entry := filepath.Join(dir, "resources", "app.asar", "dsh", "node_modules", "@deepseek-ai", "dsh", "lib", "bin.js")
	rt := dshRuntime{
		NodeBin:        exe,
		Entry:          entry,
		ElectronAsNode: true,
		Desktop:        true,
	}
	if mode == dshModeHeadless {
		if profile == "" || profile == "jsonrpc" || profile == dshManagedSDKProfile || profile == "headless" {
			profile = dshManagedHeadlessProfile
		}
		if profile == dshManagedHeadlessProfile {
			if err := ensureHeadlessProfile(cfg, profile); err != nil {
				return dshRuntime{}, err
			}
		} else if !fileExists(filepath.Join(dshHome(cfg), "profiles", profile, "package.json")) {
			return dshRuntime{}, fmt.Errorf("dsh profile %q 不存在（%s）", profile, filepath.Join(dshHome(cfg), "profiles", profile))
		}
		rt.Profile = profile
		rt.Headless = true
		return rt, nil
	}

	// sdk：常驻官方 SDK JSON-RPC server。
	if profile == "" || profile == "jsonrpc" || profile == dshManagedHeadlessProfile {
		profile = dshManagedSDKProfile
	}
	if profile == dshManagedSDKProfile {
		if err := ensureDesktopSDKProfile(cfg, profile); err != nil {
			return dshRuntime{}, err
		}
	} else if !fileExists(filepath.Join(dshHome(cfg), "profiles", profile, "package.json")) {
		return dshRuntime{}, fmt.Errorf("dsh profile %q 不存在（%s）", profile, filepath.Join(dshHome(cfg), "profiles", profile))
	}
	rt.Profile = profile
	return rt, nil
}

func defaultCLIProfile(profile string) string {
	if strings.TrimSpace(profile) == "" {
		return "jsonrpc"
	}
	return profile
}

func (r dshRuntime) key(provider, model string) dshPoolKey {
	return dshPoolKey{
		Provider:       provider,
		Model:          model,
		Profile:        r.Profile,
		NodeBin:        r.NodeBin,
		DSHEntry:       r.Entry,
		ElectronAsNode: r.ElectronAsNode,
	}
}

// detectDSHDesktop 在显式路径、常见安装目录与卸载注册表中查找 DeepSeek Harness。
func detectDSHDesktop(explicit string) (exe, dir string, err error) {
	for _, d := range desktopInstallCandidates(explicit) {
		d = strings.TrimSpace(strings.Trim(d, `"`))
		if d == "" {
			continue
		}
		candidateExe := d
		if !strings.EqualFold(filepath.Ext(d), ".exe") {
			candidateExe = filepath.Join(d, "DeepSeek Harness.exe")
		}
		installDir := filepath.Dir(candidateExe)
		if fileExists(candidateExe) && fileExists(filepath.Join(installDir, "resources", "app.asar")) {
			return candidateExe, installDir, nil
		}
	}
	return "", "", fmt.Errorf("未找到 DSH 桌面客户端（DeepSeek Harness.exe / resources\\app.asar）；请安装 DeepSeek Harness，或在设置中填写 dsh_desktop_path")
}

func desktopInstallCandidates(explicit string) []string {
	var out []string
	if e := strings.TrimSpace(explicit); e != "" {
		out = append(out, e)
		if !strings.EqualFold(filepath.Ext(e), ".exe") {
			out = append(out, filepath.Dir(e))
		}
	}
	for _, env := range []string{"LOCALAPPDATA", "ProgramFiles", "ProgramW6432", "ProgramFiles(x86)"} {
		if root := strings.TrimSpace(os.Getenv(env)); root != "" {
			out = append(out, filepath.Join(root, "Programs", "DeepSeek Harness"), filepath.Join(root, "DeepSeek Harness"))
		}
	}
	out = append(out, registryDSHDesktopDirs()...)

	seen := map[string]bool{}
	deduped := out[:0]
	for _, d := range out {
		clean := filepath.Clean(strings.Trim(strings.TrimSpace(d), `"`))
		if clean == "" || clean == "." || seen[strings.ToLower(clean)] {
			continue
		}
		seen[strings.ToLower(clean)] = true
		deduped = append(deduped, clean)
	}
	return deduped
}

// ensureDesktopSDKProfile 生成/更新桌面 SDK server profile（yzj-sdk）。
func ensureDesktopSDKProfile(cfg bot.Config, profile string) error {
	entries, err := desktopBaseEntries(cfg)
	if err != nil {
		return err
	}
	entries = append(entries,
		map[string]any{
			"id": "system-prompt",
			"config": map[string]any{
				"persona": "You are a coding agent powered by the {{model}} model. Your working directory is {{cwd}}. Answer concisely and factually.",
			},
		},
		map[string]any{"id": "hmr", "disabled": true},
		map[string]any{
			"insert": []any{
				map[string]any{"id": "sdk-jsonrpc-server", "name": "@deepseek-ai/dsh-sdk-jsonrpc-server"},
			},
		},
	)
	return writeManagedProfile(cfg, profile,
		[]string{"@deepseek-ai/dsh-base"},
		entries,
		"# Generated by yzj-bridge for the DSH desktop SDK runtime. Do not edit.\n")
}

// ensureHeadlessProfile 生成/更新桌面 headless CLI profile（yzj-headless）。
// headless bundle 默认禁用 credentials（一次性任务用 launch-time credentials）；
// 我们显式重新启用，让 headless 能读桌面客户端写入的 ~/.dsh/.credentials.yaml。
func ensureHeadlessProfile(cfg bot.Config, profile string) error {
	entries, err := desktopBaseEntries(cfg)
	if err != nil {
		return err
	}
	entries = append([]map[string]any{{"id": "credentials", "disabled": false}}, entries...)
	entries = append(entries, map[string]any{"id": "hmr", "disabled": true})
	return writeManagedProfile(cfg, profile,
		[]string{"@deepseek-ai/dsh-base", "@deepseek-ai/dsh-headless"},
		entries,
		"# Generated by yzj-bridge for the DSH desktop headless CLI. Do not edit.\n")
}

// desktopBaseEntries 从桌面 profile 复制模型 provider / 权限配置，并补齐桥默认模型。
func desktopBaseEntries(cfg bot.Config) ([]map[string]any, error) {
	home := dshHome(cfg)
	desktopPatch := filepath.Join(home, "profiles", "desktop", "cordis.patch.yml")
	data, err := os.ReadFile(desktopPatch)
	if err != nil {
		return nil, fmt.Errorf("读取 DSH 桌面客户端配置失败（%s）：%w；请先启动一次 DSH 桌面客户端并配置模型", desktopPatch, err)
	}
	var entries []map[string]any
	if err := yaml.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("解析 DSH 桌面客户端配置失败（%s）：%w", desktopPatch, err)
	}
	var kept []map[string]any
	for _, e := range entries {
		switch fmt.Sprint(e["id"]) {
		case "permission", "llm-pi-ai", "agent-default-model":
			kept = append(kept, e)
		}
	}
	if !hasPatchID(kept, "llm-pi-ai") {
		return nil, fmt.Errorf("DSH 桌面客户端配置缺少 llm-pi-ai 模型 provider（%s）；请先在桌面客户端里配置模型", desktopPatch)
	}
	if err := ensureProviderModel(kept, cfg); err != nil {
		return nil, err
	}
	applyDSHAgentReasoning(kept, cfg)
	return kept, nil
}

var dshProfileWriteMu sync.Mutex

func writeManagedProfile(cfg bot.Config, profile string, bundles []string, entries []map[string]any, header string) error {
	// 多个 bot/GUI 轮次可能同时生成 profile；共享 tmp + Windows Rename 必须串行。
	dshProfileWriteMu.Lock()
	defer dshProfileWriteMu.Unlock()
	patch, err := yaml.Marshal(entries)
	if err != nil {
		return err
	}
	patch = append([]byte(header), patch...)

	dir := filepath.Join(dshHome(cfg), "profiles", profile)
	pkg := map[string]any{
		"name":    "dsh-profile-" + profile,
		"private": true,
		"dsh": map[string]any{
			"profile": map[string]any{"bundles": bundles},
		},
	}
	pkgData, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return err
	}
	pkgData = append(pkgData, '\n')

	if err := writeFileIfChanged(filepath.Join(dir, "package.json"), pkgData, 0o644); err != nil {
		return err
	}
	return writeFileIfChanged(filepath.Join(dir, "cordis.patch.yml"), patch, 0o644)
}

func hasPatchID(entries []map[string]any, id string) bool {
	for _, e := range entries {
		if fmt.Sprint(e["id"]) == id {
			return true
		}
	}
	return false
}

// ensureProviderModel 确保当前 provider 至少包含桥可能用到的模型 id。
func ensureProviderModel(entries []map[string]any, cfg bot.Config) error {
	provider := strings.TrimSpace(cfg.DSHProvider)
	if provider == "" {
		provider = "kuaidi100"
	}
	var llm map[string]any
	for _, e := range entries {
		if fmt.Sprint(e["id"]) == "llm-pi-ai" {
			cfgMap, _ := e["config"].(map[string]any)
			if cfgMap == nil {
				return fmt.Errorf("llm-pi-ai 配置为空")
			}
			llm = cfgMap
			break
		}
	}
	if llm == nil {
		return fmt.Errorf("llm-pi-ai 配置为空")
	}
	providers, _ := llm["providers"].(map[string]any)
	if providers == nil {
		return fmt.Errorf("llm-pi-ai.providers 为空")
	}
	prov, _ := providers[provider].(map[string]any)
	if prov == nil {
		return fmt.Errorf("DSH 桌面客户端未配置 provider %q；请先在桌面客户端里配置该模型 provider", provider)
	}
	models, _ := prov["models"].([]any)

	want := []string{}
	for _, m := range []string{cfg.Model, cfg.DSHModel, "deepseek-v4-flash"} {
		if m = strings.TrimSpace(m); m != "" {
			want = append(want, m)
		}
	}
	for _, id := range want {
		if modelListed(models, id) {
			continue
		}
		input := []any{"text"}
		if strings.Contains(strings.ToLower(id), "vision") {
			input = []any{"text", "image"}
		}
		models = append(models, map[string]any{"id": id, "name": id, "input": input})
	}
	applyDSHModelReasoning(models, cfg.DSHReasoningEffort)
	prov["models"] = models
	return nil
}

func modelListed(models []any, id string) bool {
	for _, m := range models {
		switch v := m.(type) {
		case map[string]any:
			if fmt.Sprint(v["id"]) == id {
				return true
			}
		case string:
			if v == id {
				return true
			}
		}
	}
	return false
}

func writeFileIfChanged(path string, data []byte, perm os.FileMode) error {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
