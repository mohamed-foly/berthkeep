//go:build windows

package launcher

import (
	"os/exec"
	"strconv"
	"syscall"

	"golang.org/x/sys/windows"
)

// detach lets the browser outlive berthkeep without a console window.
func detach(cmd *exec.Cmd, hide bool) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
		HideWindow:    hide,
	}
}

// killTree force-kills pid and its children.
func killTree(pid int) error {
	cmd := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}
