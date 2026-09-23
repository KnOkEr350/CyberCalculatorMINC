// Package documents owns attachment route registration.
package documents

import (
	"database/sql"
	"net/http"

	"cybercalc/internal/handlers"
	"cybercalc/internal/middleware"
)

type Options struct {
	UploadDir      string
	ScannerAddress string
	// STORE-05: поведение при недоступном антивирусе — reject или quarantine.
	ScannerPolicy string
	QuotaBytes    int64
}

type Module struct {
	db          *sql.DB
	attachments *handlers.AttachmentHandlers
}

func New(db *sql.DB, options Options) *Module {
	return &Module{db: db, attachments: &handlers.AttachmentHandlers{
		DB: db, UploadDir: options.UploadDir, ScannerAddress: options.ScannerAddress, ScannerPolicy: options.ScannerPolicy, QuotaBytes: options.QuotaBytes,
	}}
}

func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/entries/{id}/attachments", middleware.RequireAuth(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		m.attachments.Upload(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("GET /api/entries/{id}/attachments", middleware.RequireAuth(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		m.attachments.ListForEntry(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("GET /api/attachments/{id}/download", middleware.RequireAuth(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		m.attachments.Download(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("PATCH /api/attachments/{id}/review", middleware.RequireAuth(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		m.attachments.Review(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("PATCH /api/attachments/{id}/metadata", middleware.RequireAuth(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		m.attachments.UpdateMetadata(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("GET /api/entries/{id}/legal-disputes", middleware.RequireAuth(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		m.attachments.LegalDisputes(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("POST /api/entries/{id}/legal-disputes", middleware.RequireAuth(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		m.attachments.ChangeLegalDispute(w, r, u, r.PathValue("id"))
	}))
}
