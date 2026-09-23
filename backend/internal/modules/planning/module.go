// Package planning owns plan/fact route registration.
package planning

import (
	"database/sql"
	"net/http"

	"cybercalc/internal/handlers"
	"cybercalc/internal/middleware"
)

type Module struct {
	db       *sql.DB
	entries  *handlers.EntryHandlers
	teaching *handlers.TeachingDirectoryHandlers
	options  Options
}

type Options struct {
	Categories bool
	Activities bool
	Teaching   bool
}

func New(db *sql.DB, options Options) *Module {
	return &Module{db: db, entries: &handlers.EntryHandlers{DB: db}, teaching: &handlers.TeachingDirectoryHandlers{DB: db}, options: options}
}

func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	if m.options.Categories {
		m.registerCategoryRoutes(mux)
	}
	if m.options.Activities {
		m.registerActivityRoutes(mux)
	}
	if m.options.Teaching {
		m.registerTeachingRoutes(mux)
	}
}

func (m *Module) registerCategoryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/categories", middleware.RequireAuth(m.db, m.entries.Categories))
}

func (m *Module) registerActivityRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/obligations", middleware.RequireAuth(m.db, m.entries.Obligations))
	mux.HandleFunc("GET /api/entries/import-template", middleware.RequireAuth(m.db, m.entries.ImportTemplate))
	mux.HandleFunc("POST /api/entries/import", middleware.RequireAuth(m.db, m.entries.Import))
	mux.HandleFunc("GET /api/entries", middleware.RequireAuth(m.db, m.entries.List))
	mux.HandleFunc("GET /api/entries/summary", middleware.RequireAuth(m.db, m.entries.Summary))
	mux.HandleFunc("POST /api/entries", middleware.RequireAuth(m.db, m.entries.Create))
	mux.HandleFunc("PUT /api/entries/{id}", middleware.RequireAuth(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		m.entries.Update(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("GET /api/entries/{id}/comments", middleware.RequireAuth(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		m.entries.Comments(w, r, u, r.PathValue("id"))
	}))
}

func (m *Module) registerTeachingRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/staff-members", middleware.RequireAuth(m.db, m.teaching.ListStaffMembers))
	mux.HandleFunc("POST /api/staff-members", middleware.RequireAuth(m.db, m.teaching.CreateStaffMember))
	mux.HandleFunc("PUT /api/staff-members/{id}", middleware.RequireAuth(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		m.teaching.UpdateStaffMember(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("GET /api/teaching-payouts", middleware.RequireAuth(m.db, m.teaching.ListTeachingPayouts))
	mux.HandleFunc("POST /api/teaching-payouts", middleware.RequireAuth(m.db, m.teaching.CreateTeachingPayout))
	mux.HandleFunc("PUT /api/teaching-payouts/{id}", middleware.RequireAuth(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		m.teaching.UpdateTeachingPayout(w, r, u, r.PathValue("id"))
	}))
}
