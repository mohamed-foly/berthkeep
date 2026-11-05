// Package fsutil holds small filesystem helpers shared by several packages.
package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// WriteFileAtomic writes via a temp file and rename.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	cleanup := func() { os.Remove(name) }
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Chmod(name, perm); err != nil {
		cleanup()
		return err
	}
	// Windows rename can fail briefly.
	var renameErr error
	for i := 0; i < 20; i++ {
		if renameErr = os.Rename(name, path); renameErr == nil {
			return nil
		}
		time.Sleep(time.Duration(5*(i+1)) * time.Millisecond)
	}
	cleanup()
	return fmt.Errorf("replace %s: %w", path, renameErr)
}

// FileLock is a cross-process advisory lock based on exclusive file creation.
type FileLock struct {
	path string
}

// ErrLockTimeout is returned when the lock cannot be taken in time.
var ErrLockTimeout = errors.New("lock timeout")

// Lock takes the lock at path, waiting up to timeout.
func Lock(path string, timeout, stale time.Duration) (*FileLock, error) {
	deadline := time.Now().Add(timeout)
	delay := 2 * time.Millisecond
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			fmt.Fprintf(f, "%d %s", os.Getpid(), time.Now().Format(time.RFC3339Nano))
			f.Close()
			return &FileLock{path: path}, nil
		}
		if st, serr := os.Stat(path); serr == nil && time.Since(st.ModTime()) > stale {
			os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%w: %s", ErrLockTimeout, path)
		}
		time.Sleep(delay)
		if delay < 50*time.Millisecond {
			delay *= 2
		}
	}
}

// Unlock releases the lock.
func (l *FileLock) Unlock() error {
	if l == nil {
		return nil
	}
	return os.Remove(l.path)
}
