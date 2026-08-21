package backends

import (
	"os/exec"
	"strings"

	"yzj-bridge/internal/bot"
)

// BackendAvailable 描述一个后端引擎的可用性，供 GUI 机器人表单过滤后端下拉。
type BackendAvailable struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// AvailableBackends 基于配置 defaults 判定各后端引擎是否已配置可用。
func AvailableBackends(defs map[string]any) []BackendAvailable {
	str := func(k string) string {
		if v, ok := defs[k].(string); ok {
			return strings.TrimSpace(v)
		}
		return ""
	}
	items := []BackendAvailable{
		{ID: "cursor_cli", Label: "Cursor CLI"},
		{ID: "claude_code", Label: "Claude Code"},
		{ID: "openai", Label: "OpenAI 兼容"},
		{ID: "dsh", Label: "DSH（DeepSeek Harness）"},
		{ID: "opencode", Label: "OpenCode"},
	}
	byID := make(map[string]*BackendAvailable, len(items))
	for i := range items {
		byID[items[i].ID] = &items[i]
	}

	if DiscoverCLI("cursor", str("cursor_bin")).Found {
		byID["cursor_cli"].Available = true
	} else {
		byID["cursor_cli"].Reason = "未找到可执行文件"
	}
	if DiscoverCLI("claude", str("claude_bin")).Found {
		byID["claude_code"].Available = true
	} else {
		byID["claude_code"].Reason = "未找到可执行文件"
	}
	if str("openai_base_url") != "" {
		byID["openai"].Available = true
	} else {
		byID["openai"].Reason = "未配置 Base URL"
	}

	// dsh：node 可执行 且 DSH 入口可解析并存在。
	nodeBin := resolveNodeBin(str("node_bin"))
	entry := resolveDSHEntry(bot.Config{
		NodeBin:  str("node_bin"),
		DSHEntry: str("dsh_entry"),
		DSHHome:  str("dsh_home"),
	})
	if nodeBin != "" && entry != "" && nodeResolvable(nodeBin) && cliExists(entry) {
		byID["dsh"].Available = true
	} else {
		byID["dsh"].Reason = "未找到 DSH 入口或 Node"
	}

	// opencode 占位后端，尚未实现：固定不可用。
	byID["opencode"].Reason = "占位后端，尚未实现"
	return items
}

// nodeResolvable 判断 node 可执行文件可用：绝对路径直接 stat，裸名走 LookPath。
func nodeResolvable(bin string) bool {
	if cliExists(bin) {
		return true
	}
	if _, err := exec.LookPath(bin); err == nil {
		return true
	}
	return false
}
