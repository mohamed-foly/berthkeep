//go:build !windows

package launcher

import (
	"syscall"
	"time"
)

// killTree terminates the process group, escalating to SIGKILL.
func killTree(pid int) error {
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	time.Sleep(2 * time.Second)
	if err := syscall.Kill(-pid, 0); err == nil {
		return syscall.Kill(-pid, syscall.SIGKILL)
	}
	return nil
}
