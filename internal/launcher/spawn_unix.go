//go:build !windows

package launcher

import (
	"os/exec"
	"syscall"
	"time"
)

// detach starts the browser in its own session so it outlives berthkeep.
func detach(cmd *exec.Cmd, _ bool) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// killTree terminates the process group, escalating to SIGKILL.
func killTree(pid int) error {
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	time.Sleep(2 * time.Second)
	if err := syscall.Kill(-pid, 0); err == nil {
		return syscall.Kill(-pid, syscall.SIGKILL)
	}
	return nil
}
