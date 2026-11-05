//go:build integration

package session

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/berthkeep/berthkeep/internal/adapters/browsers"
	"github.com/berthkeep/berthkeep/internal/adapters/lock"
	"github.com/berthkeep/berthkeep/internal/adapters/procs"
	"github.com/berthkeep/berthkeep/internal/cdp"
	"github.com/berthkeep/berthkeep/internal/config"
	"github.com/berthkeep/berthkeep/internal/domain"
	"github.com/berthkeep/berthkeep/internal/launcher"
	"github.com/berthkeep/berthkeep/internal/ports"
	"github.com/berthkeep/berthkeep/internal/registry"
)

// newSvc uses a temp home and high ports.
func newSvc(t *testing.T) *Service {
	t.Helper()
	loc := browsers.New(nil)
	if len(loc.Browsers()) == 0 {
		t.Skip("no Chromium-family browser installed")
	}
	cfg := config.Default()
	cfg.Server.Port = 19221
	cfg.Ports.RangeStart, cfg.Ports.RangeEnd = 19222, 19299
	cfg.Defaults.Mode = "headless"
	home := t.TempDir()
	svc := &Service{Home: home, Config: cfg, Store: registry.New(home, nil),
		Launcher: &launcher.Launcher{Procs: procs.New(), Locks: lock.New()}, Browsers: loc}
	t.Cleanup(func() {
		list, _ := svc.Store.List()
		for _, s := range list {
			_ = svc.Stop(context.Background(), s.ID)
		}
	})
	return svc
}

// ctx bounds each call.
func ctx() context.Context {
	c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	time.AfterFunc(61*time.Second, cancel)
	return c
}

func TestEphemeralStartStopWipes(t *testing.T) {
	svc := newSvc(t)
	s, err := svc.Create(ctx(), CreateInput{Title: "demo", Persistence: "ephemeral", Headless: true, Start: true})
	if err != nil {
		t.Fatal(err)
	}
	port := s.Runtime.Port
	if _, err := cdp.GetVersion(ctx(), port); err != nil {
		t.Fatalf("endpoint not answering: %v", err)
	}
	profile := svc.ProfileAbs(s)
	if !strings.Contains(filepath.ToSlash(profile), "/temp/") {
		t.Fatalf("ephemeral profile should live under temp/: %s", profile)
	}
	t0 := time.Now()
	if err := svc.Stop(ctx(), "demo"); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(t0); d > 3*time.Second {
		t.Errorf("stop took %s", d)
	}
	if ports.IsListening(port) {
		t.Fatal("port still open")
	}
	if _, err := os.Stat(profile); !os.IsNotExist(err) {
		t.Fatalf("ephemeral profile not wiped: %v", err)
	}
	if _, err := svc.Store.Get("demo"); err != nil {
		t.Fatal("ephemeral (non-temp) session record must remain")
	}
}

