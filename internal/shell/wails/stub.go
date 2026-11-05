//go:build server || (!windows && !cgo)

// Package wails is the native shell; this build has none.
package wails

import (
	"context"

	"github.com/berthkeep/berthkeep/internal/events"
	"github.com/berthkeep/berthkeep/internal/session"
	"github.com/berthkeep/berthkeep/internal/shell"
)

// Available reports whether this build has the native shell.
const Available = false

// Options configure the shell.
type Options struct {
	Home string
	URL  func() string
	Svc  func() *session.Service
	Hub  *events.Hub
	Quit func()
	Tray bool
}

// Shell is a placeholder.
type Shell struct{}

// New returns a placeholder shell.
func New(Options) *Shell { return &Shell{} }

// Run is unavailable in this build.
func (*Shell) Run(context.Context) error { return shell.ErrUnavailable }

// Open is unavailable in this build.
func (*Shell) Open() error { return shell.ErrUnavailable }

// PickFolder is unavailable in this build.
func (*Shell) PickFolder(context.Context, string) (string, error) { return "", shell.ErrUnavailable }

// Reveal is unavailable in this build.
func (*Shell) Reveal(string) error { return shell.ErrUnavailable }

// SetHub is a no-op in this build.
func (*Shell) SetHub(*events.Hub) {}
