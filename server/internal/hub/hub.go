// Package hub fans messages out to WebSocket clients and records events (store + push +
// notifications) so the watcher, executor and simulator share one path.
package hub

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/AlphaTechini/heimdall/server/internal/notify"
	"github.com/AlphaTechini/heimdall/server/internal/store"
)

// Hub is the WebSocket broadcast bus (docs/api.md §6).
type Hub struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

// New creates a hub.
func New() *Hub { return &Hub{subs: map[chan []byte]struct{}{}} }

// Subscribe returns a channel of JSON messages and a cancel function. A client that cannot keep
// up loses messages instead of slowing anyone down.
func (h *Hub) Subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 256)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
}

// Publish sends {"type": typ, "data": data} to every subscriber without blocking.
func (h *Hub) Publish(typ string, data any) {
	b, err := json.Marshal(map[string]any{"type": typ, "data": data})
	if err != nil {
		slog.Error("cannot encode websocket message", "type", typ, "err", err)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- b:
		default:
		}
	}
}

// Emitter stores an event, pushes it to the UI and, for exit events, alerts the owner.
type Emitter struct {
	Store    *store.Store
	Hub      *Hub
	Notifier *notify.Dispatcher
}

// Emit records the event. Failures are logged; they never stop the caller (U6).
func (em *Emitter) Emit(ctx context.Context, e store.Event) store.Event {
	saved, err := em.Store.InsertEvent(ctx, e)
	if err != nil {
		slog.Error("cannot store event", "kind", e.Kind, "err", err)
		saved = e
	}
	em.Hub.Publish("event", saved)
	switch e.Kind {
	case "exit_submitted", "exit_partial", "exit_complete":
		if e.Owner != "" && em.Notifier != nil {
			em.Notifier.Enqueue(notify.Message{Owner: e.Owner, Kind: e.Kind, Subject: subject(e.Kind), Body: e.Message})
		}
	}
	return saved
}

func subject(kind string) string {
	switch kind {
	case "exit_submitted":
		return "Heimdall: exit submitted"
	case "exit_partial":
		return "Heimdall: partial exit"
	case "exit_complete":
		return "Heimdall: exit complete"
	}
	return "Heimdall alert"
}
