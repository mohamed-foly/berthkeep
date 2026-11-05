// Package browsers locates installed Chromium-family browsers.
package browsers

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/berthkeep/berthkeep/internal/port"
)

// Candidate is a possible browser install.
type Candidate struct {
	ID, Name string
	Paths    []string // absolute executable candidates, in priority order
	PathCmds []string // executable names to look up on PATH (Linux)
	DataDir  string   // default user-data-dir
}

// Candidates lists install candidates for goos, in priority order.
func Candidates(goos string, env func(string) string) []Candidate {
	switch goos {
	case "windows":
		pf, pf86, la := env("ProgramFiles"), env("ProgramFiles(x86)"), env("LOCALAPPDATA")
		win := func(rel string) []string {
			var out []string
			for _, root := range []string{pf, pf86, la} {
				if root != "" {
					out = append(out, filepath.Join(root, rel))
				}
			}
			return out
		}
		ud := func(rel string) string {
			if la == "" {
				return ""
			}
			return filepath.Join(la, rel, "User Data")
		}
		return []Candidate{
			{ID: "chrome", Name: "Google Chrome", Paths: win(`Google\Chrome\Application\chrome.exe`), DataDir: ud(`Google\Chrome`)},
			{ID: "chrome-beta", Name: "Google Chrome Beta", Paths: win(`Google\Chrome Beta\Application\chrome.exe`), DataDir: ud(`Google\Chrome Beta`)},
			{ID: "chrome-dev", Name: "Google Chrome Dev", Paths: win(`Google\Chrome Dev\Application\chrome.exe`), DataDir: ud(`Google\Chrome Dev`)},
			{ID: "chrome-canary", Name: "Google Chrome Canary", Paths: win(`Google\Chrome SxS\Application\chrome.exe`), DataDir: ud(`Google\Chrome SxS`)},
			{ID: "chromium", Name: "Chromium", Paths: win(`Chromium\Application\chrome.exe`), DataDir: ud(`Chromium`)},
			{ID: "edge", Name: "Microsoft Edge", Paths: win(`Microsoft\Edge\Application\msedge.exe`), DataDir: ud(`Microsoft\Edge`)},
			{ID: "edge-beta", Name: "Microsoft Edge Beta", Paths: win(`Microsoft\Edge Beta\Application\msedge.exe`), DataDir: ud(`Microsoft\Edge Beta`)},
			{ID: "edge-dev", Name: "Microsoft Edge Dev", Paths: win(`Microsoft\Edge Dev\Application\msedge.exe`), DataDir: ud(`Microsoft\Edge Dev`)},
			{ID: "brave", Name: "Brave", Paths: win(`BraveSoftware\Brave-Browser\Application\brave.exe`), DataDir: ud(`BraveSoftware\Brave-Browser`)},
		}
	case "darwin":
		home := env("HOME")
		app := func(bundle, bin string) []string {
			p := []string{filepath.Join("/Applications", bundle, "Contents", "MacOS", bin)}
			if home != "" {
				p = append(p, filepath.Join(home, "Applications", bundle, "Contents", "MacOS", bin))
			}
			return p
		}
		as := func(rel string) string {
			if home == "" {
				return ""
			}
			return filepath.Join(home, "Library", "Application Support", rel)
		}
		return []Candidate{
			{ID: "chrome", Name: "Google Chrome", Paths: app("Google Chrome.app", "Google Chrome"), DataDir: as("Google/Chrome")},
			{ID: "chrome-beta", Name: "Google Chrome Beta", Paths: app("Google Chrome Beta.app", "Google Chrome Beta"), DataDir: as("Google/Chrome Beta")},
			{ID: "chrome-dev", Name: "Google Chrome Dev", Paths: app("Google Chrome Dev.app", "Google Chrome Dev"), DataDir: as("Google/Chrome Dev")},
			{ID: "chrome-canary", Name: "Google Chrome Canary", Paths: app("Google Chrome Canary.app", "Google Chrome Canary"), DataDir: as("Google/Chrome Canary")},
			{ID: "chromium", Name: "Chromium", Paths: app("Chromium.app", "Chromium"), DataDir: as("Chromium")},
			{ID: "edge", Name: "Microsoft Edge", Paths: app("Microsoft Edge.app", "Microsoft Edge"), DataDir: as("Microsoft Edge")},
			{ID: "brave", Name: "Brave", Paths: app("Brave Browser.app", "Brave Browser"), DataDir: as("BraveSoftware/Brave-Browser")},
		}
	default:
		cfg := env("XDG_CONFIG_HOME")
		if cfg == "" && env("HOME") != "" {
			cfg = filepath.Join(env("HOME"), ".config")
		}
		dd := func(rel string) string {
			if cfg == "" {
				return ""
			}
			return filepath.Join(cfg, rel)
		}
		return []Candidate{
			{ID: "chrome", Name: "Google Chrome", PathCmds: []string{"google-chrome", "google-chrome-stable"}, Paths: []string{"/opt/google/chrome/chrome"}, DataDir: dd("google-chrome")},
			{ID: "chrome-beta", Name: "Google Chrome Beta", PathCmds: []string{"google-chrome-beta"}, DataDir: dd("google-chrome-beta")},
			{ID: "chromium", Name: "Chromium", PathCmds: []string{"chromium", "chromium-browser"}, DataDir: dd("chromium")},
			{ID: "edge", Name: "Microsoft Edge", PathCmds: []string{"microsoft-edge", "microsoft-edge-stable"}, DataDir: dd("microsoft-edge")},
			{ID: "brave", Name: "Brave", PathCmds: []string{"brave-browser", "brave"}, DataDir: dd("BraveSoftware/Brave-Browser")},
		}
	}
}

// Locator is the BrowserLocator for the current machine.
type Locator struct {
	GOOS     string
	Env      func(string) string
	Exists   func(string) bool
	LookPath func(string) (string, error)
	Custom   []port.Browser // from config.browsers.custom
}

// New returns a Locator for the running OS.
func New(custom []port.Browser) *Locator {
	return &Locator{GOOS: runtime.GOOS, Env: os.Getenv, Exists: fileExists, LookPath: exec.LookPath, Custom: custom}
}

// Browsers returns installed browsers in priority order, followed by custom ones.
func (l *Locator) Browsers() []port.Browser {
	var out []port.Browser
	for _, c := range Candidates(l.GOOS, l.Env) {
		if p := l.find(c); p != "" {
			out = append(out, port.Browser{ID: c.ID, Name: c.Name, Path: p, DefaultUserDataDir: c.DataDir})
		}
	}
	for _, c := range l.Custom {
		if l.Exists(c.Path) {
			out = append(out, c)
		}
	}
	return out
}

func (l *Locator) find(c Candidate) string {
	for _, p := range c.Paths {
		if l.Exists(p) {
			return p
		}
	}
	for _, name := range c.PathCmds {
		if p, err := l.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

// Pick returns the browser with id, or the first installed one for "auto"/"".
func Pick(list []port.Browser, id string) (port.Browser, bool) {
	if id == "" || id == "auto" {
		if len(list) > 0 {
			return list[0], true
		}
		return port.Browser{}, false
	}
	for _, b := range list {
		if b.ID == id {
			return b, true
		}
	}
	return port.Browser{}, false
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
