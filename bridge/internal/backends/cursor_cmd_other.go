//go:build !windows

package backends

import "os/exec"

func applyCmdLine(*exec.Cmd, string) {}
