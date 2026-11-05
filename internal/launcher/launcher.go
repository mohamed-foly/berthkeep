package launcher

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/berthkeep/berthkeep/internal/adapters/procs"
	"github.com/berthkeep/berthkeep/internal/cdp"
	"github.com/berthkeep/berthkeep/internal/paths"
	"github.com/berthkeep/berthkeep/internal/port"
	"github.com/berthkeep/berthkeep/internal/ports"
)

// ErrExited means the browser exited before its debugging endpoint came up.
var ErrExited = errors.New("browser exited during startup")

// ErrTimeout means the endpoint did not come up in time.
var ErrTimeout = errors.New("browser did not expose CDP in time")

// Launcher owns process-level operations.
type Launcher struct {
	Procs port.ProcessLister
	Locks port.LockProbe
	// ProcNames are the executable names scanned for process matching.
	ProcNames []string
	// StartTimeout bounds WaitReady (default 20 s).
	StartTimeout time.Duration
}

// Started is the result of a successful launch.
type Started struct {
	PID    int
	Port   int
	GUID   string
	WSPath string
}

// Start spawns the browser detached and waits until /json/version answers.
func (l *Launcher) Start(ctx context.Context, s Spec) (Started, error) {
	if s.Port == 0 && s.ProfileDir != "" {
		os.Remove(filepath.Join(s.ProfileDir, "DevToolsActivePort")) // stale after previous runs
	}
	args := BuildArgs(s)
	cmd := exec.Command(s.Exe, args...)
	cmd.Env = os.Environ()
	for k, v := range s.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if s.App && s.EnvStyle {
		cmd.Args = []string{s.Exe}
		cmd.Env = append(cmd.Env, "ELECTRON_EXTRA_LAUNCH_ARGS="+strings.Join(args, " "))
	}
	var logFile *os.File
	if s.LogPath != "" {
		_ = os.MkdirAll(filepath.Dir(s.LogPath), 0o700)
		if f, err := os.OpenFile(s.LogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600); err == nil {
			logFile = f
			cmd.Stdout, cmd.Stderr = f, f
		}
	}
	detach(cmd, s.Headless)
	if err := cmd.Start(); err != nil {
		if logFile != nil {
			logFile.Close()
		}
		return Started{}, fmt.Errorf("start %s: %w", s.Exe, err)
	}
	exited := make(chan error, 1)
	go func() {
		exited <- cmd.Wait()
		if logFile != nil {
			logFile.Close()
		}
	}()
	res, err := l.waitReady(ctx, s, cmd.Process.Pid, exited)
	if err != nil {
		if tail := TailLog(s.LogPath, 40); tail != "" {
			err = fmt.Errorf("%w\n--- browser log ---\n%s", err, tail)
		}
	}
	return res, err
}

func (l *Launcher) waitReady(ctx context.Context, s Spec, pid int, exited <-chan error) (Started, error) {
	timeout := l.StartTimeout
	if timeout == 0 {
		timeout = 20 * time.Second
	}
	deadline := time.Now().Add(timeout)
	port := s.Port
	exitedEarly := false
	for time.Now().Before(deadline) {
		if port == 0 && s.ProfileDir != "" {
			if p, _, err := ReadDevToolsActivePort(s.ProfileDir); err == nil {
				port = p
			}
		}
		if port != 0 {
			if v, err := cdp.GetVersion(ctx, port); err == nil {
				return Started{PID: pid, Port: port, GUID: v.GUID(), WSPath: wsPath(v.WebSocketDebuggerURL)}, nil
			}
		}
		select {
		case <-ctx.Done():
			return Started{}, ctx.Err()
		case err := <-exited:
			// Chrome may exit 0 after a hand-off; poll once more.
			if exitedEarly || port == 0 {
				return Started{}, fmt.Errorf("%w: %v", ErrExited, err)
			}
			exitedEarly = true
		case <-time.After(100 * time.Millisecond):
		}
	}
	return Started{}, ErrTimeout
}

func wsPath(u string) string {
	if i := strings.Index(u, "/devtools/"); i >= 0 {
		return u[i:]
	}
	return ""
}

