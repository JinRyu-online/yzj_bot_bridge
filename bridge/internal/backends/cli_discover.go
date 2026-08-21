package backends

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// CLIDiscoverResult is the payload for GUI auto-scan of Cursor/Claude CLIs.
type CLIDiscoverResult struct {
	Engine     string   `json:"engine"` // cursor | claude
	Found      bool     `json:"found"`
	Path       string   `json:"path,omitempty"`
	Version    string   `json:"version,omitempty"`
	Candidates []string `json:"candidates,omitempty"`
	Install    *CLIInstallHint `json:"install,omitempty"`
	Message    string   `json:"message,omitempty"`
}

// CLIInstallHint tells the GUI how to help the user install a missing CLI.
type CLIInstallHint struct {
	Shell   string `json:"shell"`   // powershell | bash
	Command string `json:"command"` // official one-liner
	Hint    string `json:"hint"`
}

// DiscoverCLI looks for cursor (agent) or claude on PATH and common install dirs.
// configured is the current config value (may be a bare name or absolute path).
func DiscoverCLI(engine, configured string) CLIDiscoverResult {
	engine = strings.ToLower(strings.TrimSpace(engine))
	configured = strings.TrimSpace(configured)
	out := CLIDiscoverResult{Engine: engine}

	var names []string
	var candidates []string
	switch engine {
	case "cursor", "cursor_cli", "agent":
		out.Engine = "cursor"
		names = []string{"agent", "cursor-agent"}
		if configured != "" {
			names = append([]string{configured}, names...)
		}
		candidates = cursorInstallCandidates()
		out.Install = cursorInstallHint()
	case "claude", "claude_code":
		out.Engine = "claude"
		names = []string{"claude"}
		if configured != "" {
			names = append([]string{configured}, names...)
		}
		candidates = claudeInstallCandidates()
		out.Install = claudeInstallHint()
	default:
		out.Message = "unknown engine: " + engine
		return out
	}

	seen := map[string]struct{}{}
	try := func(p string) bool {
		p = strings.TrimSpace(p)
		if p == "" {
			return false
		}
		if runtime.GOOS == "windows" {
			p = resolveWindowsBin(p, namesWithExe(names)...)
		}
		key := strings.ToLower(filepath.Clean(p))
		if _, ok := seen[key]; ok {
			return false
		}
		seen[key] = struct{}{}
		if !cliExists(p) {
			// Bare name: try LookPath
			if !filepath.IsAbs(p) && !strings.ContainsAny(p, `/\`) {
				if lp, err := exec.LookPath(p); err == nil && cliExists(lp) {
					p = lp
					if runtime.GOOS == "windows" {
						p = resolveWindowsBin(p, namesWithExe(names)...)
					}
					key = strings.ToLower(filepath.Clean(p))
					if _, ok := seen[key]; ok {
						return false
					}
					seen[key] = struct{}{}
				} else {
					return false
				}
			} else {
				return false
			}
		}
		out.Candidates = append(out.Candidates, p)
		if !out.Found {
			out.Found = true
			out.Path = p
			out.Version = probeCLIVersion(engine, p)
			out.Message = "已找到可执行文件"
		}
		return true
	}

	for _, n := range names {
		_ = try(n)
	}
	for _, c := range candidates {
		_ = try(c)
	}

	if !out.Found {
		out.Message = "未在 PATH 或常见安装目录找到，可一键打开终端安装"
	}
	return out
}

func namesWithExe(names []string) []string {
	out := make([]string, 0, len(names)*2)
	for _, n := range names {
		base := filepath.Base(n)
		base = strings.TrimSuffix(base, filepath.Ext(base))
		out = append(out, base+".exe", base+".cmd")
	}
	return out
}

func cliExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func cursorInstallCandidates() []string {
	home, _ := os.UserHomeDir()
	local := os.Getenv("LOCALAPPDATA")
	var out []string
	if local != "" {
		out = append(out,
			filepath.Join(local, "cursor-agent", "agent.exe"),
			filepath.Join(local, "cursor-agent", "agent.cmd"),
			filepath.Join(local, "cursor-agent", "cursor-agent.exe"),
			filepath.Join(local, "cursor-agent", "cursor-agent.cmd"),
		)
	}
	if home != "" {
		out = append(out,
			filepath.Join(home, ".local", "bin", "agent"),
			filepath.Join(home, ".local", "bin", "agent.exe"),
			filepath.Join(home, ".local", "bin", "cursor-agent"),
			filepath.Join(home, ".local", "bin", "cursor-agent.exe"),
			filepath.Join(home, ".cursor", "bin", "agent"),
			filepath.Join(home, ".cursor", "bin", "agent.exe"),
		)
	}
	return out
}

func claudeInstallCandidates() []string {
	home, _ := os.UserHomeDir()
	local := os.Getenv("LOCALAPPDATA")
	roaming := os.Getenv("APPDATA")
	var out []string
	if home != "" {
		out = append(out,
			filepath.Join(home, ".local", "bin", "claude"),
			filepath.Join(home, ".local", "bin", "claude.exe"),
			filepath.Join(home, ".claude", "local", "claude"),
			filepath.Join(home, ".claude", "local", "claude.exe"),
		)
	}
	if local != "" {
		out = append(out,
			filepath.Join(local, "claude", "claude.exe"),
			filepath.Join(local, "Programs", "Claude Code", "claude.exe"),
		)
	}
	if roaming != "" {
		out = append(out,
			filepath.Join(roaming, "npm", "claude.cmd"),
			filepath.Join(roaming, "npm", "claude.exe"),
			filepath.Join(roaming, "npm", "node_modules", "@anthropic-ai", "claude-code", "bin", "claude.exe"),
		)
	}
	return out
}

func cursorInstallHint() *CLIInstallHint {
	if runtime.GOOS == "windows" {
		return &CLIInstallHint{
			Shell:   "powershell",
			Command: `irm 'https://cursor.com/install?win32=true' | iex`,
			Hint:    "将打开 PowerShell，确认后执行 Cursor 官方安装脚本",
		}
	}
	return &CLIInstallHint{
		Shell:   "bash",
		Command: `curl https://cursor.com/install -fsS | bash`,
		Hint:    "将打开终端，确认后执行 Cursor 官方安装脚本",
	}
}

func claudeInstallHint() *CLIInstallHint {
	if runtime.GOOS == "windows" {
		return &CLIInstallHint{
			Shell:   "powershell",
			Command: `irm https://claude.ai/install.ps1 | iex`,
			Hint:    "将打开 PowerShell，确认后执行 Claude Code 官方安装脚本",
		}
	}
	return &CLIInstallHint{
		Shell:   "bash",
		Command: `curl -fsSL https://claude.ai/install.sh | bash`,
		Hint:    "将打开终端，确认后执行 Claude Code 官方安装脚本",
	}
}

func probeCLIVersion(engine, bin string) string {
	args := []string{"--version"}
	var cmd *exec.Cmd
	if engine == "cursor" {
		cmd = cursorCommand(nil, bin, args...)
	} else {
		cmd = exec.Command(bin, args...)
		if runtime.GOOS == "windows" {
			resolved := resolveWindowsBin(bin, "claude.exe")
			if p, err := exec.LookPath(resolved); err == nil {
				resolved = p
			}
			cmd = exec.Command(resolved, args...)
		}
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(out))
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	if len(line) > 120 {
		line = line[:120] + "…"
	}
	return line
}
