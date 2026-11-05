// Package shell defines how the UI is shown.
package shell

import "errors"

// UI shows the manager window.
type UI interface {
	Open() error
}

// ErrUnavailable means this shell cannot run here.
var ErrUnavailable = errors.New("shell unavailable")
