// Package administration owns privileged management route registration.
package administration

import (
	"database/sql"
	"net/http"

	"cybercalc/internal/handlers"
	"cybercalc/internal/middleware"
)

type Module struct {
	db      *sql.DB
	handler *handlers.AdminHandlers
}

func New(db *sql.DB) *Module { return &Module{db: db, handler: &handlers.AdminHandlers{DB: db}} }

func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/users", middleware.RequireAdmin(m.db, m.handler.ListUsers))
	mux.HandleFunc("GET /api/admin/it-company-options", middleware.RequireAdmin(m.db, m.handler.ITCompanyOptions))
	mux.HandleFunc("POST /api/admin/users", middleware.RequireAdmin(m.db, m.handler.CreateUser))
	mux.HandleFunc("PATCH /api/admin/users/{id}", middleware.RequireAdmin(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		m.handler.UpdateUser(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("GET /api/admin/settings", middleware.RequireAdmin(m.db, m.handler.GetSettings))
	mux.HandleFunc("POST /api/admin/settings", middleware.RequireAdmin(m.db, m.handler.UpdateSetting))
	mux.HandleFunc("GET /api/admin/logs", middleware.RequireAdmin(m.db, m.handler.AuditLog))
	mux.HandleFunc("GET /api/admin/logs/export", middleware.RequireAdmin(m.db, m.handler.ExportAuditLog))
}
