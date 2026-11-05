// Package launcher starts, detects and stops CDP browsers.
package launcher

import (
	"fmt"
	"strconv"
	"strings"
)

// Spec describes one launch.
type Spec struct {
	Exe        string
	ProfileDir string // native absolute path
	Port       int    // 0 = auto (DevToolsActivePort)
	Headless   bool
	App        bool // Chromium-based desktop app
	EnvStyle   bool // flags via ELECTRON_EXTRA_LAUNCH_ARGS
	Width      int
	Height     int
	BaseArgs   []string // config defaults, e.g. --no-first-run
	ExtraArgs  []string
	URLs       []string
	Env        map[string]string
	LogPath    string
}

// managedFlags are owned by Berthkeep.
var managedFlags = []string{"--user-data-dir", "--remote-debugging-port", "--remote-debugging-address", "--headless", "--remote-allow-origins"}

// BuildArgs returns the argument list for s.
func BuildArgs(s Spec) []string {
	var a []string
	if s.ProfileDir != "" {
		a = append(a, "--user-data-dir="+s.ProfileDir)
	}
	a = append(a, "--remote-debugging-port="+strconv.Itoa(s.Port))
	if !s.App {
		a = append(a, "--remote-debugging-address=127.0.0.1")
		a = append(a, filterManaged(s.BaseArgs)...)
		if s.Headless {
			a = append(a, "--headless=new")
		}
		if s.Width > 0 && s.Height > 0 {
			a = append(a, fmt.Sprintf("--window-size=%d,%d", s.Width, s.Height))
		}
	}
	a = append(a, filterManaged(s.ExtraArgs)...)
	if !s.App {
		urls := s.URLs
		if len(urls) == 0 {
			urls = []string{"about:blank"}
		}
		a = append(a, urls...)
	}
	return a
}

func filterManaged(in []string) []string {
	out := make([]string, 0, len(in))
	for _, x := range in {
		name, _, _ := strings.Cut(x, "=")
		keep := true
		for _, m := range managedFlags {
			if name == m {
				keep = false
				break
			}
		}
		if keep {
			out = append(out, x)
		}
	}
	return out
}
