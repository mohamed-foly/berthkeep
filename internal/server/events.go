package server

import (
	"encoding/json"

	"github.com/mohamed-foly/berthkeep/internal/events"
	"github.com/mohamed-foly/berthkeep/internal/session"
	"github.com/mohamed-foly/berthkeep/internal/watcher"
)

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

func eventFor(v session.View) events.Event { return events.Event{Type: watcher.StatusEvent, Data: v} }

func eventRemoved(ref string) events.Event {
	return events.Event{Type: "session.removed", Data: map[string]string{"ref": ref}}
}
