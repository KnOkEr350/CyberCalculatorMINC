// Package server собирает HTTP API приложения.
package server

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"cybercalc/internal/config"
	"cybercalc/internal/handlers"
	"cybercalc/internal/middleware"
)

// BuildRoutes возвращает тот же обработчик API, который используется в main.
// Функция экспортирована, чтобы интеграционные тесты проверяли реальные маршруты.
func BuildRoutes(db *sql.DB, cfg config.Config) http.Handler {
	mux := http.NewServeMux()

	authH := &handlers.AuthHandlers{DB: db, SessionTTL: time.Duration(cfg.SessionTTLh) * time.Hour, SecureCookie: cfg.CookieSecure, MFAKey: cfg.MFAKey, RequireMFA: cfg.Environment == "production"}
	mux.HandleFunc("GET /api/live", func(w http.ResponseWriter, _ *http.Request) {
		middleware.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	ready := func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if db == nil || db.PingContext(ctx) != nil {
			middleware.WriteError(w, http.StatusServiceUnavailable, "база данных недоступна")
			return
		}
		middleware.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
	mux.HandleFunc("GET /api/ready", ready)
	// Compatibility endpoint for existing external monitoring.
	mux.HandleFunc("GET /api/health", ready)
	partnerH := &handlers.PartnerHandlers{DB: db}
	itCompanyH := &handlers.ITCompanyHandlers{DB: db}
	agreementH := &handlers.AgreementHandlers{DB: db}
	groupH := &handlers.LegalEntityGroupHandlers{DB: db}
	regionalAuthorityH := &handlers.RegionalAuthorityHandlers{DB: db}
	entryH := &handlers.EntryHandlers{DB: db}
	attachH := &handlers.AttachmentHandlers{DB: db, UploadDir: cfg.UploadDir, ScannerAddress: cfg.ScannerAddress, QuotaBytes: cfg.UploadQuotaBytes}
	dashH := &handlers.DashboardHandlers{DB: db}
	adminH := &handlers.AdminHandlers{DB: db}
	reportH := &handlers.ReportHandlers{DB: db}
	workflowH := &handlers.ReportWorkflowHandlers{DB: db}

	// Аутентификация.
	mux.HandleFunc("POST /api/auth/login", authH.Login)
	mux.HandleFunc("POST /api/auth/logout", authH.Logout)
	mux.HandleFunc("POST /api/auth/password", middleware.RequireAuth(db, authH.ChangePassword))
	mux.HandleFunc("POST /api/auth/mfa/enroll", middleware.RequireAuth(db, authH.MFAEnroll))
	mux.HandleFunc("POST /api/auth/mfa/confirm", middleware.RequireAuth(db, authH.MFAConfirm))
	mux.HandleFunc("GET /api/auth/me", middleware.RequireAuth(db, authH.Me))
	mux.HandleFunc("POST /api/auth/entity-type", middleware.RequireAuth(db, authH.SetEntityType))

	// Партнёры и справочники.
	mux.HandleFunc("GET /api/partners", middleware.RequireAuth(db, partnerH.List))
	mux.HandleFunc("POST /api/partners", middleware.RequireAuth(db, partnerH.Create))
	mux.HandleFunc("GET /api/it-companies", middleware.RequireAuth(db, itCompanyH.List))
	mux.HandleFunc("GET /api/it-companies/registry-search", middleware.RequireAuth(db, itCompanyH.RegistrySearch))
	mux.HandleFunc("POST /api/it-companies", middleware.RequireAuth(db, itCompanyH.Create))
	mux.HandleFunc("GET /api/it-companies/template", middleware.RequireAuth(db, itCompanyH.Template))
	mux.HandleFunc("POST /api/it-companies/import", middleware.RequireAuth(db, itCompanyH.Import))
	mux.HandleFunc("GET /api/directory", middleware.RequireAuth(db, partnerH.Directory))
	mux.HandleFunc("GET /api/directory/stats", middleware.RequireAuth(db, partnerH.DirectoryStats))
	mux.HandleFunc("POST /api/directory", middleware.RequireAuth(db, partnerH.CreateDirectory))
	mux.HandleFunc("GET /api/directory/proposals", middleware.RequireAuth(db, partnerH.DirectoryProposals))
	mux.HandleFunc("POST /api/directory/{id}/decision", middleware.RequireAuth(db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		partnerH.DecideDirectoryProposal(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("PUT /api/directory/{id}", middleware.RequireAuth(db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		partnerH.UpdateDirectory(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("GET /api/agreements", middleware.RequireAuth(db, agreementH.List))
	mux.HandleFunc("POST /api/agreements", middleware.RequireAuth(db, agreementH.Create))
	mux.HandleFunc("PUT /api/agreements/{id}", middleware.RequireAuth(db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		agreementH.Update(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("GET /api/legal-entity-groups", middleware.RequireAuth(db, groupH.List))
	mux.HandleFunc("POST /api/legal-entity-groups", middleware.RequireAuth(db, groupH.Create))
	mux.HandleFunc("PUT /api/legal-entity-groups/{id}", middleware.RequireAuth(db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		groupH.Update(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("GET /api/regional-authorities", middleware.RequireAuth(db, regionalAuthorityH.List))
	mux.HandleFunc("POST /api/regional-authorities", middleware.RequireAuth(db, regionalAuthorityH.Create))
	mux.HandleFunc("PUT /api/regional-authorities/{id}", middleware.RequireAuth(db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		regionalAuthorityH.Update(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("GET /api/mentors", middleware.RequireAuth(db, entryH.Mentors))
	mux.HandleFunc("POST /api/mentors", middleware.RequireAuth(db, entryH.CreateMentor))
	mux.HandleFunc("GET /api/obligations", middleware.RequireAuth(db, entryH.Obligations))
	mux.HandleFunc("GET /api/entries/import-template", middleware.RequireAuth(db, entryH.ImportTemplate))
	mux.HandleFunc("POST /api/entries/import", middleware.RequireAuth(db, entryH.Import))
	mux.HandleFunc("GET /api/admin/directory-template", middleware.RequireAuth(db, partnerH.DirectoryTemplate))
	mux.HandleFunc("POST /api/admin/directory-import", middleware.RequireAuth(db, partnerH.ImportDirectory))
	mux.HandleFunc("GET /api/categories", middleware.RequireAuth(db, entryH.Categories))

	// План и факт.
	mux.HandleFunc("GET /api/entries", middleware.RequireAuth(db, entryH.List))
	mux.HandleFunc("GET /api/entries/summary", middleware.RequireAuth(db, entryH.Summary))
	mux.HandleFunc("POST /api/entries", middleware.RequireAuth(db, entryH.Create))
	mux.HandleFunc("PUT /api/entries/{id}", middleware.RequireAuth(db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		entryH.Update(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("GET /api/entries/{id}/comments", middleware.RequireAuth(db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		entryH.Comments(w, r, u, r.PathValue("id"))
	}))

	// Вложения.
	mux.HandleFunc("POST /api/entries/{id}/attachments", middleware.RequireAuth(db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		attachH.Upload(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("GET /api/entries/{id}/attachments", middleware.RequireAuth(db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		attachH.ListForEntry(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("GET /api/attachments/{id}/download", middleware.RequireAuth(db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		attachH.Download(w, r, u, r.PathValue("id"))
	}))

	// Дашборд, отчёты и их жизненный цикл.
	mux.HandleFunc("GET /api/dashboard", middleware.RequireAuth(db, dashH.Get))
	mux.HandleFunc("POST /api/dashboard/target", middleware.RequireAuth(db, dashH.SetBudgetTarget))
	mux.HandleFunc("GET /api/reports/export", middleware.RequireAuth(db, reportH.Export))
	mux.HandleFunc("GET /api/report-workflow", middleware.RequireAuth(db, workflowH.Get))
	mux.HandleFunc("POST /api/report-workflow/transition", middleware.RequireAuth(db, workflowH.Transition))

	// Административная панель.
	mux.HandleFunc("GET /api/admin/users", middleware.RequireAdmin(db, adminH.ListUsers))
	mux.HandleFunc("GET /api/admin/it-company-options", middleware.RequireAdmin(db, adminH.ITCompanyOptions))
	mux.HandleFunc("POST /api/admin/users", middleware.RequireAdmin(db, adminH.CreateUser))
	mux.HandleFunc("PATCH /api/admin/users/{id}", middleware.RequireAdmin(db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		adminH.UpdateUser(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("GET /api/admin/settings", middleware.RequireAdmin(db, adminH.GetSettings))
	mux.HandleFunc("POST /api/admin/settings", middleware.RequireAdmin(db, adminH.UpdateSetting))
	mux.HandleFunc("GET /api/admin/logs", middleware.RequireAdmin(db, adminH.AuditLog))

	return middleware.Security(mux, cfg.PublicURL, cfg.Environment == "production")
}
