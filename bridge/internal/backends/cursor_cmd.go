package backends

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type cursorLaunch struct {
	name    string
	args    []string
	cmdLine string
}

// cursorCommand 构造 Cursor CLI 进程。
// Windows 不能 CreateProcess 直接启动 .cmd/.bat（再叠加 CREATE_NO_WINDOW
// 会报 The directory name is invalid）。Cursor Agent 的 .cmd 只是转调同目录 .ps1，
// 因此优先用 powershell.exe -File 传原始 argv，避免 cmd.exe /C 截断换行、展开 %VAR%、
// 以及把 & | > 当成第二段命令。没有同目录 .ps1 时才退回 cmd.exe，并用 CmdLine 避免
// 与 Go argv 转义叠一层。
func cursorCommand(ctx context.Context, bin string, args ...string) *exec.Cmd {
	launch := planCursorLaunch(resolveCursorLaunchBin(bin), args)
	var cmd *exec.Cmd
	if ctx != nil {
		cmd = exec.CommandContext(ctx, launch.name, launch.args...)
	} else {
		cmd = exec.Command(launch.name, launch.args...)
	}
	applyCmdLine(cmd, launch.cmdLine)
	return cmd
}

func resolveCursorLaunchBin(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "agent"
	}
	resolved := resolveWindowsBin(raw, "agent.exe", "cursor-agent.exe")
	if runtime.GOOS != "windows" {
		return resolved
	}
	if strings.EqualFold(filepath.Ext(resolved), ".exe") || isWindowsBatch(resolved) {
		return resolved
	}
	if p, err := exec.LookPath(resolved); err == nil {
		return resolveWindowsBin(p, "agent.exe", "cursor-agent.exe")
	}
	return resolved
}

func planCursorLaunch(bin string, args []string) cursorLaunch {
	if runtime.GOOS != "windows" || !isWindowsBatch(bin) {
		return cursorLaunch{name: bin, args: args}
	}
	if ps1 := siblingCursorPS1(bin); ps1 != "" {
		out := make([]string, 0, 5+len(args))
		out = append(out, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", ps1)
		out = append(out, args...)
		return cursorLaunch{name: windowsPowerShellExe(), args: out}
	}
	return cursorLaunch{
		name:    windowsCmdExe(),
		args:    []string{"/S", "/C"},
		cmdLine: windowsCmdExe() + " /S /C " + quoteCmdLine(bin, args),
	}
}

func siblingCursorPS1(bin string) string {
	dir := filepath.Dir(bin)
	base := strings.TrimSuffix(filepath.Base(bin), filepath.Ext(bin))
	for _, name := range []string{base + ".ps1", "cursor-agent.ps1"} {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func isWindowsBatch(bin string) bool {
	switch strings.ToLower(filepath.Ext(bin)) {
	case ".cmd", ".bat":
		return true
	default:
		return false
	}
}

func quoteCmdLine(bin string, args []string) string {
	var b strings.Builder
	b.WriteByte('"')
	b.WriteString(quoteCmdArg(bin))
	for _, a := range args {
		b.WriteByte(' ')
		b.WriteString(quoteCmdArg(a))
	}
	b.WriteByte('"')
	return b.String()
}

func quoteCmdArg(s string) string {
	s = strings.ReplaceAll(s, `%`, `%%`)
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func windowsPowerShellExe() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	p := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return p
	}
	return "powershell.exe"
}

func windowsCmdExe() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	p := filepath.Join(root, "System32", "cmd.exe")
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return p
	}
	return "cmd.exe"
}

// ensureCmdDir 先创建工作目录再设 cmd.Dir。Windows CreateProcess 在 lpCurrentDirectory
// 不存在时返回 ERROR_DIRECTORY（The directory name is invalid），报错会写在可执行文件名上，
// 容易误判成 powershell.exe / agent.cmd 本身坏了。
func ensureCmdDir(cmd *exec.Cmd, dir string) {
	if cmd == nil {
		return
	}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	cmd.Dir = dir
}
