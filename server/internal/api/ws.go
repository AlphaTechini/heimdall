package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/coder/websocket"
)

func (a *API) originPatterns() []string {
	var out []string
	for _, o := range a.Env.CORSOrigins {
		if u, err := url.Parse(o); err == nil && u.Host != "" {
			out = append(out, u.Host)
		}
	}
	return out
}

// stream serves GET /stream (docs/api.md §6). It also sends the current signals on connect so
// a new page does not have to wait for the next check.
func (a *API) stream(w http.ResponseWriter, r *http.Request) {
	opts := &websocket.AcceptOptions{OriginPatterns: a.originPatterns()}
	for _, o := range a.Env.CORSOrigins {
		if o == "*" {
			opts.InsecureSkipVerify = true
		}
	}
	c, err := websocket.Accept(w, r, opts)
	if err != nil {
		slog.Debug("websocket rejected", "err", err)
		return
	}
	defer c.CloseNow()
	ctx := c.CloseRead(r.Context())
	msgs, cancel := a.Hub.Subscribe()
	defer cancel()
	write := func(b []byte) bool {
		wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
		defer wcancel()
		return c.Write(wctx, websocket.MessageText, b) == nil
	}
	for i := range a.Targets.Targets {
		if l, ok := a.Watcher.Latest(a.Targets.Targets[i].ID); ok {
			b, _ := json.Marshal(map[string]any{"type": "signals", "data": l})
			if !write(b) {
				return
			}
		}
	}
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case b, ok := <-msgs:
			if !ok || !write(b) {
				return
			}
		case <-ping.C:
			pctx, pcancel := context.WithTimeout(ctx, 5*time.Second)
			err := c.Ping(pctx)
			pcancel()
			if err != nil {
				return
			}
		}
	}
}
