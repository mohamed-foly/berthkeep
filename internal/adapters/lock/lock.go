// Package lock reports whether a browser holds a profile.
package lock

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mohamed-foly/berthkeep/internal/port"
)

// Probe is the LockProbe for the current OS.
type Probe struct {
	// Injectable for tests.
	Hostname func() (string, error)
	Alive    func(pid int) bool
}

// New returns a Probe using the real hostname and process table.
func New() *Probe { return &Probe{Hostname: os.Hostname, Alive: pidAlive} }

// State implements port.LockProbe.
func (p *Probe) State(dir string) (port.LockState, error) {
	if st, err := windowsLockfile(filepath.Join(dir, "lockfile")); err == nil {
		return st, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	return p.singletonLock(filepath.Join(dir, "SingletonLock"))
}

// singletonLock reads the POSIX symlink "<host>-<pid>".
func (p *Probe) singletonLock(path string) (port.LockState, error) {
	target, err := os.Readlink(path)
	if errors.Is(err, fs.ErrNotExist) {
		return port.LockFree, nil
	}
	if err != nil {
		return port.LockFree, nil // not a symlink: no lock
	}
	i := strings.LastIndex(target, "-")
	if i < 0 {
		return port.LockStale, nil
	}
	host, pidStr := target[:i], target[i+1:]
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return port.LockStale, nil
	}
	if h, err := p.Hostname(); err == nil && h != host {
		// Held by another host.
		return port.LockHeld, nil
	}
	if p.Alive(pid) {
		return port.LockHeld, nil
	}
	return port.LockStale, nil
}
