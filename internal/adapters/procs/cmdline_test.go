package procs

import (
	"testing"

	"github.com/mohamed-foly/berthkeep/internal/port"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want Flags
	}{
		{"windows quoted exe and dir",
			`"C:\Program Files\Google\Chrome\Application\chrome.exe" --remote-debugging-port=9222 --user-data-dir="C:\Users\u\AppData\Local\tv profile" --restore-last-session`,
			Flags{Exe: `C:\Program Files\Google\Chrome\Application\chrome.exe`, UserDataDir: `C:\Users\u\AppData\Local\tv profile`, Port: 9222, PortSet: true}},
		{"forward slashes unquoted",
			`"C:\Program Files\Google\Chrome\Application\chrome.exe" --disable-sync --remote-debugging-address=127.0.0.1 --remote-debugging-port=9333 --user-data-dir=D:/work/app/login-session --window-size=1440,1000`,
			Flags{Exe: `C:\Program Files\Google\Chrome\Application\chrome.exe`, UserDataDir: "D:/work/app/login-session", Port: 9333, PortSet: true}},
		{"child renderer",
			`chrome --type=renderer --user-data-dir=/home/u/p --remote-debugging-port=9222`,
			Flags{Exe: "chrome", UserDataDir: "/home/u/p", Port: 9222, PortSet: true, Type: "renderer"}},
		{"headless auto port, space-separated value",
			`/opt/google/chrome/chrome --headless=new --remote-debugging-port 0 --user-data-dir /tmp/x about:blank`,
			Flags{Exe: "/opt/google/chrome/chrome", UserDataDir: "/tmp/x", Port: 0, PortSet: true, Headless: true}},
		{"app window", `msedge.exe --app=http://127.0.0.1:9221/ --user-data-dir=C:\h\ui-profile`,
			Flags{Exe: "msedge.exe", UserDataDir: `C:\h\ui-profile`, App: "http://127.0.0.1:9221/"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Parse(tt.cmd); got != tt.want {
				t.Fatalf("\n got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestMainDebugBrowsers(t *testing.T) {
	list := []port.Process{
		{PID: 1, CommandLine: `chrome.exe --remote-debugging-port=9222 --user-data-dir=C:\p`},
		{PID: 2, CommandLine: `chrome.exe --type=gpu-process --remote-debugging-port=9222 --user-data-dir=C:\p`},
		{PID: 3, CommandLine: `chrome.exe --user-data-dir=C:\q`},
	}
	got := MainDebugBrowsers(list)
	if len(got) != 1 || got[0].PID != 1 || got[0].Flags.Port != 9222 {
		t.Fatalf("unexpected: %+v", got)
	}
}
