// Package port declares the interfaces the domain depends on.
package port

import "context"

// Browser is an installed Chromium-family browser (or a custom executable).
type Browser struct {
	ID   string `json:"id"`   // chrome, chrome-beta, edge, brave, chromium, custom ids…
	Name string `json:"name"` // display name
	Path string `json:"path"` // executable
	// DefaultUserDataDir is the browser's own profile root.
	DefaultUserDataDir string `json:"defaultUserDataDir,omitempty"`
}

// BrowserLocator finds installed browsers.
type BrowserLocator interface {
	Browsers() []Browser
}

// Process is one OS process with its command line.
type Process struct {
	PID         int
	PPID        int
	Name        string
	CommandLine string
}

// ProcessLister lists browser processes (main and child processes).
type ProcessLister interface {
	// List returns processes whose executable name is in names (case-insensitive).
	List(ctx context.Context, names []string) ([]Process, error)
}

// LockState is the state of a profile's single-instance lock.
type LockState string

const (
	LockFree  LockState = "free"  // no lock file
	LockHeld  LockState = "held"  // a live browser holds it
	LockStale LockState = "stale" // lock file left by a crash
)

// LockProbe reports whether a browser currently holds a profile.
type LockProbe interface {
	State(profileDir string) (LockState, error)
}
