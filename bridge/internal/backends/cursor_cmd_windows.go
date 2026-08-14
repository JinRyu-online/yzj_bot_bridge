//go:build windows

package backends

import (
	"os/exec"
	"syscall"
)

func applyCmdLine(cmd *exec.Cmd, cmdLine string) {
	if cmd == nil || cmdLine == "" {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CmdLine = cmdLine
}