// ReadDevToolsActivePort reads <profile>/DevToolsActivePort.
func ReadDevToolsActivePort(profileDir string) (int, string, error) {
	b, err := os.ReadFile(filepath.Join(profileDir, "DevToolsActivePort"))
	if err != nil {
		return 0, "", err
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r", ""), "\n")
	p, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil || p <= 0 {
		return 0, "", fmt.Errorf("bad DevToolsActivePort")
	}
	guid := ""
	if len(lines) > 1 {
		guid = lines[1][strings.LastIndex(lines[1], "/")+1:]
	}
	return p, strings.TrimSpace(guid), nil
}

// State is a session's live status.
type State string

const (
	Running           State = "running"
	RunningUnverified State = "running-unverified"
	Busy              State = "busy"       // profile locked, no CDP
	PortTaken         State = "port-taken" // something else answers on the port
	Stopped           State = "stopped"
)

// Status is the probed state of one session.
type Status struct {
	State   State  `json:"state"`
	PID     int    `json:"pid,omitempty"`
	Port    int    `json:"port,omitempty"`
	GUID    string `json:"guid,omitempty"`
	Browser string `json:"browser,omitempty"`
	Tier    int    `json:"tier,omitempty"` // which proof matched
	Holder  string `json:"holder,omitempty"`
}

// Probe describes what to check.
type Probe struct {
	Port       int    // known port, 0 if unknown
	ProfileDir string // native path
	GUID       string // runtime.wsGuid recorded at launch
	PID        int    // runtime.pid
}

// Status runs the tiered ownership proof.
func (l *Launcher) Status(ctx context.Context, p Probe) Status {
	lockState := port.LockFree
	if p.ProfileDir != "" && l.Locks != nil {
		if st, err := l.Locks.State(p.ProfileDir); err == nil {
			lockState = st
		}
	}
	dtPort, dtGUID := 0, ""
	if p.ProfileDir != "" {
		dtPort, dtGUID, _ = ReadDevToolsActivePort(p.ProfileDir)
	}
	candidates := []int{}
	if p.Port > 0 {
		candidates = append(candidates, p.Port)
	}
	if dtPort > 0 && dtPort != p.Port && lockState == port.LockHeld {
		candidates = append(candidates, dtPort)
	}
	answered := 0
	var answeredVer cdp.Version
	for _, cp := range candidates {
		v, err := cdp.GetVersion(ctx, cp)
		if err != nil {
			continue
		}
		g := v.GUID()
		if p.GUID != "" && g == p.GUID {
			return Status{State: Running, PID: p.PID, Port: cp, GUID: g, Browser: v.Browser, Tier: 1}
		}
		if dtGUID != "" && g == dtGUID && dtPort == cp {
			return Status{State: Running, Port: cp, GUID: g, Browser: v.Browser, Tier: 2}
		}
		if answered == 0 {
			answered, answeredVer = cp, v
		}
	}
	if answered != 0 || lockState == port.LockHeld {
		if m, ok, err := l.matchProcess(ctx, p.ProfileDir); err == nil {
			if ok && (answered == 0 || m.Flags.Port == answered || m.Flags.Port == 0) {
				st := Status{State: Running, PID: m.PID, Port: m.Flags.Port, Tier: 3}
				if v, err := cdp.GetVersion(ctx, m.Flags.Port); err == nil {
					st.GUID, st.Browser = v.GUID(), v.Browser
				} else {
					st.State = Busy
				}
				return st
			}
			if ok {
				// Running on an unexpected port.
				return Status{State: Busy, PID: m.PID, Port: m.Flags.Port, Holder: fmt.Sprintf("this profile runs with port %d", m.Flags.Port)}
			}
			if lockState == port.LockHeld {
				return Status{State: Busy, Holder: "profile locked by a browser without a debugging port"}
			}
			return Status{State: PortTaken, Port: answered, Browser: answeredVer.Browser, Holder: l.portHolder(ctx, answered)}
		}
		if answered != 0 && lockState == port.LockHeld {
			return Status{State: RunningUnverified, Port: answered, GUID: answeredVer.GUID(), Browser: answeredVer.Browser}
		}
		if lockState == port.LockHeld {
			return Status{State: Busy}
		}
		return Status{State: PortTaken, Port: answered, Browser: answeredVer.Browser}
	}
	return Status{State: Stopped}
}

func (l *Launcher) names() []string {
	if len(l.ProcNames) > 0 {
		return l.ProcNames
	}
	return procs.BrowserExeNames
}

func (l *Launcher) matchProcess(ctx context.Context, profileDir string) (procs.DebugBrowser, bool, error) {
	if l.Procs == nil || profileDir == "" {
		return procs.DebugBrowser{}, false, errors.New("no process lister")
	}
	list, err := l.Procs.List(ctx, l.names())
	if err != nil {
		return procs.DebugBrowser{}, false, err
	}
	for _, p := range list {
		f := procs.Parse(p.CommandLine)
		if f.IsMain() && f.UserDataDir != "" && paths.SamePath(f.UserDataDir, profileDir) {
			return procs.DebugBrowser{Process: p, Flags: f}, f.PortSet, nil
		}
	}
	return procs.DebugBrowser{}, false, nil
}

// portHolder describes which browser process holds port, when known.
func (l *Launcher) portHolder(ctx context.Context, p int) string {
	if l.Procs == nil {
		return ""
	}
	list, err := l.Procs.List(ctx, l.names())
	if err != nil {
		return ""
	}
	for _, b := range procs.MainDebugBrowsers(list) {
		if b.Flags.Port == p {
			return fmt.Sprintf("pid %d (%s)", b.PID, b.Flags.UserDataDir)
		}
	}
	return ""
}

// Stop closes the browser gracefully, then force-kills after grace.
func (l *Launcher) Stop(ctx context.Context, p Probe, grace time.Duration) error {
	st := l.Status(ctx, p)
	if st.State != Running && st.State != RunningUnverified {
		if st.State == Stopped {
			return nil
		}
		return fmt.Errorf("refusing to stop: state %s (%s)", st.State, st.Holder)
	}
	if st.Port > 0 {
		_ = cdp.CloseBrowser(ctx, st.Port)
	}
	if l.waitGone(ctx, st.Port, p.ProfileDir, grace) {
		return nil
	}
	pid := st.PID
	if pid == 0 {
		if m, ok, _ := l.matchProcess(ctx, p.ProfileDir); ok || m.PID != 0 {
			pid = m.PID
		}
	}
	if pid == 0 {
		return errors.New("browser did not exit and its pid is unknown")
	}
	_ = killTree(pid)
	if l.waitGone(ctx, st.Port, p.ProfileDir, 5*time.Second) {
		return nil
	}
	return fmt.Errorf("browser pid %d is still running", pid)
}

func (l *Launcher) waitGone(ctx context.Context, p int, profileDir string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		portGone := p == 0 || !ports.IsListening(p)
		lockGone := true
		if profileDir != "" && l.Locks != nil {
			if s, err := l.Locks.State(profileDir); err == nil && s == port.LockHeld {
				lockGone = false
			}
		}
		if portGone && lockGone {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(100 * time.Millisecond):
		}
	}
	return false
}

// TailLog returns the last n lines of the log file.
func TailLog(path string, n int) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if len(lines) > n {
			lines = lines[1:]
		}
	}
	return strings.Join(lines, "\n")
}
