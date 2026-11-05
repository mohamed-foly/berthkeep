package paths

import (
	"path/filepath"
	"testing"
)

func env(m map[string]string) Env { return func(k string) string { return m[k] } }

func TestResolveHome(t *testing.T) {
	yes := func(string) bool { return true }
	no := func(string) bool { return false }
	tests := []struct {
		name string
		in   HomeInput
		want string
	}{
		{"flag wins", HomeInput{Flag: "/x/flag", Env: env(map[string]string{EnvHome: "/x/env"}), ExePath: "/opt/app/berthkeep", GOOS: "linux", Writable: yes, Exists: yes}, "/x/flag"},
		{"env next", HomeInput{Env: env(map[string]string{EnvHome: "/x/env"}), ExePath: "/srv/app/berthkeep", GOOS: "linux", Writable: yes, Exists: yes}, "/x/env"},
		{"existing data dir next to exe", HomeInput{Env: env(nil), ExePath: "/usr/bin/berthkeep", GOOS: "linux", Writable: no, Exists: yes}, "/usr/bin/data"},
		{"writable portable dir", HomeInput{Env: env(nil), ExePath: "/srv/tools/bk/berthkeep", GOOS: "linux", Writable: yes, Exists: no}, "/srv/tools/bk/data"},
		{"system location falls back to per-user", HomeInput{Env: env(map[string]string{"HOME": "/home/u"}), ExePath: "/usr/local/bin/berthkeep", GOOS: "linux", Writable: yes, Exists: no}, "/home/u/.local/share/berthkeep"},
		{"go run temp dir falls back", HomeInput{Env: env(map[string]string{"XDG_DATA_HOME": "/xdg"}), ExePath: "/tmp/go-build1/exe/berthkeep", GOOS: "linux", TempDir: "/tmp", Writable: yes, Exists: no}, "/xdg/berthkeep"},
		{"darwin per-user", HomeInput{Env: env(map[string]string{"HOME": "/Users/u"}), ExePath: "/Applications/Berthkeep.app/Contents/MacOS/berthkeep", GOOS: "darwin", Writable: yes, Exists: no}, "/Users/u/Library/Application Support/berthkeep"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveHome(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			absWant, _ := filepath.Abs(tt.want)
			if NormKey(got) != NormKey(tt.want) && NormKey(got) != NormKey(absWant) {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestNormKey(t *testing.T) {
	tests := map[string]string{
		`D:\Work\App\login-session\`: "d:/work/app/login-session",
		`D:/work//app/Login-Session`: "d:/work/app/login-session",
		`d:\\work\\app`:              "d:/work/app",
		"/home/u/Profile/":           "/home/u/Profile",
		"/":                          "/",
	}
	for in, want := range tests {
		if got := NormKey(in); got != want {
			t.Errorf("NormKey(%q)=%q want %q", in, got, want)
		}
	}
	if !SamePath(`C:\Users\x\AppData\Local\p`, "c:/users/X/appdata/local/P/") {
		t.Error("SamePath should ignore case and separators on drive paths")
	}
}

func TestVariants(t *testing.T) {
	got := Variants(`D:\a\b`)
	want := []string{"D:/a/b", `D:\a\b`, `D:\\a\\b`}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("variant %d = %q want %q", i, got[i], want[i])
		}
	}
	if v := Variants("/home/u/p"); len(v) != 3 {
		t.Errorf("posix variants: %v", v)
	}
}

func TestSlugify(t *testing.T) {
	tests := map[string]string{
		"Shop admin (staging)": "shop-admin-staging",
		"  --Hello__World--  ": "hello-world",
		"تجربة":                "session",
		"web-gpt-2":            "web-gpt-2",
	}
	for in, want := range tests {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q)=%q want %q", in, got, want)
		}
	}
}

func TestIsWithin(t *testing.T) {
	if !IsWithin(`D:\home\profiles\a`, `d:/home`) || IsWithin(`D:\homework`, `D:\home`) {
		t.Error("IsWithin boundary check failed")
	}
}
