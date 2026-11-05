// Package procs lists browser processes and parses their command lines.
package procs

import (
	"strconv"
	"strings"

	"github.com/mohamed-foly/berthkeep/internal/port"
)

// Flags are the CDP switches of a command line.
type Flags struct {
	Exe         string
	UserDataDir string
	Port        int // see PortSet for an explicit 0
	PortSet     bool
	Type        string // empty for the main process
	Headless    bool
	App         string // --app=<url>
}

// IsMain reports whether this is the main browser process (no --type=).
func (f Flags) IsMain() bool { return f.Type == "" }

// Tokenize splits a command line into arguments.
func Tokenize(cmd string) []string {
	var out []string
	var cur strings.Builder
	inQuote, has := false, false
	for _, r := range cmd {
		switch {
		case r == '"':
			inQuote = !inQuote
			has = true
		case (r == ' ' || r == '\t') && !inQuote:
			if has {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(r)
			has = true
		}
	}
	if has {
		out = append(out, cur.String())
	}
	return out
}

// Parse extracts Flags from a full command line.
func Parse(cmd string) Flags {
	toks := Tokenize(cmd)
	var f Flags
	if len(toks) > 0 {
		f.Exe = toks[0]
	}
	for i := 1; i < len(toks); i++ {
		t := toks[i]
		name, val, hasVal := strings.Cut(t, "=")
		next := func() string {
			if hasVal {
				return val
			}
			if i+1 < len(toks) && !strings.HasPrefix(toks[i+1], "-") {
				i++
				return toks[i]
			}
			return ""
		}
		switch name {
		case "--user-data-dir":
			f.UserDataDir = next()
		case "--remote-debugging-port":
			if n, err := strconv.Atoi(next()); err == nil {
				f.Port, f.PortSet = n, true
			}
		case "--type":
			f.Type = next()
		case "--headless":
			f.Headless = true
		case "--app":
			f.App = next()
		}
	}
	return f
}

// DebugBrowser is a main browser process started with a debugging port.
type DebugBrowser struct {
	port.Process
	Flags Flags
}

// MainDebugBrowsers keeps main processes with a debugging port.
func MainDebugBrowsers(list []port.Process) []DebugBrowser {
	var out []DebugBrowser
	for _, p := range list {
		f := Parse(p.CommandLine)
		if f.IsMain() && f.PortSet {
			out = append(out, DebugBrowser{Process: p, Flags: f})
		}
	}
	return out
}

// BrowserExeNames are the process names scanned by default.
var BrowserExeNames = []string{"chrome.exe", "msedge.exe", "brave.exe", "chromium.exe", "chrome", "chromium", "chromium-browser", "msedge", "microsoft-edge", "brave", "Google Chrome", "Microsoft Edge", "Brave Browser", "Chromium"}
