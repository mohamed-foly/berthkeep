// Package session is the Facade used by the CLI, HTTP API and tray.
package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mohamed-foly/berthkeep/internal/adapters/browsers"
	"github.com/mohamed-foly/berthkeep/internal/adapters/fsx"
	"github.com/mohamed-foly/berthkeep/internal/cdp"
	"github.com/mohamed-foly/berthkeep/internal/config"
	"github.com/mohamed-foly/berthkeep/internal/domain"
	"github.com/mohamed-foly/berthkeep/internal/launcher"
	"github.com/mohamed-foly/berthkeep/internal/paths"
	"github.com/mohamed-foly/berthkeep/internal/port"
	"github.com/mohamed-foly/berthkeep/internal/ports"
	"github.com/mohamed-foly/berthkeep/internal/registry"
)

// Service wires the session use cases.
type Service struct {
	Home     string
	Config   config.Config
	Store    *registry.Store
	Launcher *launcher.Launcher
	Browsers port.BrowserLocator
	Now      func() time.Time
	// PortFree checks a port before start; defaults to ports.IsFree.
	PortFree func(int) bool
}

// View is a session plus its live status.
type View struct {
	domain.Session
	Status     launcher.Status `json:"status"`
	ProfileAbs string          `json:"profileAbs"`
	Conflicts  []string        `json:"conflicts,omitempty"`
}

// Endpoint returns http://127.0.0.1:<port>.
func Endpoint(port int) string { return cdp.Endpoint(port) }

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// ProfileAbs resolves a session's profile directory to a native absolute path.
func (s *Service) ProfileAbs(sess domain.Session) string {
	if sess.ProfileDir == "" {
		return ""
	}
	p := sess.ProfileDir
	if sess.IsManaged() {
		p = filepath.Join(s.Home, filepath.FromSlash(p))
	}
	abs, err := paths.NativePath(p)
	if err != nil {
		return p
	}
	return abs
}

func (s *Service) probe(sess domain.Session) launcher.Probe {
	p := launcher.Probe{Port: sess.FixedPort(), ProfileDir: s.ProfileAbs(sess)}
	if rt := sess.Runtime; rt != nil {
		if p.Port == 0 {
			p.Port = rt.Port
		}
		p.GUID, p.PID = rt.WSGUID, rt.PID
	}
	return p
}

// Status probes one session.
func (s *Service) Status(ctx context.Context, sess domain.Session) launcher.Status {
	return s.Launcher.Status(ctx, s.probe(sess))
}

