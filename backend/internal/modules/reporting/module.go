// Package reporting owns dashboard, export and report-workflow routes.
package reporting

import (
	"database/sql"
	"net/http"

	"cybercalc/internal/handlers"
	"cybercalc/internal/middleware"
	"cybercalc/internal/modules/reporting/repository"
)

type Module struct {
	db        *sql.DB
	dashboard *handlers.DashboardHandlers
	reports   *handlers.ReportHandlers
	workflow  *handlers.ReportWorkflowHandlers
	snapshots *handlers.SnapshotHandlers
	calendar  handlers.ReportCalendarHandlers
	options   Options
}

type Options struct {
	Dashboard bool
	Reports   bool
	Snapshots bool
}

func New(db *sql.DB, options Options) *Module {
	projection := repository.NewActivityProjection(db)
	return &Module{db: db, dashboard: &handlers.DashboardHandlers{DB: db, Projection: projection}, reports: &handlers.ReportHandlers{DB: db}, workflow: &handlers.ReportWorkflowHandlers{DB: db}, snapshots: &handlers.SnapshotHandlers{DB: db}, calendar: handlers.ReportCalendarHandlers{}, options: options}
}

func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	if m.options.Dashboard {
		m.registerDashboardRoutes(mux)
	}
	if m.options.Reports {
		m.registerReportRoutes(mux)
	}
	if m.options.Snapshots {
		m.registerSnapshotRoutes(mux)
	}
}

func (m *Module) registerDashboardRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/dashboard", middleware.RequireAuth(m.db, m.dashboard.Get))
	mux.HandleFunc("POST /api/dashboard/target", middleware.RequireAuth(m.db, m.dashboard.SetBudgetTarget))
}

func (m *Module) registerReportRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/reports/export", middleware.RequireAuth(m.db, m.reports.Export))
	mux.HandleFunc("GET /api/reports/generated", middleware.RequireAuth(m.db, m.reports.ListGenerated))
	mux.HandleFunc("GET /api/reports/generated/{id}", middleware.RequireAuth(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		m.reports.DownloadGenerated(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("GET /api/report-calendar", middleware.RequireAuth(m.db, m.calendar.Get))
	mux.HandleFunc("GET /api/report-workflow", middleware.RequireAuth(m.db, m.workflow.Get))
	mux.HandleFunc("POST /api/report-workflow/transition", middleware.RequireAuth(m.db, m.workflow.Transition))
}

func (m *Module) registerSnapshotRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/report-snapshots", middleware.RequireAuth(m.db, m.snapshots.List))
	mux.HandleFunc("POST /api/report-snapshots", middleware.RequireAuth(m.db, m.snapshots.Create))
	mux.HandleFunc("GET /api/report-snapshots/{id}", middleware.RequireAuth(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		m.snapshots.Download(w, r, u, r.PathValue("id"))
	}))
}