func setCookie(t *testing.T, port int) {
	t.Helper()
	v, err := cdp.GetVersion(ctx(), port)
	if err != nil {
		t.Fatal(err)
	}
	c, err := cdp.Dial(ctx(), v.WebSocketDebuggerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	cookie := map[string]any{"name": "bk", "value": "kept", "domain": "example.test", "path": "/", "expires": float64(time.Now().Add(24 * time.Hour).Unix())}
	if err := c.Call(ctx(), "Storage.setCookies", map[string]any{"cookies": []any{cookie}}, nil); err != nil {
		t.Fatal(err)
	}
}

func hasCookie(t *testing.T, port int) bool {
	t.Helper()
	v, err := cdp.GetVersion(ctx(), port)
	if err != nil {
		t.Fatal(err)
	}
	c, err := cdp.Dial(ctx(), v.WebSocketDebuggerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var res struct {
		Cookies []struct{ Name, Value string } `json:"cookies"`
	}
	if err := c.Call(ctx(), "Storage.getCookies", nil, &res); err != nil {
		t.Fatal(err)
	}
	for _, ck := range res.Cookies {
		if ck.Name == "bk" && ck.Value == "kept" {
			return true
		}
	}
	return false
}

func TestQuickTempPersistWhileRunning(t *testing.T) {
	svc := newSvc(t)
	s, err := svc.Create(ctx(), CreateInput{Title: "keep", Persistence: "temp", Start: true})
	if err != nil {
		t.Fatal(err)
	}
	setCookie(t, s.Runtime.Port)
	s, err = svc.Persist(ctx(), "keep", false)
	if err != nil {
		t.Fatal(err)
	}
	if s.PendingRelocation != "profiles/keep" || s.AutoRemove {
		t.Fatalf("persist while running should defer the move: %+v", s)
	}
	if err := svc.Stop(ctx(), "keep"); err != nil {
		t.Fatal(err)
	}
	s, err = svc.Store.Get("keep")
	if err != nil {
		t.Fatalf("persisted session must not be auto-removed: %v", err)
	}
	if s.ProfileDir != "profiles/keep" {
		t.Fatalf("profile not relocated: %s", s.ProfileDir)
	}
	if _, err := os.Stat(filepath.Join(svc.Home, "profiles", "keep")); err != nil {
		t.Fatal("relocated profile missing")
	}
	_, st, err := svc.Start(ctx(), "keep", "")
	if err != nil {
		t.Fatal(err)
	}
	if !hasCookie(t, st.Port) {
		t.Fatal("cookie set before persisting is gone after relocation")
	}
}

func TestQuickTempRemovedOnStop(t *testing.T) {
	svc := newSvc(t)
	if _, err := svc.Create(ctx(), CreateInput{Title: "scratch", Persistence: "temp", Start: true}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Stop(ctx(), "scratch"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Store.Get("scratch"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("quick temp record should be deleted, got %v", err)
	}
}

func TestModeSwitchKeepsPortAndTabs(t *testing.T) {
	svc := newSvc(t)
	s, err := svc.Create(ctx(), CreateInput{Title: "switch", Persistence: "ephemeral", Start: true})
	if err != nil {
		t.Fatal(err)
	}
	port := s.Runtime.Port
	if _, err := cdp.OpenTab(ctx(), port, "data:text/html,<title>bk-tab</title>"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	s, st, err := svc.SwitchMode(ctx(), "switch", domain.Headed)
	if err != nil {
		t.Fatal(err)
	}
	if st.Port != port || s.Mode != domain.Headed {
		t.Fatalf("port %d→%d mode %s", port, st.Port, s.Mode)
	}
	deadline := time.Now().Add(5 * time.Second)
	found := false
	for !found && time.Now().Before(deadline) {
		pages, _ := cdp.Pages(ctx(), st.Port)
		for _, p := range pages {
			if strings.Contains(p.URL, "bk-tab") {
				found = true
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !found {
		t.Fatal("tab not reopened after mode switch")
	}
	if s.Persistence != domain.Ephemeral {
		t.Fatal("mode switch must not change persistence")
	}
}

func TestSameFixedPortConflict(t *testing.T) {
	svc := newSvc(t)
	p, err := ports.Allocate(19260, 19299, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx(), CreateInput{Title: "a", Port: p, Persistence: "ephemeral", Start: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx(), CreateInput{Title: "b", Port: p, Persistence: "ephemeral"}); err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.Start(ctx(), "b", "")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	if !strings.Contains(err.Error(), "port") {
		t.Fatalf("error should name the port holder: %v", err)
	}
	views, _ := svc.List(ctx())
	conflicts := 0
	for _, v := range views {
		conflicts += len(v.Conflicts)
	}
	if conflicts != 2 {
		t.Fatalf("both sessions should be flagged, got %d", conflicts)
	}
}

func TestAdoptDefaultProfileRefused(t *testing.T) {
	svc := newSvc(t)
	b := svc.Browsers.Browsers()[0]
	if b.DefaultUserDataDir == "" {
		t.Skip("no default data dir known")
	}
	if _, err := os.Stat(b.DefaultUserDataDir); err != nil {
		t.Skip("default profile folder absent")
	}
	_, err := svc.Create(ctx(), CreateInput{Title: "mine", AdoptDir: b.DefaultUserDataDir})
	if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "Chrome 136") {
		t.Fatalf("want refusal, got %v", err)
	}
}

func TestAutoPortRaceRetries(t *testing.T) {
	svc := newSvc(t)
	p, err := ports.Allocate(19280, 19299, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	exe := svc.Browsers.Browsers()[0].Path
	other := exec.Command(exe, "--user-data-dir="+t.TempDir(), "--remote-debugging-port="+strconv.Itoa(p), "--headless=new", "--no-first-run", "about:blank")
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	go other.Wait()
	defer other.Process.Kill()
	for deadline := time.Now().Add(15 * time.Second); !ports.IsListening(p) && time.Now().Before(deadline); {
		time.Sleep(100 * time.Millisecond)
	}
	svc.Config.Ports.RangeStart, svc.Config.Ports.RangeEnd = p, p+5
	svc.PortFree = func(int) bool { return true } // simulate both starters seeing the port free
	s, err := svc.Create(ctx(), CreateInput{Title: "racer", Persistence: "ephemeral", Start: true})
	if err != nil {
		t.Fatal(err)
	}
	if s.Runtime.Port == p {
		t.Fatalf("recorded the other browser's port %d", p)
	}
	if st := svc.Status(ctx(), s); st.State != launcher.Running || st.Tier != 1 {
		t.Fatalf("status after retry: %+v", st)
	}
}
