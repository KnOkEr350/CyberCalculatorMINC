// Package reporting owns dashboard, export and report-workflow routes.
package reporting

import (
	"database/sql"
	"net/http"

	"cybercalc/internal/handlers"
	"cybercalc/internal/middleware"
)

type Module struct {
	db        *sql.DB
	dashboard *handlers.DashboardHandlers
	reports   *handlers.ReportHandlers
	workflow  *handlers.ReportWorkflowHandlers
}

func New(db *sql.DB) *Module {
	return &Module{db: db, dashboard: &handlers.DashboardHandlers{DB: db}, reports: &handlers.ReportHandlers{DB: db}, workflow: &handlers.ReportWorkflowHandlers{DB: db}}
}

func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/dashboard", middleware.RequireAuth(m.db, m.dashboard.Get))
	mux.HandleFunc("POST /api/dashboard/target", middleware.RequireAuth(m.db, m.dashboard.SetBudgetTarget))
	mux.HandleFunc("GET /api/reports/export", middleware.RequireAuth(m.db, m.reports.Export))
	mux.HandleFunc("GET /api/report-workflow", middleware.RequireAuth(m.db, m.workflow.Get))
	mux.HandleFunc("POST /api/report-workflow/transition", middleware.RequireAuth(m.db, m.workflow.Transition))
}
