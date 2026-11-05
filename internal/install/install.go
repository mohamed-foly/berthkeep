// Package install adds or removes OS integration: shortcut, PATH, autostart.
package install

import (
	"os"
	"path/filepath"
	"runtime"
)

// Options select what to install.
type Options struct {
	Path      bool // put berthkeep and bkeep on the user PATH
	Autostart bool // start in the tray at login
}

// Launcher returns the program a shortcut starts: the windowless
// berthkeepw.exe on Windows when present, else exe.
func Launcher(exe string) string {
	if runtime.GOOS == "windows" {
		w := filepath.Join(filepath.Dir(exe), "berthkeepw.exe")
		if _, err := os.Stat(w); err == nil {
			return w
		}
	}
	return exe
}
