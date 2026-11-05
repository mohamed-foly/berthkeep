//go:build windows

package lock

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"

	"github.com/berthkeep/berthkeep/internal/port"
)

// windowsLockfile: held while open by Chrome, else stale.
func windowsLockfile(path string) (port.LockState, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err == nil {
		f.Close()
		return port.LockStale, nil
	}
	if errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return port.LockHeld, nil
	}
	return "", err
}

func pidAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
}
