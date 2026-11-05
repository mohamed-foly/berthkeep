//go:build !windows

// Package detach starts processes that outlive berthkeep.
package detach

import (
	"os/exec"
	"syscall"
)

// Apply starts cmd in its own session.
func Apply(cmd *exec.Cmd, _ bool) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
