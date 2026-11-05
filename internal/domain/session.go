// Package domain holds Berthkeep's core types.
package domain

import "time"

// Persistence says whether a session's profile survives a stop.
type Persistence string

const (
	Persistent Persistence = "persistent"
	Ephemeral  Persistence = "ephemeral"
)

// Mode is how the browser window is shown.
type Mode string

const (
	Headed   Mode = "headed"
	Headless Mode = "headless"
)

// Kind distinguishes browsers from Chromium-based desktop apps.
type Kind string

const (
	KindBrowser Kind = "browser"
	KindApp     Kind = "app"
)

// OriginType records how a session came to exist.
type OriginType string

const (
	OriginCreated  OriginType = "created"
	OriginCloned   OriginType = "cloned"
	OriginAdopted  OriginType = "adopted"
	OriginMigrated OriginType = "migrated"
)

// Origin is the provenance of a session.
type Origin struct {
	Type OriginType `json:"type"`
	From string     `json:"from,omitempty"`
	At   time.Time  `json:"at"`
}

// Aliases are former locations and ports (migration provenance).
type Aliases struct {
	Paths []string `json:"paths,omitempty"`
	Ports []int    `json:"ports,omitempty"`
}

// Window is the requested window size.
type Window struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Runtime is the last known launch.
type Runtime struct {
	PID       int       `json:"pid,omitempty"`
	Port      int       `json:"port,omitempty"`
	Mode      Mode      `json:"mode,omitempty"`
	WSGUID    string    `json:"wsGuid,omitempty"`
	WSPath    string    `json:"wsPath,omitempty"`
	StartedAt time.Time `json:"startedAt,omitempty"`
	LastTabs  []string  `json:"lastTabs,omitempty"`
}

// Session is the primary entity: one profile + one port + launch settings.
type Session struct {
	ID                string            `json:"id"`
	Slug              string            `json:"slug"`
	Title             string            `json:"title"`
	Labels            []string          `json:"labels,omitempty"`
	Notes             string            `json:"notes,omitempty"`
	Color             string            `json:"color,omitempty"`
	Port              *int              `json:"port"` // nil = auto-assign on start
	Persistence       Persistence       `json:"persistence"`
	AutoRemove        bool              `json:"autoRemove,omitempty"`
	ProfileDir        string            `json:"profileDir"` // relative = managed, absolute = external
	PendingRelocation string            `json:"pendingRelocation,omitempty"`
	Kind              Kind              `json:"kind"`
	Browser           string            `json:"browser"`
	BrowserPath       string            `json:"browserPath,omitempty"`
	AppArgsStyle      string            `json:"appArgsStyle,omitempty"`
	Mode              Mode              `json:"mode"`
	StartURLs         []string          `json:"startUrls,omitempty"`
	RestoreTabs       bool              `json:"restoreTabs"`
	Window            *Window           `json:"window,omitempty"`
	ExtraArgs         []string          `json:"extraArgs,omitempty"`
	Env               map[string]string `json:"env,omitempty"`
	Origin            Origin            `json:"origin"`
	Aliases           Aliases           `json:"aliases"`
	CreatedAt         time.Time         `json:"createdAt"`
	UpdatedAt         time.Time         `json:"updatedAt"`
	LastStartedAt     *time.Time        `json:"lastStartedAt,omitempty"`
	LastStoppedAt     *time.Time        `json:"lastStoppedAt,omitempty"`
	Runtime           *Runtime          `json:"runtime,omitempty"`
}

// FixedPort returns the configured port, or 0 when auto.
func (s Session) FixedPort() int {
	if s.Port == nil {
		return 0
	}
	return *s.Port
}

// IsManaged reports whether the profile lives inside the Berthkeep home.
func (s Session) IsManaged() bool {
	return s.ProfileDir != "" && !isAbs(s.ProfileDir)
}

func isAbs(p string) bool {
	if len(p) >= 2 && p[1] == ':' {
		return true
	}
	return len(p) > 0 && (p[0] == '/' || p[0] == '\\')
}
