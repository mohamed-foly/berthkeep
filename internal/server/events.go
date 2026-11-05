package server

import (
	"encoding/json"

	"github.com/berthkeep/berthkeep/internal/events"
	"github.com/berthkeep/berthkeep/internal/session"
	"github.com/berthkeep/berthkeep/internal/watcher"
)

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

func eventFor(v session.View) events.Event { return events.Event{Type: watcher.StatusEvent, Data: v} }

func eventRemoved(ref string) events.Event {
	return events.Event{Type: "session.removed", Data: map[string]string{"ref": ref}}
}
