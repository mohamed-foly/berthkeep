//go:build !windows

package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Unix writes .desktop files (Linux) or a LaunchAgent (macOS) and ~/.local/bin links.
type Unix struct {
	Exe  string
	Home string // user home
	GOOS string
}

// New returns the installer for the current user.
func New(exe string) *Unix {
	h, _ := os.UserHomeDir()
	return &Unix{Exe: exe, Home: h, GOOS: runtime.GOOS}
}

func (u *Unix) desktop() string {
	return filepath.Join(u.Home, ".local", "share", "applications", "berthkeep.desktop")
}

func (u *Unix) autostart() string {
	if u.GOOS == "darwin" {
		return filepath.Join(u.Home, "Library", "LaunchAgents", "io.berthkeep.plist")
	}
	return filepath.Join(u.Home, ".config", "autostart", "berthkeep.desktop")
}

func (u *Unix) bin(name string) string { return filepath.Join(u.Home, ".local", "bin", name) }

func entry(exe, args string) string {
	return fmt.Sprintf("[Desktop Entry]\nType=Application\nName=Berthkeep\nComment=Chromium CDP session manager\nExec=\"%s\" %s\nTerminal=false\nCategories=Development;\n", exe, args)
}

const plist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>io.berthkeep</string>
<key>ProgramArguments</key><array><string>%s</string><string>gui</string><string>--tray</string></array>
<key>RunAtLoad</key><true/>
</dict></plist>
`

// Install applies o and returns what it did.
func (u *Unix) Install(o Options) ([]string, error) {
	var done []string
	if u.GOOS != "darwin" {
		if err := write(u.desktop(), entry(u.Exe, "ui")); err != nil {
			return done, err
		}
		done = append(done, "menu entry: "+u.desktop())
	}
	if o.Path {
		for _, name := range []string{"berthkeep", "bkeep"} {
			link := u.bin(name)
			_ = os.MkdirAll(filepath.Dir(link), 0o755)
			_ = os.Remove(link)
			if err := os.Symlink(u.Exe, link); err != nil {
				return done, err
			}
		}
		done = append(done, "linked berthkeep and bkeep in "+filepath.Dir(u.bin("x"))+" (keep it on PATH)")
	}
	if o.Autostart {
		body := entry(u.Exe, "gui --tray")
		if u.GOOS == "darwin" {
			body = fmt.Sprintf(plist, u.Exe)
		}
		if err := write(u.autostart(), body); err != nil {
			return done, err
		}
		done = append(done, "starts in the tray at login")
	}
	return done, nil
}

// Uninstall removes everything Install may have added.
func (u *Unix) Uninstall() ([]string, error) {
	var done []string
	for _, p := range []string{u.desktop(), u.autostart(), u.bin("berthkeep"), u.bin("bkeep")} {
		if err := os.Remove(p); err == nil {
			done = append(done, "removed "+p)
		} else if !errors.Is(err, os.ErrNotExist) {
			return done, err
		}
	}
	return done, nil
}

func write(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o644)
}
