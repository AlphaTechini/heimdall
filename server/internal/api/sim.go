package api

import (
	"errors"
	"net/http"

	"github.com/AlphaTechini/heimdall/server/internal/sim"
)

const simRefused = "The incident simulator only runs on a local Arbitrum One fork."

func (a *API) simGuard(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.Sim == nil || !a.Sim.Enabled() {
			writeErr(w, http.StatusForbidden, simRefused)
			return
		}
		h(w, r)
	}
}

func (a *API) simScenarios(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.Sim.Scenarios())
}

func (a *API) simRun(w http.ResponseWriter, r *http.Request) {
	id, err := a.Sim.Run(r.PathValue("id"))
	switch {
	case errors.Is(err, sim.ErrBusy):
		writeErr(w, 409, "A scenario is already running. Wait for it to finish or reset first.")
	case errors.Is(err, sim.ErrUnknown):
		writeErr(w, 404, "Unknown scenario.")
	case err != nil:
		writeErr(w, 400, err.Error())
	default:
		writeJSON(w, 202, map[string]int64{"runId": id})
	}
}

func (a *API) simReset(w http.ResponseWriter, r *http.Request) {
	if err := a.Sim.Reset(r.Context()); err != nil {
		if errors.Is(err, sim.ErrBusy) {
			writeErr(w, 409, "A scenario is running. Wait for it to finish before resetting.")
			return
		}
		writeErr(w, 500, "Could not reset the simulation: "+err.Error())
		return
	}
	writeJSON(w, 202, map[string]bool{"reset": true})
}

func (a *API) simCompare(w http.ResponseWriter, r *http.Request) {
	c, err := a.Sim.Compare(r.Context())
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, c)
}
