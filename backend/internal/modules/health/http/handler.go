// Package healthhttp adapts health use cases to net/http.
package healthhttp

import (
	"context"
	"net/http"

	"cybercalc/internal/modules/health/domain"
	"cybercalc/internal/platform/httpx"
)

// Service is the narrow use-case contract required by this transport.
type Service interface {
	Liveness() domain.Status
	Readiness(context.Context) (domain.Status, error)
}

type Handler struct {
	service Service
}

func New(service Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/live", h.live)
	mux.HandleFunc("GET /api/ready", h.ready)
	// Compatibility endpoint for existing external monitoring.
	mux.HandleFunc("GET /api/health", h.ready)
}

func (h *Handler) live(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, h.service.Liveness())
}

func (h *Handler) ready(w http.ResponseWriter, r *http.Request) {
	status, err := h.service.Readiness(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, status)
}
