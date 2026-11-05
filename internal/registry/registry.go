// Package registry is the Repository for sessions, persisted in <home>/registry.json.
package registry

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/berthkeep/berthkeep/internal/domain"
	"github.com/berthkeep/berthkeep/internal/fsutil"
	"github.com/berthkeep/berthkeep/internal/paths"
)

const (
	fileName    = "registry.json"
	backupName  = "registry.bak.json"
	lockName    = "registry.lock"
	schemaVer   = 1
	lockTimeout = 10 * time.Second
	lockStale   = 10 * time.Second
)

// File is the on-disk document.
type File struct {
	Version  int              `json:"version"`
	Sessions []domain.Session `json:"sessions"`
}

// Store reads and writes the registry of one home directory.
type Store struct {
	home string
	now  func() time.Time
}

// New returns a Store for home. now may be nil (uses time.Now).
func New(home string, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{home: home, now: now}
}

func (s *Store) path(name string) string { return filepath.Join(s.home, name) }

// Load reads the registry.
func (s *Store) Load() (File, error) {
	f, err := readFile(s.path(fileName))
	if err == nil {
		return f, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return File{Version: schemaVer}, nil
	}
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	if errors.As(err, &syn) || errors.As(err, &typ) {
		corrupt := s.path(fmt.Sprintf("registry.corrupt-%s.json", s.now().UTC().Format("20060102T150405Z")))
		_ = os.Rename(s.path(fileName), corrupt)
		if b, berr := readFile(s.path(backupName)); berr == nil {
			return b, nil
		}
		return File{Version: schemaVer}, nil
	}
	return File{}, err
}

func readFile(p string) (File, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return File{}, err
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return File{}, err
	}
	if f.Version == 0 {
		f.Version = schemaVer
	}
	return f, nil
}

// Update applies fn under the lock and saves.
func (s *Store) Update(fn func(*File) error) error {
	if err := os.MkdirAll(s.home, 0o700); err != nil {
		return err
	}
	lock, err := fsutil.Lock(s.path(lockName), lockTimeout, lockStale)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Unlock() }()
	f, err := s.Load()
	if err != nil {
		return err
	}
	if err := fn(&f); err != nil {
		return err
	}
	return s.write(f)
}

func (s *Store) write(f File) error {
	f.Version = schemaVer
	sort.SliceStable(f.Sessions, func(i, j int) bool { return f.Sessions[i].CreatedAt.Before(f.Sessions[j].CreatedAt) })
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if cur, err := os.ReadFile(s.path(fileName)); err == nil {
		_ = fsutil.WriteFileAtomic(s.path(backupName), cur, 0o600)
	}
	return fsutil.WriteFileAtomic(s.path(fileName), append(b, '\n'), 0o600)
}

// List returns all sessions.
func (s *Store) List() ([]domain.Session, error) {
	f, err := s.Load()
	return f.Sessions, err
}

// Get resolves ref by id, slug or exact title (case-insensitive for slug and title).
func (s *Store) Get(ref string) (domain.Session, error) {
	f, err := s.Load()
	if err != nil {
		return domain.Session{}, err
	}
	if i := f.index(ref); i >= 0 {
		return f.Sessions[i], nil
	}
	return domain.Session{}, fmt.Errorf("session %q: %w", ref, domain.ErrNotFound)
}

func (f *File) index(ref string) int {
	for i, x := range f.Sessions {
		if x.ID == ref {
			return i
		}
	}
	for i, x := range f.Sessions {
		if strings.EqualFold(x.Slug, ref) {
			return i
		}
	}
	for i, x := range f.Sessions {
		if strings.EqualFold(x.Title, ref) {
			return i
		}
	}
	return -1
}

// Create stores a new session.
func (s *Store) Create(sess domain.Session) (domain.Session, error) {
	err := s.Update(func(f *File) error {
		now := s.now().UTC()
		sess.ID = NewID()
		sess.Slug = uniqueSlug(f, paths.Slugify(firstNonEmpty(sess.Slug, sess.Title)))
		sess.CreatedAt, sess.UpdatedAt = now, now
		if sess.Origin.At.IsZero() {
			sess.Origin.At = now
		}
		if err := checkProfileOwner(f, sess); err != nil {
			return err
		}
		f.Sessions = append(f.Sessions, sess)
		return nil
	})
	return sess, err
}

// Modify applies fn to the session ref under the lock and saves it.
func (s *Store) Modify(ref string, fn func(*domain.Session) error) (domain.Session, error) {
	var out domain.Session
	err := s.Update(func(f *File) error {
		i := f.index(ref)
		if i < 0 {
			return fmt.Errorf("session %q: %w", ref, domain.ErrNotFound)
		}
		cur := f.Sessions[i]
		if err := fn(&cur); err != nil {
			return err
		}
		if !strings.EqualFold(cur.Slug, f.Sessions[i].Slug) {
			cur.Slug = paths.Slugify(cur.Slug)
			for j, x := range f.Sessions {
				if j != i && strings.EqualFold(x.Slug, cur.Slug) {
					return fmt.Errorf("slug %q already used: %w", cur.Slug, domain.ErrConflict)
				}
			}
		}
		if err := checkProfileOwner(&File{Sessions: append(append([]domain.Session{}, f.Sessions[:i]...), f.Sessions[i+1:]...)}, cur); err != nil {
			return err
		}
		cur.UpdatedAt = s.now().UTC()
		f.Sessions[i] = cur
		out = cur
		return nil
	})
	return out, err
}

// Delete removes the session ref.
func (s *Store) Delete(ref string) error {
	return s.Update(func(f *File) error {
		i := f.index(ref)
		if i < 0 {
			return fmt.Errorf("session %q: %w", ref, domain.ErrNotFound)
		}
		f.Sessions = append(f.Sessions[:i], f.Sessions[i+1:]...)
		return nil
	})
}

func checkProfileOwner(f *File, sess domain.Session) error {
	if sess.ProfileDir == "" {
		return nil
	}
	for _, x := range f.Sessions {
		if x.ID != sess.ID && paths.SamePath(x.ProfileDir, sess.ProfileDir) {
			return fmt.Errorf("profile %s already belongs to session %q: %w", sess.ProfileDir, x.Slug, domain.ErrConflict)
		}
	}
	return nil
}

func uniqueSlug(f *File, base string) string {
	taken := map[string]bool{}
	for _, x := range f.Sessions {
		taken[strings.ToLower(x.Slug)] = true
	}
	if !taken[base] {
		return base
	}
	for n := 2; ; n++ {
		c := fmt.Sprintf("%s-%d", base, n)
		if !taken[c] {
			return c
		}
	}
}

// NewID returns a short random session id like "s_8f3k2a9c".
func NewID() string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return "s_" + hex.EncodeToString(b)
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
