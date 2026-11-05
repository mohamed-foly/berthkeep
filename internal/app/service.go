package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/berthkeep/berthkeep/internal/events"
	"github.com/berthkeep/berthkeep/internal/port"
	"github.com/berthkeep/berthkeep/internal/server"
	"github.com/berthkeep/berthkeep/internal/session"
	"github.com/berthkeep/berthkeep/internal/watcher"
)

// ServeOptions configure the background service.
type ServeOptions struct {
	Home    string
	Port    int // 0 = config value
	Version string
	Shell   port.Shell
	Ready   func(*server.Server) // called once listening is about to start
}

// Serve runs the API server and watcher until ctx ends.
func Serve(ctx context.Context, o ServeOptions) error {
	if in, alive := server.ReadInfo(o.Home); alive {
		return fmt.Errorf("%w on port %d (pid %d)", server.ErrRunning, in.Port, in.PID)
	}
	svc, err := NewSessionService(o.Home)
	if err != nil {
		return err
	}
	if err := svc.GC(ctx); err != nil {
		slog.Warn("startup gc", "err", err)
	}
	p := o.Port
	if p == 0 {
		p = svc.Config.Server.Port
	}
	hub := events.NewHub()
	srv := server.New(o.Home, p, svc, hub)
	srv.Version, srv.Shell = o.Version, o.Shell
	srv.Reload = func() (*session.Service, error) { return NewSessionService(o.Home) }
	w := &watcher.Watcher{Svc: srv.Service, Hub: hub, Fast: 2 * time.Second, Slow: 10 * time.Second}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go w.Run(ctx)
	if o.Ready != nil {
		o.Ready(srv)
	}
	return srv.ListenAndServe(ctx)
}
