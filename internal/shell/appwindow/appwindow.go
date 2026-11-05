// Package appwindow shows the UI in a chromeless browser window.
package appwindow

import (
	"os/exec"
	"path/filepath"

	"github.com/mohamed-foly/berthkeep/internal/adapters/browsers"
	"github.com/mohamed-foly/berthkeep/internal/port"
	"github.com/mohamed-foly/berthkeep/internal/shell"
)

// UI opens URL with --app in its own small profile.
type UI struct {
	URL      string
	Home     string
	Browsers port.BrowserLocator
}

// Open implements shell.UI.
func (u UI) Open() error {
	b, ok := browsers.Pick(u.Browsers.Browsers(), "auto")
	if !ok {
		return shell.ErrUnavailable
	}
	args := []string{
		"--app=" + u.URL,
		"--user-data-dir=" + filepath.Join(u.Home, "ui-profile"),
		"--no-first-run", "--no-default-browser-check", "--window-size=1100,760",
		"--disable-extensions", "--disable-sync", "--disable-background-networking",
	}
	return exec.Command(b.Path, args...).Start()
}
