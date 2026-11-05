package launcher

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBuildArgs(t *testing.T) {
	got := BuildArgs(Spec{
		ProfileDir: `C:\h\profiles\shop`, Port: 9230, Headless: true, Width: 1280, Height: 900,
		BaseArgs:  []string{"--no-first-run", "--remote-allow-origins=*"},
		ExtraArgs: []string{"--lang=en", "--user-data-dir=C:\\evil", "--remote-debugging-address=0.0.0.0"},
		URLs:      []string{"https://example.com"},
	})
	want := []string{`--user-data-dir=C:\h\profiles\shop`, "--remote-debugging-port=9230", "--remote-debugging-address=127.0.0.1",
		"--no-first-run", "--headless=new", "--window-size=1280,900", "--lang=en", "https://example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("\n got %q\nwant %q", got, want)
	}
}

func TestBuildArgsAutoPortAndApp(t *testing.T) {
	got := BuildArgs(Spec{ProfileDir: "/p", Port: 0})
	if got[1] != "--remote-debugging-port=0" || got[len(got)-1] != "about:blank" {
		t.Fatalf("auto: %q", got)
	}
	app := BuildArgs(Spec{App: true, Port: 9222, Headless: true, ExtraArgs: []string{"--foo"}})
	if !reflect.DeepEqual(app, []string{"--remote-debugging-port=9222", "--foo"}) {
		t.Fatalf("app: %q", app)
	}
}

func TestReadDevToolsActivePort(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "DevToolsActivePort"), []byte("20833\r\n/devtools/browser/0aa3-guid\n"), 0o600)
	p, g, err := ReadDevToolsActivePort(dir)
	if err != nil || p != 20833 || g != "0aa3-guid" {
		t.Fatalf("got %d %q %v", p, g, err)
	}
}
