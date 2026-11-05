//go:build integration

package launcher

import (
	"context"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/berthkeep/berthkeep/internal/adapters/browsers"
	"github.com/berthkeep/berthkeep/internal/adapters/lock"
	"github.com/berthkeep/berthkeep/internal/adapters/procs"
	"github.com/berthkeep/berthkeep/internal/cdp"
	"github.com/berthkeep/berthkeep/internal/port"
	"github.com/berthkeep/berthkeep/internal/ports"
)

// Real-browser tests: temp profiles, ports 19300-19349 only.

func setup(t *testing.T) (*Launcher, string) {
	t.Helper()
	b, ok := browsers.Pick(browsers.New(nil).Browsers(), "auto")
	if !ok {
		t.Skip("no Chromium-family browser installed")
	}
	return &Launcher{Procs: procs.New(), Locks: lock.New()}, b.Path
}

func freePort(t *testing.T) int {
	p, err := ports.Allocate(19300, 19349, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func stopAndCheck(t *testing.T, l *Launcher, p Probe) {
	t.Helper()
	ctx := context.Background()
	t0 := time.Now()
	if err := l.Stop(ctx, p, 8*time.Second); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if d := time.Since(t0); d > 3*time.Second {
		t.Errorf("stop took %s (acceptance: < 3 s)", d)
	}
	if st := l.Status(ctx, p); st.State != Stopped {
		t.Fatalf("after stop: %+v", st)
	}
	if s, _ := l.Locks.State(p.ProfileDir); s == port.LockHeld {
		t.Fatal("profile still locked after stop")
	}
}

func TestFixedPortTier1(t *testing.T) {
	l, exe := setup(t)
	ctx := context.Background()
	dir := t.TempDir()
	p := freePort(t)
	st, err := l.Start(ctx, Spec{Exe: exe, ProfileDir: dir, Port: p, Headless: true, LogPath: filepath.Join(t.TempDir(), "b.log")})
	if err != nil {
		t.Fatal(err)
	}
	if st.Port != p || st.GUID == "" {
		t.Fatalf("started: %+v", st)
	}
	probe := Probe{Port: p, ProfileDir: dir, GUID: st.GUID, PID: st.PID}
	if s := l.Status(ctx, probe); s.State != Running || s.Tier != 1 {
		t.Fatalf("status: %+v", s)
	}
	if _, err := cdp.OpenTab(ctx, p, "about:blank#bk"); err != nil {
		t.Fatal(err)
	}
	pages, err := cdp.Pages(ctx, p)
	if err != nil || len(pages) < 2 {
		t.Fatalf("pages: %v %v", pages, err)
	}
	stopAndCheck(t, l, probe)
}

func TestAutoPortTier2(t *testing.T) {
	l, exe := setup(t)
	ctx := context.Background()
	dir := t.TempDir()
	st, err := l.Start(ctx, Spec{Exe: exe, ProfileDir: dir, Port: 0, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	if st.Port == 0 {
		t.Fatal("auto port not discovered")
	}
	probe := Probe{ProfileDir: dir} // no launch record: must be proven via DevToolsActivePort
	if s := l.Status(ctx, probe); s.State != Running || s.Tier != 2 || s.Port != st.Port {
		t.Fatalf("status: %+v (started %+v)", s, st)
	}
	stopAndCheck(t, l, Probe{Port: st.Port, ProfileDir: dir, GUID: st.GUID, PID: st.PID})
}

func TestExternallyStartedTier3(t *testing.T) {
	l, exe := setup(t)
	ctx := context.Background()
	dir := t.TempDir()
	p := freePort(t)
	cmd := exec.Command(exe, "--user-data-dir="+dir, "--remote-debugging-port="+strconv.Itoa(p), "--headless=new", "--no-first-run", "about:blank")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go cmd.Wait()
	deadline := time.Now().Add(15 * time.Second)
	for !ports.IsListening(p) && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	probe := Probe{Port: p, ProfileDir: dir, GUID: "stale-guid-from-a-previous-run"}
	s := l.Status(ctx, probe)
	if s.State != Running || s.Tier != 3 || s.PID == 0 {
		t.Fatalf("status: %+v", s)
	}
	probe.GUID, probe.PID = s.GUID, s.PID
	stopAndCheck(t, l, probe)
}

func TestPortTakenByOtherProcess(t *testing.T) {
	l, _ := setup(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	// Another program answers /json/version on the port (not our profile).
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"Browser":"Other/1","webSocketDebuggerUrl":"ws://127.0.0.1:1/devtools/browser/x"}`))
	}))
	p := ln.Addr().(*net.TCPAddr).Port
	s := l.Status(context.Background(), Probe{Port: p, ProfileDir: t.TempDir()})
	if s.State != PortTaken {
		t.Fatalf("status: %+v", s)
	}
}
