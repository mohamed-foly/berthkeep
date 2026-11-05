package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/berthkeep/berthkeep/internal/install"
)

func init() {
	register("install", "add a Start-menu/menu entry: install [--path] [--autostart]", runInstall)
	register("uninstall", "remove what install added", runUninstall)
}

func exePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return exe, nil
}

func runInstall(e *Env, args []string) error {
	fs := newFlags("install", e)
	path := fs.Bool("path", false, "put berthkeep and bkeep on the user PATH")
	auto := fs.Bool("autostart", false, "start in the tray at login")
	if _, err := parseInterleaved(fs, args); err != nil {
		return errUsage
	}
	exe, err := exePath()
	if err != nil {
		return err
	}
	done, err := install.New(exe).Install(install.Options{Path: *path, Autostart: *auto})
	for _, d := range done {
		fmt.Fprintln(e.Out, d)
	}
	return err
}

func runUninstall(e *Env, _ []string) error {
	exe, err := exePath()
	if err != nil {
		return err
	}
	done, err := install.New(exe).Uninstall()
	for _, d := range done {
		fmt.Fprintln(e.Out, d)
	}
	if len(done) == 0 && err == nil {
		fmt.Fprintln(e.Out, "nothing to remove")
	}
	return err
}
