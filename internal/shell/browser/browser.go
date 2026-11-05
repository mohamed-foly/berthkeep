// Package browser opens URLs and folders with the OS defaults.
package browser

import (
	"os/exec"
	"path/filepath"
	"runtime"
)

// Open shows url in the default browser.
func Open(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

// Reveal shows path in the file manager.
func Reveal(path string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer", "/select,", filepath.Clean(path)).Start()
	case "darwin":
		return exec.Command("open", "-R", path).Start()
	default:
		return exec.Command("xdg-open", filepath.Dir(path)).Start()
	}
}

// UI opens a fixed URL in the default browser.
type UI struct{ URL string }

// Open implements shell.UI.
func (u UI) Open() error { return Open(u.URL) }
