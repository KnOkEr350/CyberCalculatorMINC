// Package okz is the composition root for the versioned OKZ catalog.
package okz

import (
	"database/sql"
	"net/http"

	"cybercalc/internal/middleware"
	okzhttp "cybercalc/internal/modules/okz/http"
	"cybercalc/internal/modules/okz/repository"
	"cybercalc/internal/modules/okz/service"
)

type Module struct {
	db      *sql.DB
	handler *okzhttp.Handler
}

func New(db *sql.DB) *Module {
	catalog := repository.NewSQLCatalog(db)
	return &Module{db: db, handler: okzhttp.New(service.New(catalog))}
}

func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/okz", middleware.RequireAuth(m.db, func(w http.ResponseWriter, r *http.Request, _ middleware.AuthUser) {
		m.handler.Search(w, r)
	}))
	mux.HandleFunc("GET /api/admin/okz/versions", middleware.RequireAdmin(m.db, func(w http.ResponseWriter, r *http.Request, _ middleware.AuthUser) {
		m.handler.Versions(w, r)
	}))
	mux.HandleFunc("GET /api/admin/okz/template", middleware.RequireAdmin(m.db, func(w http.ResponseWriter, r *http.Request, _ middleware.AuthUser) {
		m.handler.Template(w, r)
	}))
	mux.HandleFunc("POST /api/admin/okz/import", middleware.RequireAdmin(m.db, func(w http.ResponseWriter, r *http.Request, user middleware.AuthUser) {
		m.handler.Import(w, r, user.ID)
	}))
}
