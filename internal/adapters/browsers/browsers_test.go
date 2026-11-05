package browsers

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/mohamed-foly/berthkeep/internal/port"
)

func TestWindowsCandidatesAndPriority(t *testing.T) {
	env := map[string]string{"ProgramFiles": `C:\PF`, "ProgramFiles(x86)": `C:\PF86`, "LOCALAPPDATA": `C:\U\L`}
	installed := map[string]bool{
		filepath.Join(`C:\PF86`, `Microsoft\Edge\Application\msedge.exe`): true,
		filepath.Join(`C:\PF`, `Google\Chrome\Application\chrome.exe`):    true,
	}
	l := &Locator{GOOS: "windows", Env: func(k string) string { return env[k] }, Exists: func(p string) bool { return installed[p] },
		LookPath: func(string) (string, error) { return "", errors.New("no") }}
	got := l.Browsers()
	if len(got) != 2 || got[0].ID != "chrome" || got[1].ID != "edge" {
		t.Fatalf("unexpected browsers: %+v", got)
	}
	if got[0].DefaultUserDataDir != filepath.Join(`C:\U\L`, `Google\Chrome`, "User Data") {
		t.Fatalf("default data dir: %s", got[0].DefaultUserDataDir)
	}
	if b, ok := Pick(got, "auto"); !ok || b.ID != "chrome" {
		t.Fatal("auto should pick chrome first")
	}
	if b, ok := Pick(got, "edge"); !ok || b.ID != "edge" {
		t.Fatal("pick edge")
	}
	if _, ok := Pick(got, "brave"); ok {
		t.Fatal("brave is not installed")
	}
}

func TestLinuxUsesPathLookupAndCustom(t *testing.T) {
	l := &Locator{GOOS: "linux", Env: func(k string) string { return map[string]string{"HOME": "/home/u"}[k] },
		Exists: func(p string) bool { return p == "/opt/cft/chrome" },
		LookPath: func(n string) (string, error) {
			if n == "chromium-browser" {
				return "/usr/bin/chromium-browser", nil
			}
			return "", errors.New("no")
		},
		Custom: []port.Browser{{ID: "cft", Name: "Chrome for Testing", Path: "/opt/cft/chrome"}, {ID: "gone", Path: "/nope"}},
	}
	got := l.Browsers()
	if len(got) != 2 || got[0].ID != "chromium" || got[0].Path != "/usr/bin/chromium-browser" || got[1].ID != "cft" {
		t.Fatalf("unexpected: %+v", got)
	}
	if filepath.ToSlash(got[0].DefaultUserDataDir) != "/home/u/.config/chromium" {
		t.Fatalf("data dir: %s", got[0].DefaultUserDataDir)
	}
}

func TestDarwinCandidates(t *testing.T) {
	c := Candidates("darwin", func(k string) string { return map[string]string{"HOME": "/Users/u"}[k] })
	if c[0].ID != "chrome" || filepath.ToSlash(c[0].Paths[0]) != "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" {
		t.Fatalf("unexpected: %+v", c[0])
	}
}
