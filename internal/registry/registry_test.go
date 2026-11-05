package registry

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/mohamed-foly/berthkeep/internal/domain"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	return New(t.TempDir(), nil)
}

func TestCreateGetModifyDelete(t *testing.T) {
	s := newStore(t)
	a, err := s.Create(domain.Session{Title: "Shop admin", ProfileDir: "profiles/shop-admin"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Slug != "shop-admin" || a.ID == "" {
		t.Fatalf("unexpected %+v", a)
	}
	b, err := s.Create(domain.Session{Title: "Shop admin", ProfileDir: "profiles/shop-admin-2"})
	if err != nil {
		t.Fatal(err)
	}
	if b.Slug != "shop-admin-2" {
		t.Fatalf("slug not uniqued: %s", b.Slug)
	}
	for _, ref := range []string{a.ID, "SHOP-ADMIN", "shop admin"} {
		got, err := s.Get(ref)
		if err != nil || got.ID != a.ID {
			t.Fatalf("Get(%q) = %v, %v", ref, got.ID, err)
		}
	}
	if _, err := s.Modify(a.ID, func(x *domain.Session) error { x.Labels = []string{"x"}; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Modify(b.ID, func(x *domain.Session) error { x.Slug = "shop-admin"; return nil }); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected slug conflict, got %v", err)
	}
	if err := s.Delete("shop-admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(a.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestProfileOwnedOnce(t *testing.T) {
	s := newStore(t)
	if _, err := s.Create(domain.Session{Title: "a", ProfileDir: `D:\p\one`}); err != nil {
		t.Fatal(err)
	}
	_, err := s.Create(domain.Session{Title: "b", ProfileDir: "d:/p/ONE/"})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected conflict for same profile, got %v", err)
	}
}

func TestCorruptRecoversFromBackup(t *testing.T) {
	s := newStore(t)
	if _, err := s.Create(domain.Session{Title: "first"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(domain.Session{Title: "second"}); err != nil { // backup now holds "first"
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.home, fileName), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Title != "first" {
		t.Fatalf("expected backup contents, got %+v", list)
	}
	matches, _ := filepath.Glob(filepath.Join(s.home, "registry.corrupt-*.json"))
	if len(matches) != 1 {
		t.Fatalf("corrupt file not preserved: %v", matches)
	}
}

func TestConcurrentGoroutines(t *testing.T) {
	s := newStore(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := s.Create(domain.Session{Title: fmt.Sprintf("s%d", i)}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	list, _ := s.List()
	if len(list) != 20 {
		t.Fatalf("want 20 sessions, got %d", len(list))
	}
}

// TestConcurrentProcesses runs 20 separate processes that each add a session.
func TestConcurrentProcesses(t *testing.T) {
	if home := os.Getenv("BK_REGISTRY_CHILD_HOME"); home != "" {
		n, _ := strconv.Atoi(os.Getenv("BK_REGISTRY_CHILD_N"))
		if _, err := New(home, nil).Create(domain.Session{Title: fmt.Sprintf("p%d", n)}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	home := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cmd := exec.Command(exe, "-test.run=^TestConcurrentProcesses$")
			cmd.Env = append(os.Environ(), "BK_REGISTRY_CHILD_HOME="+home, "BK_REGISTRY_CHILD_N="+strconv.Itoa(i))
			if out, err := cmd.CombinedOutput(); err != nil {
				errs <- fmt.Errorf("child %d: %v: %s", i, err, out)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	list, err := New(home, nil).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 20 {
		t.Fatalf("want 20 sessions after 20 processes, got %d", len(list))
	}
}
