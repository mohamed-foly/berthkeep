package fsx

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, root, rel, data string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCloneSkipsLocksAndCaches(t *testing.T) {
	src, dst := t.TempDir(), filepath.Join(t.TempDir(), "clone")
	write(t, src, "Local State", "{}")
	write(t, src, "Default/Network/Cookies", "cookies")
	write(t, src, "Default/Cache/Cache_Data/f_1", "cache")
	write(t, src, "Default/Service Worker/CacheStorage/x", "sw")
	write(t, src, "lockfile", "")
	write(t, src, "DevToolsActivePort", "1")
	write(t, src, "BrowserMetrics-spare.pma", "m")
	res, err := CopyDir(src, dst, func(rel string, isDir bool) bool { return CloneSkip(rel, isDir, true) })
	if err != nil {
		t.Fatal(err)
	}
	for _, keep := range []string{"Local State", "Default/Network/Cookies"} {
		if _, err := os.Stat(filepath.Join(dst, keep)); err != nil {
			t.Errorf("%s missing from clone", keep)
		}
	}
	for _, gone := range []string{"Default/Cache", "Default/Service Worker/CacheStorage", "lockfile", "DevToolsActivePort", "BrowserMetrics-spare.pma"} {
		if _, err := os.Stat(filepath.Join(dst, gone)); err == nil {
			t.Errorf("%s should be skipped", gone)
		}
	}
	if res.Files != 2 {
		t.Errorf("files copied = %d, want 2", res.Files)
	}
}

func TestMoveAndTrash(t *testing.T) {
	home := t.TempDir()
	src := filepath.Join(home, "temp", "s_1")
	write(t, src, "Default/Preferences", "p")
	dst := filepath.Join(home, "profiles", "keep")
	if err := Move(src, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "Default", "Preferences")); err != nil {
		t.Fatal("moved file missing")
	}
	if err := Move(dst, dst); err == nil {
		t.Fatal("moving onto an existing target must fail")
	}
	trashed, err := Trash(home, dst, "keep")
	if err != nil || trashed == "" {
		t.Fatalf("trash: %q %v", trashed, err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("source still exists after trash")
	}
}
