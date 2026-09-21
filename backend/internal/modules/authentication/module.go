// Package authentication owns authentication route registration.
package authentication

import (
	"database/sql"
	"net/http"
	"time"

	"cybercalc/internal/handlers"
	"cybercalc/internal/middleware"
)

type Options struct {
	SessionTTL   time.Duration
	SecureCookie bool
	MFAKey       string
	RequireMFA   bool
}

type Module struct {
	db      *sql.DB
	handler *handlers.AuthHandlers
}

func New(db *sql.DB, options Options) *Module {
	return &Module{db: db, handler: &handlers.AuthHandlers{
		DB: db, SessionTTL: options.SessionTTL, SecureCookie: options.SecureCookie,
		MFAKey: options.MFAKey, RequireMFA: options.RequireMFA,
	}}
}

func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/login", m.handler.Login)
	mux.HandleFunc("POST /api/auth/logout", m.handler.Logout)
	mux.HandleFunc("POST /api/auth/password", middleware.RequireAuth(m.db, m.handler.ChangePassword))
	mux.HandleFunc("POST /api/auth/mfa/enroll", middleware.RequireAuth(m.db, m.handler.MFAEnroll))
	mux.HandleFunc("POST /api/auth/mfa/confirm", middleware.RequireAuth(m.db, m.handler.MFAConfirm))
	mux.HandleFunc("GET /api/auth/me", middleware.RequireAuth(m.db, m.handler.Me))
	mux.HandleFunc("POST /api/auth/entity-type", middleware.RequireAuth(m.db, m.handler.SetEntityType))
}
