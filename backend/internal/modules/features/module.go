// Package features exposes the safe frontend feature flag snapshot.
package features

import (
	"net/http"

	"cybercalc/internal/platform/featureflags"
	"cybercalc/internal/platform/httpx"
)

const contractVersion = 1

type Module struct {
	frontend featureflags.Set
}

func New(frontend featureflags.Set) *Module {
	return &Module{frontend: frontend}
}

func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/features", m.get)
}

func (m *Module) get(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"version": contractVersion,
		"flags":   m.frontend.Snapshot(),
	})
}
