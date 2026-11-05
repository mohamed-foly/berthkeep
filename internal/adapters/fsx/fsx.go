// Package fsx clones, wipes, moves and trashes profiles.
package fsx

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// lockFiles are never copied.
var lockFiles = map[string]bool{"lockfile": true, "SingletonLock": true, "SingletonCookie": true, "SingletonSocket": true, "DevToolsActivePort": true}

// cacheDirs are rebuilt by the browser on demand; skipped when skipCaches is set.
var cacheDirs = map[string]bool{
	"Cache": true, "Code Cache": true, "GPUCache": true, "GrShaderCache": true, "GraphiteDawnCache": true,
	"ShaderCache": true, "DawnCache": true, "DawnGraphiteCache": true, "DawnWebGPUCache": true, "Crashpad": true,
	"component_crx_cache": true, "extensions_crx_cache": true,
}

// CloneSkip reports whether rel is left out of a clone.
func CloneSkip(rel string, isDir bool, skipCaches bool) bool {
	base := filepath.Base(rel)
	if !isDir && lockFiles[base] {
		return true
	}
	if strings.HasPrefix(base, "BrowserMetrics") {
		return true
	}
	if skipCaches && isDir {
		if cacheDirs[base] {
			return true
		}
		if strings.HasSuffix(filepath.ToSlash(rel), "Service Worker/CacheStorage") {
			return true
		}
	}
	return false
}

// CopyResult reports what a copy did.
type CopyResult struct {
	Files   int
	Bytes   int64
	Skipped []string // unreadable (locked) files
}

// CopyDir copies src into dst (created). skip may be nil.
func CopyDir(src, dst string, skip func(rel string, isDir bool) bool) (CopyResult, error) {
	var res CopyResult
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == src {
				return err
			}
			res.Skipped = append(res.Skipped, p)
			return nil
		}
		rel, _ := filepath.Rel(src, p)
		if rel == "." {
			return os.MkdirAll(dst, 0o700)
		}
		if skip != nil && skip(filepath.ToSlash(rel), d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil // profiles don't need symlinks
		}
		n, err := copyFile(p, target)
		if err != nil {
			res.Skipped = append(res.Skipped, rel)
			return nil
		}
		res.Files++
		res.Bytes += n
		return nil
	})
	return res, err
}

func copyFile(src, dst string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		if st, serr := os.Stat(src); serr == nil {
			_ = os.Chtimes(dst, st.ModTime(), st.ModTime())
		}
	}
	return n, err
}

// Tree counts files and bytes under dir.
func Tree(dir string) (files int, bytes int64, err error) {
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				files++
				bytes += info.Size()
			}
		}
		return nil
	})
	return
}

// RemoveAllRetry deletes dir, retrying while the browser still releases files.
func RemoveAllRetry(dir string, attempts int, delay time.Duration) error {
	var err error
	for i := 0; i < attempts; i++ {
		if err = os.RemoveAll(dir); err == nil {
			return nil
		}
		time.Sleep(delay)
	}
	return err
}

// Move moves a directory.
func Move(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("target %s already exists", dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	srcFiles, srcBytes, err := Tree(src)
	if err != nil {
		return err
	}
	res, err := CopyDir(src, dst, nil)
	if err != nil {
		return err
	}
	if len(res.Skipped) > 0 || res.Files != srcFiles || res.Bytes != srcBytes {
		_ = os.RemoveAll(dst)
		return fmt.Errorf("copy verification failed (%d/%d files, %d/%d bytes, %d unreadable)", res.Files, srcFiles, res.Bytes, srcBytes, len(res.Skipped))
	}
	return RemoveAllRetry(src, 10, 500*time.Millisecond)
}

// Trash moves dir into <home>/trash/<ts>-<name> and returns the new location.
func Trash(home, dir, name string) (string, error) {
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	dst := filepath.Join(home, "trash", time.Now().UTC().Format("20060102T150405Z")+"-"+name)
	return dst, Move(dir, dst)
}
