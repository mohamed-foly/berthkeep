// Package paths resolves the Berthkeep home directory and normalises filesystem paths.
package paths

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
)

// AppName is used for per-user data directories.
const AppName = "berthkeep"

// EnvHome overrides the home directory.
const EnvHome = "BERTHKEEP_HOME"

// Env abstracts environment lookup for testing.
type Env func(key string) string

// HomeInput holds ResolveHome's inputs.
type HomeInput struct {
	Flag    string // --home
	Env     Env
	ExePath string // os.Executable()
	GOOS    string
	TempDir string
	// Writable reports whether dir can be written to.
	Writable func(dir string) bool
	// Exists reports whether a directory exists.
	Exists func(dir string) bool
}

// ResolveHome picks --home, the env var, <exe-dir>/data, then the per-user dir.
func ResolveHome(in HomeInput) (string, error) {
	if in.Flag != "" {
		return filepath.Abs(in.Flag)
	}
	if v := in.Env(EnvHome); v != "" {
		return filepath.Abs(v)
	}
	if in.ExePath != "" {
		exeDir := filepath.Dir(in.ExePath)
		data := filepath.Join(exeDir, "data")
		if in.Exists(data) {
			return data, nil
		}
		if !isUnder(exeDir, in.TempDir) && !isSystemLocation(exeDir, in.GOOS) && in.Writable(exeDir) {
			return data, nil
		}
	}
	return perUserDir(in)
}

// Home resolves the home for the current process.
func Home(flag string) (string, error) {
	exe, _ := os.Executable()
	if exe != "" {
		if r, err := filepath.EvalSymlinks(exe); err == nil {
			exe = r
		}
	}
	return ResolveHome(HomeInput{
		Flag: flag, Env: os.Getenv, ExePath: exe, GOOS: runtime.GOOS, TempDir: os.TempDir(),
		Writable: writable, Exists: dirExists,
	})
}

func perUserDir(in HomeInput) (string, error) {
	switch in.GOOS {
	case "windows":
		if v := in.Env("LOCALAPPDATA"); v != "" {
			return filepath.Join(v, AppName), nil
		}
	case "darwin":
		if h := in.Env("HOME"); h != "" {
			return filepath.Join(h, "Library", "Application Support", AppName), nil
		}
	default:
		if v := in.Env("XDG_DATA_HOME"); v != "" {
			return filepath.Join(v, AppName), nil
		}
		if h := in.Env("HOME"); h != "" {
			return filepath.Join(h, ".local", "share", AppName), nil
		}
	}
	return "", errors.New("cannot determine a per-user data directory; set " + EnvHome)
}

func isSystemLocation(dir, goos string) bool {
	d := strings.ToLower(filepath.ToSlash(dir))
	switch goos {
	case "windows":
		return strings.Contains(d, "/program files") || strings.Contains(d, "/windows/")
	case "darwin":
		return strings.HasPrefix(d, "/applications/") || strings.HasPrefix(d, "/usr/") || strings.HasPrefix(d, "/opt/homebrew/")
	default:
		return strings.HasPrefix(d, "/usr/") || strings.HasPrefix(d, "/bin") || strings.HasPrefix(d, "/opt/") || strings.HasPrefix(d, "/snap/")
	}
}

func isUnder(dir, root string) bool {
	if root == "" {
		return false
	}
	d, r := NormKey(dir), NormKey(root)
	return d == r || strings.HasPrefix(d, r+"/")
}

func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".berthkeep-probe-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}

func dirExists(dir string) bool {
	st, err := os.Stat(dir)
	return err == nil && st.IsDir()
}

// NormKey returns a case- and separator-insensitive path key.
func NormKey(p string) string {
	s := strings.ReplaceAll(strings.TrimSpace(p), `\`, "/")
	for strings.Contains(s, "//") {
		s = strings.ReplaceAll(s, "//", "/")
	}
	if len(s) > 1 {
		s = strings.TrimRight(s, "/")
	}
	if isDrivePath(s) {
		return strings.ToLower(s)
	}
	return s
}

// SamePath reports whether two paths refer to the same location by key.
func SamePath(a, b string) bool { return NormKey(a) == NormKey(b) }

// NativePath makes p absolute and cleaned with OS separators.
func NativePath(p string) (string, error) {
	a, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.Clean(a), nil
}

// Variants returns slash, backslash and escaped spellings.
func Variants(p string) []string {
	f := strings.ReplaceAll(filepath.ToSlash(p), `\`, "/")
	b := strings.ReplaceAll(f, "/", `\`)
	bb := strings.ReplaceAll(f, "/", `\\`)
	out := []string{f}
	for _, v := range []string{b, bb} {
		if v != out[len(out)-1] && v != f {
			out = append(out, v)
		}
	}
	return out
}

// IsWithin reports whether path is inside root (or equal to it).
func IsWithin(path, root string) bool { return isUnder(path, root) }

func isDrivePath(s string) bool {
	return len(s) >= 2 && s[1] == ':' && unicode.IsLetter(rune(s[0]))
}

// Slugify turns a title into a URL-safe slug.
func Slugify(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		default:
			if b.Len() > 0 && !dash {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > 48 {
		s = strings.Trim(s[:48], "-")
	}
	if s == "" {
		s = "session"
	}
	return s
}
