//go:build !windows

package procs

import (
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mohamed-foly/berthkeep/internal/port"
)

// Lister lists processes with ps(1).
type Lister struct{}

// New returns the POSIX ProcessLister.
func New() *Lister { return &Lister{} }

// List implements port.ProcessLister.
func (Lister) List(ctx context.Context, names []string) ([]port.Process, error) {
	b, err := exec.CommandContext(ctx, "ps", "-axww", "-o", "pid=,ppid=,command=").Output()
	if err != nil {
		return nil, err
	}
	return parsePS(string(b), names), nil
}

func parsePS(out string, names []string) []port.Process {
	want := map[string]bool{}
	for _, n := range names {
		want[strings.ToLower(n)] = true
	}
	var res []port.Process
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		if err1 != nil || err2 != nil {
			continue
		}
		cmd := strings.TrimSpace(strings.SplitN(strings.TrimSpace(line), " ", 3)[2])
		exe := Tokenize(cmd)
		if len(exe) == 0 {
			continue
		}
		name := filepath.Base(exe[0])
		if !want[strings.ToLower(name)] && !matchesBundle(cmd, want) {
			continue
		}
		res = append(res, port.Process{PID: pid, PPID: ppid, Name: name, CommandLine: cmd})
	}
	return res
}

// matchesBundle matches macOS names with spaces.
func matchesBundle(cmd string, want map[string]bool) bool {
	lc := strings.ToLower(cmd)
	for n := range want {
		if strings.Contains(n, " ") && strings.Contains(lc, "/"+n+" ") {
			return true
		}
	}
	return false
}
