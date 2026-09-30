// Package api serves the REST endpoints and the WebSocket stream (docs/api.md §5-§6).
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/AlphaTechini/heimdall/server/internal/chain"
	"github.com/AlphaTechini/heimdall/server/internal/config"
	"github.com/AlphaTechini/heimdall/server/internal/executor"
	"github.com/AlphaTechini/heimdall/server/internal/hub"
	"github.com/AlphaTechini/heimdall/server/internal/notify"
	"github.com/AlphaTechini/heimdall/server/internal/policy"
	"github.com/AlphaTechini/heimdall/server/internal/sim"
	"github.com/AlphaTechini/heimdall/server/internal/store"
	"github.com/AlphaTechini/heimdall/server/internal/watcher"
	"github.com/ethereum/go-ethereum/common"
)

// Deps are the services the API needs.
type Deps struct {
	Env      *config.Env
	Targets  *config.Targets
	Signals  *config.Signals
	Chain    *chain.Client
	Store    *store.Store
	Watcher  *watcher.Watcher
	Executor *executor.Executor
	Policy   *policy.Service
	Hub      *hub.Hub
	Notifier *notify.Dispatcher
	Telegram *notify.Telegram
	Email    *notify.Email
	Sim      *sim.Sim
	Factory  common.Address
}

// API is the HTTP server state.
type API struct {
	Deps
	authKey       []byte
	emailCooldown map[string]time.Time
	cooldownMu    sync.Mutex
}

// New creates the API. authKey signs bearer tokens.
func New(d Deps, authKey []byte) *API {
	return &API{Deps: d, authKey: authKey, emailCooldown: map[string]time.Time{}}
}

// Handler returns the routed, CORS-wrapped handler.
func (a *API) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", a.healthz)
	m.HandleFunc("GET /config", a.getConfig)
	m.HandleFunc("GET /positions", a.positions)
	m.HandleFunc("GET /signals/{targetId}", a.signalsLatest)
	m.HandleFunc("GET /signals/{targetId}/history", a.signalsHistory)
	m.HandleFunc("GET /guards/{guard}", a.guard)
	m.HandleFunc("GET /activity", a.activity)
	m.HandleFunc("GET /exits", a.exits)
	m.HandleFunc("GET /exits/{id}", a.exit)
	m.HandleFunc("GET /tx/{hash}", a.tx)
	m.HandleFunc("GET /backtests", a.backtests)
	m.HandleFunc("GET /backtests/{id}", a.backtest)
	m.HandleFunc("GET /stream", a.stream)

	m.HandleFunc("GET /auth/nonce", a.authNonce)
	m.HandleFunc("POST /auth/verify", a.authVerify)
	m.HandleFunc("PUT /policies/{guard}/{targetId}", a.authed(a.putPolicy))
	m.HandleFunc("GET /me/settings", a.authed(a.meSettings))
	m.HandleFunc("PUT /me/default-policy", a.authed(a.meDefaultPolicy))
	m.HandleFunc("POST /me/telegram/link", a.authed(a.telegramLink))
	m.HandleFunc("DELETE /me/telegram", a.authed(a.telegramUnlink))
	m.HandleFunc("POST /me/telegram/test", a.authed(a.telegramTest))
	m.HandleFunc("POST /me/email", a.authed(a.emailSet))
	m.HandleFunc("POST /me/email/verify", a.authed(a.emailVerify))
	m.HandleFunc("POST /me/email/test", a.authed(a.emailTest))
	m.HandleFunc("POST /exits/{guard}/{targetId}/stop", a.authed(a.stopExit))

	m.HandleFunc("GET /sim/scenarios", a.simGuard(a.simScenarios))
	m.HandleFunc("POST /sim/scenarios/{id}/run", a.simGuard(a.simRun))
	m.HandleFunc("POST /sim/reset", a.simGuard(a.simReset))
	m.HandleFunc("GET /sim/compare", a.simGuard(a.simCompare))
	return a.middleware(m)
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (s *statusWriter) WriteHeader(c int) { s.code = c; s.ResponseWriter.WriteHeader(c) }

// Unwrap lets http.ResponseController and the WebSocket library reach the real writer.
func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func (a *API) allowedOrigin(origin string) bool {
	for _, o := range a.Env.CORSOrigins {
		if o == "*" || strings.EqualFold(o, origin) {
			return true
		}
	}
	return false
}

func (a *API) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" && a.allowedOrigin(o) {
			w.Header().Set("Access-Control-Allow-Origin", o)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		sw := &statusWriter{ResponseWriter: w, code: 200}
		start := time.Now()
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("handler panic", "path", r.URL.Path, "panic", rec, "stack", string(debug.Stack()))
				writeErr(sw, http.StatusInternalServerError, "Something went wrong on our side. Please try again.")
			}
			slog.Debug("request", "method", r.Method, "path", r.URL.Path, "status", sw.code, "ms", time.Since(start).Milliseconds())
		}()
		next.ServeHTTP(sw, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Debug("cannot write response", "err", err)
	}
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// decodeBody reads a small JSON body into v.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "The request body is not valid JSON.")
		return false
	}
	return true
}

func parseAddr(s string) (common.Address, bool) {
	if !common.IsHexAddress(s) {
		return common.Address{}, false
	}
	return common.HexToAddress(s), true
}

func (a *API) rpcErr(w http.ResponseWriter, err error) {
	slog.Warn("chain read failed", "err", err)
	writeErr(w, http.StatusBadGateway, "Could not read the blockchain right now. Please try again in a moment.")
}

func (a *API) dbErr(w http.ResponseWriter, err error) {
	slog.Error("database error", "err", err)
	writeErr(w, http.StatusInternalServerError, "Something went wrong saving or loading your data. Please try again.")
}

func (a *API) target(w http.ResponseWriter, id string) *config.Target {
	t := a.Targets.Target(id)
	if t == nil {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("Unknown position %q.", id))
	}
	return t
}

func originHost(r *http.Request) (host, origin string) {
	if o := r.Header.Get("Origin"); o != "" {
		if u, err := url.Parse(o); err == nil && u.Host != "" {
			return u.Host, o
		}
	}
	return r.Host, "http://" + r.Host
}

func (a *API) ctx(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 20*time.Second)
}

var errNotFound = errors.New("not found")
