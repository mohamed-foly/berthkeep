//go:build !windows

package lock

import (
	"io/fs"
	"syscall"

	"github.com/mohamed-foly/berthkeep/internal/port"
)

// windowsLockfile is a no-op on POSIX.
func windowsLockfile(string) (port.LockState, error) { return "", fs.ErrNotExist }

func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
