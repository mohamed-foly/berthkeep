//go:build !windows

package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnixInstallUninstall(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		t.Run(goos, func(t *testing.T) {
			home := t.TempDir()
			exe := filepath.Join(home, "app", "berthkeep")
			os.MkdirAll(filepath.Dir(exe), 0o755)
			os.WriteFile(exe, []byte("x"), 0o755)
			u := &Unix{Exe: exe, Home: home, GOOS: goos}
			if _, err := u.Install(Options{Path: true, Autostart: true}); err != nil {
				t.Fatal(err)
			}
			auto, err := os.ReadFile(u.autostart())
			if err != nil || !strings.Contains(string(auto), "--tray") {
				t.Fatalf("autostart: %v %s", err, auto)
			}
			if target, err := os.Readlink(u.bin("bkeep")); err != nil || target != exe {
				t.Fatalf("bkeep link: %v %s", err, target)
			}
			if _, err := u.Uninstall(); err != nil {
				t.Fatal(err)
			}
			for _, p := range []string{u.desktop(), u.autostart(), u.bin("berthkeep"), u.bin("bkeep")} {
				if _, err := os.Lstat(p); !os.IsNotExist(err) {
					t.Fatalf("%s still exists", p)
				}
			}
		})
	}
}
