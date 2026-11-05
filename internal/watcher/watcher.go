// Package watcher polls session status and publishes changes.
package watcher

import (
	"context"
	"log/slog"
	"time"

	"github.com/mohamed-foly/berthkeep/internal/events"
	"github.com/mohamed-foly/berthkeep/internal/launcher"
	"github.com/mohamed-foly/berthkeep/internal/session"
)

// StatusEvent is published when a session's state changes.
const StatusEvent = "session.status"

// Watcher is an adaptive status poller.
type Watcher struct {
	Svc  func() *session.Service
	Hub  *events.Hub
	Fast time.Duration // UI connected or a session running
	Slow time.Duration // idle
	last map[string]launcher.State
}

// Run polls until ctx ends.
func (w *Watcher) Run(ctx context.Context) {
	if w.Fast == 0 {
		w.Fast = 2 * time.Second
	}
	if w.Slow == 0 {
		w.Slow = 10 * time.Second
	}
	w.last = map[string]launcher.State{}
	for {
		busy := w.Tick(ctx)
		d := w.Slow
		if busy || w.Hub.Count() > 0 {
			d = w.Fast
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(d):
		}
	}
}

// Tick checks once; it reports whether any session is running.
func (w *Watcher) Tick(ctx context.Context) bool {
	if w.last == nil {
		w.last = map[string]launcher.State{}
	}
	svc := w.Svc()
	views, err := svc.List(ctx)
	if err != nil {
		slog.Warn("watcher list", "err", err)
		return false
	}
	running, stopped := false, false
	seen := map[string]bool{}
	for _, v := range views {
		seen[v.ID] = true
		st := v.Status.State
		if st == launcher.Running || st == launcher.RunningUnverified {
			running = true
		}
		prev, known := w.last[v.ID]
		if !known || prev != st {
			w.Hub.Publish(events.Event{Type: StatusEvent, Data: v})
			if known && (prev == launcher.Running || prev == launcher.RunningUnverified) && st == launcher.Stopped {
				stopped = true
			}
		}
		w.last[v.ID] = st
	}
	for id := range w.last {
		if !seen[id] {
			delete(w.last, id)
			w.Hub.Publish(events.Event{Type: "session.removed", Data: map[string]string{"id": id}})
		}
	}
	if stopped {
		if err := svc.GC(ctx); err != nil {
			slog.Warn("watcher gc", "err", err)
		}
		w.Hub.Publish(events.Event{Type: "sessions.changed"})
	}
	return running
}
