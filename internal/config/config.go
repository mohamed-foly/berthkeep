// Package config loads and saves <home>/config.json.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/berthkeep/berthkeep/internal/fsutil"
)

// FileName is the config file inside the home directory.
const FileName = "config.json"

type Server struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	OpenOnStart bool   `json:"openOnStart"`
}

type Ports struct {
	RangeStart int   `json:"rangeStart"`
	RangeEnd   int   `json:"rangeEnd"`
	Reserved   []int `json:"reserved,omitempty"`
}

type Window struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type Defaults struct {
	Browser     string   `json:"browser"`
	Mode        string   `json:"mode"`
	Persistence string   `json:"persistence"`
	Window      Window   `json:"window"`
	Args        []string `json:"args"`
}

type CustomBrowser struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

type Browsers struct {
	Custom []CustomBrowser `json:"custom,omitempty"`
}

type Clone struct {
	SkipCaches bool `json:"skipCaches"`
}

type Stop struct {
	GraceMs int `json:"graceMs"`
}

type ScanSource struct {
	Type string `json:"type"` // claude | codex | auto
	Path string `json:"path"`
}

type Scan struct {
	Sources             []ScanSource `json:"sources,omitempty"`
	AutoDetect          bool         `json:"autoDetect"`
	IncludeProjectFiles bool         `json:"includeProjectFiles"`
	MaxProjectFiles     int          `json:"maxProjectFiles"`
	ExcludePaths        []string     `json:"excludePaths,omitempty"`
}

// Config is the user-editable configuration.
type Config struct {
	Server   Server   `json:"server"`
	Ports    Ports    `json:"ports"`
	Defaults Defaults `json:"defaults"`
	Browsers Browsers `json:"browsers"`
	Clone    Clone    `json:"clone"`
	Stop     Stop     `json:"stop"`
	Scan     Scan     `json:"scan"`
}

// Default returns the decided defaults.
func Default() Config {
	return Config{
		Server:   Server{Host: "127.0.0.1", Port: 9221, OpenOnStart: true},
		Ports:    Ports{RangeStart: 9222, RangeEnd: 9299},
		Defaults: Defaults{Browser: "auto", Mode: "headed", Persistence: "persistent", Window: Window{Width: 1280, Height: 900}, Args: []string{"--no-first-run", "--no-default-browser-check"}},
		Clone:    Clone{SkipCaches: true},
		Stop:     Stop{GraceMs: 8000},
		Scan:     Scan{AutoDetect: true, IncludeProjectFiles: true, MaxProjectFiles: 20000},
	}
}

// Validate checks values that would make the tool misbehave.
func (c Config) Validate() error {
	var errs []error
	if c.Server.Host != "127.0.0.1" && c.Server.Host != "localhost" {
		errs = append(errs, fmt.Errorf("server.host must be loopback (127.0.0.1), got %q", c.Server.Host))
	}
	if !validPort(c.Server.Port) {
		errs = append(errs, fmt.Errorf("server.port %d out of range", c.Server.Port))
	}
	if !validPort(c.Ports.RangeStart) || !validPort(c.Ports.RangeEnd) || c.Ports.RangeStart > c.Ports.RangeEnd {
		errs = append(errs, fmt.Errorf("ports range %d-%d invalid", c.Ports.RangeStart, c.Ports.RangeEnd))
	}
	if c.Server.Port >= c.Ports.RangeStart && c.Server.Port <= c.Ports.RangeEnd {
		errs = append(errs, fmt.Errorf("server.port %d must be outside the CDP range", c.Server.Port))
	}
	if c.Defaults.Mode != "headed" && c.Defaults.Mode != "headless" {
		errs = append(errs, fmt.Errorf("defaults.mode must be headed or headless"))
	}
	if c.Defaults.Persistence != "persistent" && c.Defaults.Persistence != "ephemeral" {
		errs = append(errs, fmt.Errorf("defaults.persistence must be persistent or ephemeral"))
	}
	return errors.Join(errs...)
}

func validPort(p int) bool { return p >= 1024 && p <= 65535 }

// Load reads <home>/config.json, filling missing fields with defaults.
func Load(home string) (Config, error) {
	c := Default()
	b, err := os.ReadFile(filepath.Join(home, FileName))
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("config.json: %w", err)
	}
	return c, c.Validate()
}

// Save validates and writes the config atomically.
func Save(home string, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(filepath.Join(home, FileName), append(b, '\n'), 0o600)
}
