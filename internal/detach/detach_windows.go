//go:build windows

// Package detach starts processes that outlive berthkeep.
package detach

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// Apply detaches cmd from this console.
func Apply(cmd *exec.Cmd, hide bool) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
		HideWindow:    hide,
	}
}
