//go:build windows

package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// Uses a throw-away registry key and temp folders; never the real PATH or Start menu.
func TestInstallUninstall(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "berthkeep.exe")
	os.WriteFile(exe, []byte("x"), 0o755)
	os.WriteFile(filepath.Join(dir, "berthkeepw.exe"), []byte("x"), 0o755)
	base := `Software\BerthkeepTest`
	t.Cleanup(func() {
		registry.DeleteKey(registry.CURRENT_USER, base+`\Env`)
		registry.DeleteKey(registry.CURRENT_USER, base+`\Run`)
		registry.DeleteKey(registry.CURRENT_USER, base)
	})
	var link, target string
	w := &Windows{Exe: exe, StartMenu: filepath.Join(dir, "menu"), EnvKey: base + `\Env`, RunKey: base + `\Run`,
		ValueName: "Berthkeep", AliasName: "bkeep.exe",
		MakeLink: func(l, tg, _, _ string) error { link, target = l, tg; return os.WriteFile(l, nil, 0o644) }}
	os.MkdirAll(w.StartMenu, 0o755)
	if _, err := w.Install(Options{Path: true, Autostart: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(target, "berthkeepw.exe") || !strings.HasSuffix(link, "Berthkeep.lnk") {
		t.Fatalf("shortcut %s -> %s", link, target)
	}
	if got := readValue(t, w.EnvKey, "Path"); !strings.EqualFold(got, dir) {
		t.Fatalf("path value %q", got)
	}
	if _, err := w.Install(Options{Path: true}); err != nil { // idempotent
		t.Fatal(err)
	}
	if got := readValue(t, w.EnvKey, "Path"); !strings.EqualFold(got, dir) {
		t.Fatalf("path duplicated: %q", got)
	}
	if run := readValue(t, w.RunKey, "Berthkeep"); !strings.Contains(run, `berthkeepw.exe" gui --tray`) {
		t.Fatalf("run value %q", run)
	}
	if _, err := os.Stat(filepath.Join(dir, "bkeep.exe")); err != nil {
		t.Fatal("bkeep alias missing")
	}
	if _, err := w.Uninstall(); err != nil {
		t.Fatal(err)
	}
	if got := readValue(t, w.EnvKey, "Path"); got != "" {
		t.Fatalf("path not cleaned: %q", got)
	}
	if got := readValue(t, w.RunKey, "Berthkeep"); got != "" {
		t.Fatalf("autostart not removed: %q", got)
	}
	for _, p := range []string{link, filepath.Join(dir, "bkeep.exe")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s still exists", p)
		}
	}
}

func readValue(t *testing.T, key, name string) string {
	t.Helper()
	k, err := registry.OpenKey(registry.CURRENT_USER, key, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, _ := k.GetStringValue(name)
	return v
}

func TestShortcutIsReal(t *testing.T) {
	dir := t.TempDir()
	lnk := filepath.Join(dir, "x.lnk")
	if err := shortcut(lnk, os.Args[0], "", dir); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(lnk); err != nil || st.Size() == 0 {
		t.Fatalf("lnk not written: %v", err)
	}
}
