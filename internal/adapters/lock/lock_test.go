package lock

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/berthkeep/berthkeep/internal/port"
)

func TestFreeWhenNoLockFiles(t *testing.T) {
	st, err := New().State(t.TempDir())
	if err != nil || st != port.LockFree {
		t.Fatalf("got %v %v", st, err)
	}
}

func TestStaleLockfile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lockfile"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := New().State(dir)
	// On Windows a lockfile that opens fine is stale.
	if err != nil || (st != port.LockStale && st != port.LockFree) {
		t.Fatalf("got %v %v", st, err)
	}
}

func TestSingletonLockRule(t *testing.T) {
	p := &Probe{Hostname: func() (string, error) { return "box", nil }, Alive: func(pid int) bool { return pid == 42 }}
	dir := t.TempDir()
	link := filepath.Join(dir, "SingletonLock")
	cases := map[string]port.LockState{"box-42": port.LockHeld, "box-7": port.LockStale, "other-7": port.LockHeld, "garbage": port.LockStale}
	for target, want := range cases {
		os.Remove(link)
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlinks unavailable here: %v", err)
		}
		if got, _ := p.singletonLock(link); got != want {
			t.Errorf("%s: got %v want %v", target, got, want)
		}
	}
}
