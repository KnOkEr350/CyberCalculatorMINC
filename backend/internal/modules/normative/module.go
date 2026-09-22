// Package normative is the composition root for the official source registry.
package normative

import (
	"database/sql"
	"net/http"

	"cybercalc/internal/middleware"
	normativehttp "cybercalc/internal/modules/normative/http"
	"cybercalc/internal/modules/normative/repository"
	"cybercalc/internal/modules/normative/service"
)

type Module struct {
	db      *sql.DB
	handler *normativehttp.Handler
}

func New(db *sql.DB) *Module {
	return &Module{db: db, handler: normativehttp.New(service.New(repository.NewSQLRegistry(db)))}
}
func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/normative-sources", middleware.RequireAdmin(m.db, func(w http.ResponseWriter, r *http.Request, _ middleware.AuthUser) { m.handler.List(w, r) }))
	mux.HandleFunc("POST /api/admin/normative-sources/import", middleware.RequireAdmin(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) { m.handler.Import(w, r, u.ID) }))
	mux.HandleFunc("GET /api/admin/normative-sources/diff", middleware.RequireAdmin(m.db, func(w http.ResponseWriter, r *http.Request, _ middleware.AuthUser) { m.handler.Diff(w, r) }))
}
