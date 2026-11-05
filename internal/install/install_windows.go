//go:build windows

package install

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Windows holds the locations used; tests point them elsewhere.
type Windows struct {
	Exe       string
	StartMenu string // folder for Berthkeep.lnk
	EnvKey    string // HKCU key holding Path
	RunKey    string // HKCU key for autostart
	Broadcast func() // tells Explorer the environment changed
	MakeLink  func(lnk, target, args, workdir string) error
	ValueName string
	AliasName string
}

// New returns the installer for the current user.
func New(exe string) *Windows {
	return &Windows{
		Exe:       exe,
		StartMenu: filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs`),
		EnvKey:    `Environment`,
		RunKey:    `Software\Microsoft\Windows\CurrentVersion\Run`,
		Broadcast: broadcastEnv,
		MakeLink:  shortcut,
		ValueName: "Berthkeep",
		AliasName: "bkeep.exe",
	}
}

func (w *Windows) lnk() string { return filepath.Join(w.StartMenu, "Berthkeep.lnk") }

// Install applies o and returns what it did.
func (w *Windows) Install(o Options) ([]string, error) {
	var done []string
	launcher := Launcher(w.Exe)
	if err := w.MakeLink(w.lnk(), launcher, "", filepath.Dir(w.Exe)); err != nil {
		return done, fmt.Errorf("shortcut: %w", err)
	}
	done = append(done, "Start menu shortcut: "+w.lnk())
	if o.Path {
		alias := filepath.Join(filepath.Dir(w.Exe), w.AliasName)
		if _, err := os.Stat(alias); errors.Is(err, os.ErrNotExist) {
			if err := os.Link(w.Exe, alias); err != nil {
				return done, fmt.Errorf("bkeep alias: %w", err)
			}
		}
		changed, err := w.editPath(func(parts []string) []string {
			if indexFold(parts, filepath.Dir(w.Exe)) < 0 {
				parts = append(parts, filepath.Dir(w.Exe))
			}
			return parts
		})
		if err != nil {
			return done, fmt.Errorf("PATH: %w", err)
		}
		if changed {
			done = append(done, "added to user PATH: "+filepath.Dir(w.Exe)+" (new terminals only)")
		}
		done = append(done, "alias: "+alias)
	}
	if o.Autostart {
		k, _, err := registry.CreateKey(registry.CURRENT_USER, w.RunKey, registry.SET_VALUE)
		if err != nil {
			return done, fmt.Errorf("autostart: %w", err)
		}
		defer k.Close()
		if err := k.SetStringValue(w.ValueName, fmt.Sprintf(`"%s" gui --tray`, launcher)); err != nil {
			return done, fmt.Errorf("autostart: %w", err)
		}
		done = append(done, "starts in the tray at login")
	}
	return done, nil
}

// Uninstall removes everything Install may have added.
func (w *Windows) Uninstall() ([]string, error) {
	var done []string
	if err := os.Remove(w.lnk()); err == nil {
		done = append(done, "removed shortcut")
	}
	if changed, err := w.editPath(func(parts []string) []string {
		if i := indexFold(parts, filepath.Dir(w.Exe)); i >= 0 {
			parts = append(parts[:i], parts[i+1:]...)
		}
		return parts
	}); err == nil && changed {
		done = append(done, "removed from user PATH")
	}
	if err := os.Remove(filepath.Join(filepath.Dir(w.Exe), w.AliasName)); err == nil {
		done = append(done, "removed bkeep alias")
	}
	if k, err := registry.OpenKey(registry.CURRENT_USER, w.RunKey, registry.SET_VALUE); err == nil {
		if k.DeleteValue(w.ValueName) == nil {
			done = append(done, "removed autostart")
		}
		k.Close()
	}
	return done, nil
}

// editPath rewrites the user Path value; it reports whether it changed.
func (w *Windows) editPath(fn func([]string) []string) (bool, error) {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, w.EnvKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false, err
	}
	defer k.Close()
	cur, _, err := k.GetStringValue("Path")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return false, err
	}
	var parts []string
	for _, p := range strings.Split(cur, ";") {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	next := strings.Join(fn(append([]string(nil), parts...)), ";")
	if next == strings.Join(parts, ";") {
		return false, nil
	}
	if err := k.SetExpandStringValue("Path", next); err != nil {
		return false, err
	}
	if w.Broadcast != nil {
		w.Broadcast()
	}
	return true, nil
}

func indexFold(list []string, v string) int {
	for i, x := range list {
		if strings.EqualFold(strings.TrimRight(x, `\`), strings.TrimRight(v, `\`)) {
			return i
		}
	}
	return -1
}

// shortcut writes a .lnk through the WScript.Shell COM object.
func shortcut(lnk, target, args, workdir string) error {
	if err := os.MkdirAll(filepath.Dir(lnk), 0o755); err != nil {
		return err
	}
	script := `$s = (New-Object -ComObject WScript.Shell).CreateShortcut($env:BK_LNK); $s.TargetPath = $env:BK_TARGET; $s.Arguments = $env:BK_ARGS; $s.WorkingDirectory = $env:BK_DIR; $s.IconLocation = "$env:BK_TARGET,0"; $s.Description = 'Berthkeep'; $s.Save()`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = append(os.Environ(), "BK_LNK="+lnk, "BK_TARGET="+target, "BK_ARGS="+args, "BK_DIR="+workdir)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// broadcastEnv sends WM_SETTINGCHANGE so new shells see the PATH.
func broadcastEnv() {
	send := windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW")
	env, _ := windows.UTF16PtrFromString("Environment")
	const hwndBroadcast, wmSettingChange, smtoAbortIfHung = 0xffff, 0x001A, 0x0002
	var result uintptr
	_, _, _ = send.Call(hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(env)), smtoAbortIfHung, 5000, uintptr(unsafe.Pointer(&result)))
}
