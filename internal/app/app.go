// Package app wires adapters into services.
package app

import (
	"github.com/mohamed-foly/berthkeep/internal/adapters/browsers"
	"github.com/mohamed-foly/berthkeep/internal/adapters/lock"
	"github.com/mohamed-foly/berthkeep/internal/adapters/procs"
	"github.com/mohamed-foly/berthkeep/internal/config"
	"github.com/mohamed-foly/berthkeep/internal/launcher"
	"github.com/mohamed-foly/berthkeep/internal/port"
	"github.com/mohamed-foly/berthkeep/internal/registry"
	"github.com/mohamed-foly/berthkeep/internal/session"
)

// NewSessionService wires the session Facade for home.
func NewSessionService(home string) (*session.Service, error) {
	cfg, err := config.Load(home)
	if err != nil {
		return nil, err
	}
	custom := make([]port.Browser, 0, len(cfg.Browsers.Custom))
	for _, b := range cfg.Browsers.Custom {
		custom = append(custom, port.Browser{ID: b.ID, Name: b.Name, Path: b.Path})
	}
	return &session.Service{
		Home:     home,
		Config:   cfg,
		Store:    registry.New(home, nil),
		Launcher: &launcher.Launcher{Procs: procs.New(), Locks: lock.New()},
		Browsers: browsers.New(custom),
	}, nil
}
