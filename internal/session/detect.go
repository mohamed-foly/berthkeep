package session

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/berthkeep/berthkeep/internal/adapters/procs"
	"github.com/berthkeep/berthkeep/internal/domain"
	"github.com/berthkeep/berthkeep/internal/paths"
	"github.com/berthkeep/berthkeep/internal/port"
)

// Detected is a running browser that exposes a debugging port.
type Detected struct {
	PID         int            `json:"pid"`
	Port        int            `json:"port"`
	UserDataDir string         `json:"userDataDir"`
	Exe         string         `json:"exe"`
	Headless    bool           `json:"headless"`
	Lock        port.LockState `json:"lock,omitempty"`
	Session     string         `json:"session,omitempty"` // slug when managed
}

// Detect lists debug-enabled browsers and marks the managed ones.
func (s *Service) Detect(ctx context.Context) ([]Detected, error) {
	list, err := s.Launcher.Procs.List(ctx, procs.BrowserExeNames)
	if err != nil {
		return nil, err
	}
	sessions, err := s.Store.List()
	if err != nil {
		return nil, err
	}
	out := []Detected{}
	for _, b := range procs.MainDebugBrowsers(list) {
		d := Detected{PID: b.PID, Port: b.Flags.Port, UserDataDir: b.Flags.UserDataDir, Exe: b.Flags.Exe, Headless: b.Flags.Headless}
		if d.UserDataDir != "" && s.Launcher.Locks != nil {
			d.Lock, _ = s.Launcher.Locks.State(d.UserDataDir)
			for _, x := range sessions {
				if paths.SamePath(s.ProfileAbs(x), d.UserDataDir) {
					d.Session = x.Slug
				}
			}
		}
		out = append(out, d)
	}
	return out, nil
}

// AdoptInput picks a running browser (by port or pid) or a folder.
type AdoptInput struct {
	Port  int    `json:"port"`
	PID   int    `json:"pid"`
	Dir   string `json:"dir"`
	Title string `json:"title"`
}

// Adopt registers an existing profile in place.
func (s *Service) Adopt(ctx context.Context, in AdoptInput) (domain.Session, error) {
	dir, port := in.Dir, 0
	if dir == "" {
		found, err := s.Detect(ctx)
		if err != nil {
			return domain.Session{}, err
		}
		for _, d := range found {
			if (in.PID != 0 && d.PID == in.PID) || (in.Port != 0 && d.Port == in.Port) {
				if d.Session != "" {
					return domain.Session{}, fmt.Errorf("already managed as %s: %w", d.Session, domain.ErrConflict)
				}
				dir, port = d.UserDataDir, d.Port
				break
			}
		}
		if dir == "" {
			return domain.Session{}, fmt.Errorf("no debug browser matches: %w", domain.ErrNotFound)
		}
	}
	title := in.Title
	if title == "" {
		title = filepath.Base(filepath.Clean(dir))
	}
	return s.Create(ctx, CreateInput{Title: title, AdoptDir: dir, Port: port, Headed: true})
}