// List returns every session with its live status and port conflicts.
func (s *Service) List(ctx context.Context) ([]View, error) {
	list, err := s.Store.List()
	if err != nil {
		return nil, err
	}
	byPort := map[int][]string{}
	for _, x := range list {
		if p := x.FixedPort(); p != 0 {
			byPort[p] = append(byPort[p], x.Slug)
		}
	}
	out := make([]View, 0, len(list))
	for _, x := range list {
		v := View{Session: x, Status: s.Status(ctx, x), ProfileAbs: s.ProfileAbs(x)}
		if p := x.FixedPort(); p != 0 && len(byPort[p]) > 1 {
			for _, o := range byPort[p] {
				if o != x.Slug {
					v.Conflicts = append(v.Conflicts, fmt.Sprintf("port %d shared with %s", p, o))
				}
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// Get returns one session view.
func (s *Service) Get(ctx context.Context, ref string) (View, error) {
	x, err := s.Store.Get(ref)
	if err != nil {
		return View{}, err
	}
	return View{Session: x, Status: s.Status(ctx, x), ProfileAbs: s.ProfileAbs(x)}, nil
}

// CreateInput describes a new session.
type CreateInput struct {
	Title       string
	Labels      []string
	Notes       string
	Persistence string // persistent | ephemeral | temp (quick temp)
	Port        int    // 0 = auto
	Browser     string
	BrowserPath string
	Headless    bool
	Headed      bool
	URLs        []string
	ExtraArgs   []string
	CloneFrom   string // session ref
	CloneLive   bool   // copy while the source runs
	AdoptDir    string // register this folder in place
	Start       bool
}

// Create registers a session and prepares its profile.
func (s *Service) Create(ctx context.Context, in CreateInput) (domain.Session, error) {
	if strings.TrimSpace(in.Title) == "" {
		return domain.Session{}, fmt.Errorf("title is required: %w", domain.ErrInvalid)
	}
	if in.Port != 0 && (in.Port < 1024 || in.Port > 65535) {
		return domain.Session{}, fmt.Errorf("port %d out of range: %w", in.Port, domain.ErrInvalid)
	}
	sess := domain.Session{
		Title: in.Title, Labels: in.Labels, Notes: in.Notes, Kind: domain.KindBrowser,
		Browser: firstNonEmpty(in.Browser, s.Config.Defaults.Browser), BrowserPath: in.BrowserPath,
		Mode: domain.Mode(s.Config.Defaults.Mode), StartURLs: in.URLs, ExtraArgs: in.ExtraArgs, RestoreTabs: true,
		Origin: domain.Origin{Type: domain.OriginCreated},
	}
	if in.BrowserPath != "" {
		sess.Browser = "custom"
	}
	if in.Headless {
		sess.Mode = domain.Headless
	} else if in.Headed {
		sess.Mode = domain.Headed
	}
	if in.Port != 0 {
		p := in.Port
		sess.Port = &p
	}
	switch firstNonEmpty(in.Persistence, s.Config.Defaults.Persistence) {
	case "persistent":
		sess.Persistence = domain.Persistent
	case "ephemeral":
		sess.Persistence = domain.Ephemeral
	case "temp":
		sess.Persistence, sess.AutoRemove = domain.Ephemeral, true
	default:
		return domain.Session{}, fmt.Errorf("persistence must be persistent, ephemeral or temp: %w", domain.ErrInvalid)
	}
	if in.AdoptDir != "" {
		abs, err := s.checkAdoptable(in.AdoptDir)
		if err != nil {
			return domain.Session{}, err
		}
		sess.ProfileDir, sess.Persistence, sess.AutoRemove = abs, domain.Persistent, false
		sess.Origin = domain.Origin{Type: domain.OriginAdopted, From: abs}
	}
	created, err := s.Store.Create(sess)
	if err != nil {
		return domain.Session{}, err
	}
	if created.ProfileDir == "" {
		rel := "profiles/" + created.Slug
		if created.Persistence == domain.Ephemeral {
			rel = "temp/" + created.ID
		}
		created, err = s.Store.Modify(created.ID, func(x *domain.Session) error { x.ProfileDir = rel; return nil })
		if err != nil {
			return domain.Session{}, err
		}
	}
	if in.CloneFrom != "" {
		if err := s.clone(ctx, in.CloneFrom, created, in.CloneLive); err != nil {
			_ = s.Store.Delete(created.ID)
			_ = os.RemoveAll(s.ProfileAbs(created))
			return domain.Session{}, err
		}
	}
	if in.Start {
		started, _, err := s.Start(ctx, created.ID, "")
		if err != nil {
			return created, err
		}
		created = started
	}
	return created, nil
}

// checkAdoptable refuses a browser's default user-data-dir and non-profile folders.
func (s *Service) checkAdoptable(dir string) (string, error) {
	abs, err := paths.NativePath(dir)
	if err != nil {
		return "", err
	}
	for _, b := range s.Browsers.Browsers() {
		if b.DefaultUserDataDir != "" && paths.SamePath(abs, b.DefaultUserDataDir) {
			return "", fmt.Errorf("%s is %s's default profile folder; Chrome 136+ refuses remote debugging on it. Clone it instead: %w", abs, b.Name, domain.ErrInvalid)
		}
	}
	st, err := os.Stat(abs)
	if err != nil || !st.IsDir() {
		return "", fmt.Errorf("%s is not a directory: %w", abs, domain.ErrInvalid)
	}
	return abs, nil
}

func (s *Service) clone(ctx context.Context, srcRef string, dst domain.Session, live bool) error {
	src, err := s.Store.Get(srcRef)
	if err != nil {
		return err
	}
	st := s.Status(ctx, src)
	wasRunning := st.State == launcher.Running || st.State == launcher.RunningUnverified
	if wasRunning && !live {
		if err := s.stop(ctx, src, true); err != nil {
			return err
		}
	}
	res, err := fsx.CopyDir(s.ProfileAbs(src), s.ProfileAbs(dst), func(rel string, isDir bool) bool {
		return fsx.CloneSkip(rel, isDir, s.Config.Clone.SkipCaches)
	})
	if wasRunning && !live {
		if _, _, serr := s.Start(ctx, src.ID, ""); serr != nil && err == nil {
			err = fmt.Errorf("clone done, but restarting %s failed: %w", src.Slug, serr)
		}
	}
	if err != nil {
		return err
	}
	_, err = s.Store.Modify(dst.ID, func(x *domain.Session) error {
		x.Origin = domain.Origin{Type: domain.OriginCloned, From: src.ID, At: s.now().UTC()}
		if len(res.Skipped) > 0 {
			x.Notes = strings.TrimSpace(x.Notes + fmt.Sprintf("\nlive clone skipped %d locked file(s)", len(res.Skipped)))
		}
		return nil
	})
	return err
}

// Start launches the session (idempotent).
func (s *Service) Start(ctx context.Context, ref string, mode domain.Mode) (domain.Session, launcher.Status, error) {
	sess, err := s.Store.Get(ref)
	if err != nil {
		return domain.Session{}, launcher.Status{}, err
	}
	if mode != "" && mode != sess.Mode {
		if sess, err = s.Store.Modify(sess.ID, func(x *domain.Session) error { x.Mode = mode; return nil }); err != nil {
			return sess, launcher.Status{}, err
		}
	}
	return s.start(ctx, sess, startOpts{})
}

// startOpts override stored settings for one launch without persisting them.
type startOpts struct {
	urls        []string // tabs to reopen (restart, mode switch)
	portHint    int      // previous auto port, reused when free
	keepProfile bool     // keep an ephemeral profile
}

func (s *Service) start(ctx context.Context, sess domain.Session, o startOpts) (domain.Session, launcher.Status, error) {
	st := s.Status(ctx, sess)
	switch st.State {
	case launcher.Running, launcher.RunningUnverified:
		return s.recordRuntime(sess, st, sess.Mode)
	case launcher.Busy:
		return sess, st, fmt.Errorf("profile of %s is in use (%s): %w", sess.Slug, firstNonEmpty(st.Holder, "pid "+fmt.Sprint(st.PID)), domain.ErrBusy)
	case launcher.PortTaken:
		return sess, st, fmt.Errorf("port %d is taken by %s: %w", st.Port, firstNonEmpty(st.Holder, "another program"), domain.ErrConflict)
	}
	port, err := s.pickPort(sess, o.portHint)
	if err != nil {
		return sess, st, err
	}
	exe, err := s.executable(sess)
	if err != nil {
		return sess, st, err
	}
	profile := s.ProfileAbs(sess)
	if sess.Persistence == domain.Ephemeral && sess.IsManaged() && !o.keepProfile {
		if err := fsx.RemoveAllRetry(profile, 10, 500*time.Millisecond); err != nil {
			return sess, st, fmt.Errorf("clean ephemeral profile: %w", err)
		}
	}
	if err := os.MkdirAll(profile, 0o700); err != nil {
		return sess, st, err
	}
	urls := sess.StartURLs
	switch {
	case len(o.urls) > 0:
		urls = o.urls
	case sess.RestoreTabs && sess.Persistence == domain.Persistent && sess.Runtime != nil && len(sess.Runtime.LastTabs) > 0:
		urls = sess.Runtime.LastTabs
	}
	spec := launcher.Spec{
		Exe: exe, ProfileDir: profile, Port: port, Headless: sess.Mode == domain.Headless,
		App: sess.Kind == domain.KindApp, EnvStyle: sess.AppArgsStyle == "electron-env",
		BaseArgs: s.Config.Defaults.Args, ExtraArgs: sess.ExtraArgs, URLs: urls, Env: sess.Env,
		LogPath: filepath.Join(s.Home, "logs", sess.Slug+".log"),
	}
	if w := sess.Window; w != nil {
		spec.Width, spec.Height = w.Width, w.Height
	} else if sess.Mode == domain.Headless {
		spec.Width, spec.Height = s.Config.Defaults.Window.Width, s.Config.Defaults.Window.Height
	}
	started, err := s.Launcher.Start(ctx, spec)
	for tries := 0; errors.Is(err, launcher.ErrPortRace) && sess.FixedPort() == 0 && tries < 3; tries++ {
		// Lost an auto port to another starter: take the next free one.
		if spec.Port, err = s.pickPort(sess, 0, spec.Port); err != nil {
			break
		}
		started, err = s.Launcher.Start(ctx, spec)
	}
	if errors.Is(err, launcher.ErrPortRace) {
		return sess, st, fmt.Errorf("%s: %v: %w", sess.Slug, err, domain.ErrConflict)
	}
	if err != nil {
		return sess, st, fmt.Errorf("%s: %w: %v", sess.Slug, ErrStartFailed, err)
	}
	return s.recordRuntime(sess, launcher.Status{State: launcher.Running, PID: started.PID, Port: started.Port, GUID: started.GUID}, sess.Mode)
}

// ErrStartFailed wraps launch failures (CLI exit code 4).
var ErrStartFailed = errors.New("browser failed to start")

func (s *Service) recordRuntime(sess domain.Session, st launcher.Status, mode domain.Mode) (domain.Session, launcher.Status, error) {
	if sess.Runtime != nil && sess.Runtime.WSGUID == st.GUID && sess.Runtime.Port == st.Port && st.GUID != "" {
		return sess, st, nil
	}
	now := s.now().UTC()
	updated, err := s.Store.Modify(sess.ID, func(x *domain.Session) error {
		var tabs []string
		if x.Runtime != nil {
			tabs = x.Runtime.LastTabs
		}
		x.Runtime = &domain.Runtime{PID: st.PID, Port: st.Port, Mode: mode, WSGUID: st.GUID, WSPath: "/devtools/browser/" + st.GUID, StartedAt: now, LastTabs: tabs}
		x.LastStartedAt = &now
		return nil
	})
	return updated, st, err
}

func (s *Service) executable(sess domain.Session) (string, error) {
	if sess.BrowserPath != "" {
		return sess.BrowserPath, nil
	}
	b, ok := browsers.Pick(s.Browsers.Browsers(), sess.Browser)
	if !ok {
		return "", fmt.Errorf("browser %q is not installed (see `berthkeep browsers`): %w", sess.Browser, domain.ErrNotFound)
	}
	return b.Path, nil
}

func (s *Service) pickPort(sess domain.Session, hint int, skip ...int) (int, error) {
	free := s.PortFree
	if free == nil {
		free = ports.IsFree
	}
	if p := sess.FixedPort(); p != 0 {
		if !free(p) {
			return 0, fmt.Errorf("port %d is in use by another program: %w", p, domain.ErrConflict)
		}
		return p, nil
	}
	reserved := map[int]bool{s.Config.Server.Port: true}
	for _, p := range skip {
		reserved[p] = true
	}
	for _, p := range s.Config.Ports.Reserved {
		reserved[p] = true
	}
	list, err := s.Store.List()
	if err != nil {
		return 0, err
	}
	for _, x := range list {
		if p := x.FixedPort(); p != 0 && x.ID != sess.ID {
			reserved[p] = true
		}
	}
	if hint > 0 && !reserved[hint] && free(hint) {
		return hint, nil
	}
	p, err := ports.Allocate(s.Config.Ports.RangeStart, s.Config.Ports.RangeEnd, reserved, free)
	if err != nil {
		return 0, fmt.Errorf("%v: %w", err, domain.ErrConflict)
	}
	return p, nil
}

// Stop stops the session and runs post-stop hooks.
func (s *Service) Stop(ctx context.Context, ref string) error {
	sess, err := s.Store.Get(ref)
	if err != nil {
		return err
	}
	return s.stop(ctx, sess, false)
}

// stop closes the browser; keepProfile skips the wipe.
func (s *Service) stop(ctx context.Context, sess domain.Session, keepProfile bool) error {
	p := s.probe(sess)
	st := s.Launcher.Status(ctx, p)
	if st.State == launcher.Running || st.State == launcher.RunningUnverified {
		if pages, err := cdp.Pages(ctx, st.Port); err == nil {
			tabs := make([]string, 0, len(pages))
			for _, pg := range pages {
				if pg.URL != "about:blank" {
					tabs = append(tabs, pg.URL)
				}
			}
			sess, _ = s.Store.Modify(sess.ID, func(x *domain.Session) error {
				if x.Runtime == nil {
					x.Runtime = &domain.Runtime{}
				}
				x.Runtime.LastTabs = tabs
				return nil
			})
		}
		p.Port, p.PID, p.GUID = st.Port, st.PID, st.GUID
		grace := time.Duration(s.Config.Stop.GraceMs) * time.Millisecond
		if err := s.Launcher.Stop(ctx, p, grace); err != nil {
			return err
		}
	} else if st.State == launcher.Busy {
		return fmt.Errorf("profile of %s is held by another browser (%s): %w", sess.Slug, st.Holder, domain.ErrBusy)
	}
	if keepProfile {
		return nil
	}
	return s.afterStop(sess)
}

// afterStop runs the post-stop hooks: ephemeral wipe, pending relocation, auto-remove.
func (s *Service) afterStop(sess domain.Session) error {
	now := s.now().UTC()
	profile := s.ProfileAbs(sess)
	if sess.AutoRemove {
		_ = fsx.RemoveAllRetry(profile, 10, 500*time.Millisecond)
		return s.Store.Delete(sess.ID)
	}
	if sess.Persistence == domain.Ephemeral && sess.IsManaged() {
		if err := fsx.RemoveAllRetry(profile, 10, 500*time.Millisecond); err != nil {
			return fmt.Errorf("wipe ephemeral profile: %w", err)
		}
	}
	if sess.PendingRelocation != "" {
		dst := filepath.Join(s.Home, filepath.FromSlash(sess.PendingRelocation))
		if err := fsx.Move(profile, dst); err != nil {
			return fmt.Errorf("relocate profile: %w", err)
		}
	}
	_, err := s.Store.Modify(sess.ID, func(x *domain.Session) error {
		if x.PendingRelocation != "" {
			x.ProfileDir, x.PendingRelocation = x.PendingRelocation, ""
		}
		x.LastStoppedAt = &now
		if x.Runtime != nil {
			x.Runtime.PID, x.Runtime.WSGUID, x.Runtime.WSPath = 0, "", ""
		}
		return nil
	})
	return err
}

// Restart stops (keeping the profile) and starts again with the same tabs.
func (s *Service) Restart(ctx context.Context, ref string) (domain.Session, launcher.Status, error) {
	sess, err := s.Store.Get(ref)
	if err != nil {
		return sess, launcher.Status{}, err
	}
	if err := s.stop(ctx, sess, true); err != nil {
		return sess, launcher.Status{}, err
	}
	return s.relaunch(ctx, sess.ID)
}

// SwitchMode relaunches in mode, keeping port and tabs.
func (s *Service) SwitchMode(ctx context.Context, ref string, mode domain.Mode) (domain.Session, launcher.Status, error) {
	if mode != domain.Headed && mode != domain.Headless {
		return domain.Session{}, launcher.Status{}, fmt.Errorf("mode must be headed or headless: %w", domain.ErrInvalid)
	}
	sess, err := s.Store.Modify(ref, func(x *domain.Session) error { x.Mode = mode; return nil })
	if err != nil {
		return sess, launcher.Status{}, err
	}
	st := s.Status(ctx, sess)
	if st.State != launcher.Running && st.State != launcher.RunningUnverified {
		return sess, st, nil // takes effect on the next start
	}
	if err := s.stop(ctx, sess, true); err != nil {
		return sess, st, err
	}
	return s.relaunch(ctx, sess.ID)
}

// relaunch restarts with the same profile, tabs and port.
func (s *Service) relaunch(ctx context.Context, id string) (domain.Session, launcher.Status, error) {
	sess, err := s.Store.Get(id)
	if err != nil {
		return sess, launcher.Status{}, err
	}
	o := startOpts{keepProfile: true}
	if rt := sess.Runtime; rt != nil {
		o.portHint = rt.Port
		if sess.RestoreTabs {
			o.urls = rt.LastTabs
		}
	}
	return s.start(ctx, sess, o)
}

// Persist turns an ephemeral session persistent.
func (s *Service) Persist(ctx context.Context, ref string, now bool) (domain.Session, error) {
	sess, err := s.Store.Get(ref)
	if err != nil {
		return sess, err
	}
	if sess.Persistence == domain.Persistent && sess.PendingRelocation == "" {
		return sess, nil
	}
	target := "profiles/" + sess.Slug
	if _, err := os.Stat(filepath.Join(s.Home, filepath.FromSlash(target))); err == nil && !paths.SamePath(sess.ProfileDir, target) {
		target = "profiles/" + sess.Slug + "-" + sess.ID
	}
	st := s.Status(ctx, sess)
	running := st.State == launcher.Running || st.State == launcher.RunningUnverified
	sess, err = s.Store.Modify(sess.ID, func(x *domain.Session) error {
		x.Persistence, x.AutoRemove = domain.Persistent, false
		if x.IsManaged() && !strings.HasPrefix(x.ProfileDir, "profiles/") {
			x.PendingRelocation = target
		}
		return nil
	})
	if err != nil || sess.PendingRelocation == "" {
		return sess, err
	}
	if !running {
		return sess, s.afterStop(sess)
	}
	if now {
		if err := s.stop(ctx, sess, false); err != nil { // hooks relocate, no wipe
			return sess, err
		}
		out, _, err := s.relaunch(ctx, sess.ID)
		return out, err
	}
	return sess, nil
}

// MakeEphemeral wipes the profile on the next stop.
func (s *Service) MakeEphemeral(ref string) (domain.Session, error) {
	return s.Store.Modify(ref, func(x *domain.Session) error {
		if !x.IsManaged() {
			return fmt.Errorf("only managed profiles can become ephemeral: %w", domain.ErrInvalid)
		}
		x.Persistence = domain.Ephemeral
		return nil
	})
}

// Delete removes a session.
func (s *Service) Delete(ctx context.Context, ref string, keepProfile bool) (trashed string, err error) {
	sess, err := s.Store.Get(ref)
	if err != nil {
		return "", err
	}
	if err := s.stop(ctx, sess, true); err != nil {
		return "", err
	}
	profile := s.ProfileAbs(sess)
	if sess.IsManaged() && !keepProfile {
		if sess.Persistence == domain.Ephemeral {
			err = fsx.RemoveAllRetry(profile, 10, 500*time.Millisecond)
		} else {
			trashed, err = fsx.Trash(s.Home, profile, sess.Slug)
		}
		if err != nil {
			return "", err
		}
	}
	return trashed, s.Store.Delete(sess.ID)
}

// EndpointFor returns the endpoint, optionally starting it.
func (s *Service) EndpointFor(ctx context.Context, ref string, ensure bool) (string, launcher.Status, error) {
	sess, err := s.Store.Get(ref)
	if err != nil {
		return "", launcher.Status{}, err
	}
	st := s.Status(ctx, sess)
	if st.State != launcher.Running && st.State != launcher.RunningUnverified {
		if !ensure {
			return "", st, fmt.Errorf("%s is %s (use --ensure to start it): %w", sess.Slug, st.State, domain.ErrNotFound)
		}
		if _, st, err = s.Start(ctx, sess.ID, ""); err != nil {
			return "", st, err
		}
	}
	return Endpoint(st.Port), st, nil
}

// GC runs pending post-stop hooks for sessions that stopped while nobody was watching.
func (s *Service) GC(ctx context.Context) error {
	list, err := s.Store.List()
	if err != nil {
		return err
	}
	var errs []error
	for _, x := range list {
		needs := x.PendingRelocation != "" || x.AutoRemove || (x.Persistence == domain.Ephemeral && x.IsManaged() && dirHasContent(s.ProfileAbs(x)))
		if !needs || x.Runtime == nil || x.Runtime.WSGUID == "" {
			continue // never started, or hooks already ran
		}
		if st := s.Status(ctx, x); st.State == launcher.Stopped {
			if err := s.afterStop(x); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func dirHasContent(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}

func firstNonEmpty(v ...string) string {
	for _, x := range v {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}
