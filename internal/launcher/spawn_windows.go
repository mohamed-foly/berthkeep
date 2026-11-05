//go:build windows

package launcher

import (
	"os/exec"
	"strconv"
	"syscall"
)

// killTree force-kills pid and its children.
func killTree(pid int) error {
	cmd := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}
