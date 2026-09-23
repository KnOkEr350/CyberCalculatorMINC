// Package server composes the application's independent HTTP modules.
package server

import (
	"database/sql"
	"net/http"
	"time"

	"cybercalc/internal/config"
	"cybercalc/internal/middleware"
	"cybercalc/internal/modules/administration"
	"cybercalc/internal/modules/authentication"
	"cybercalc/internal/modules/directories"
	"cybercalc/internal/modules/documents"
	"cybercalc/internal/modules/features"
	"cybercalc/internal/modules/health"
	"cybercalc/internal/modules/normative"
	"cybercalc/internal/modules/okz"
	"cybercalc/internal/modules/planning"
	"cybercalc/internal/modules/reporting"
	"cybercalc/internal/platform/featureflags"
	"cybercalc/internal/platform/routing"
)

// BuildRoutes is the composition root used by main and integration tests.
// Feature modules own their route declarations; this function only selects and
// assembles registrars, then applies process-wide middleware.
func BuildRoutes(db *sql.DB, cfg config.Config) http.Handler {
	mux := http.NewServeMux()
	anyFeatureEnabled := cfg.BackendFeatureFlags.Any(featureflags.Names()...)
	activityEnabled := cfg.BackendFeatureFlags.Any(
		featureflags.Teachers,
		featureflags.OOPRPD,
		featureflags.Internships,
		featureflags.Practice,
		featureflags.TopITAI,
		featureflags.Schools,
		featureflags.MinistryDecision,
	)
	routing.RegisterAll(
		mux,
		health.New(db),
		features.New(cfg.FrontendFeatureFlags),
		authentication.New(db, authentication.Options{
			SessionTTL:   time.Duration(cfg.SessionTTLh) * time.Hour,
			SecureCookie: cfg.CookieSecure,
			MFAKey:       cfg.MFAKey,
			RequireMFA:   cfg.Environment == "production",
		}),
		routing.When(anyFeatureEnabled, directories.New(db)),
		routing.When(cfg.BackendFeatureFlags.Any(featureflags.Teachers, featureflags.SettingsV44), okz.New(db)),
		routing.When(cfg.BackendFeatureFlags.Any(featureflags.ReportingV44, featureflags.SettingsV44), normative.New(db)),
		planning.New(db, planning.Options{
			Categories: anyFeatureEnabled,
			Activities: activityEnabled,
			Teaching:   cfg.BackendFeatureFlags.Enabled(featureflags.Teachers),
		}),
		routing.When(activityEnabled, documents.New(db, documents.Options{
			UploadDir:      cfg.UploadDir,
			ScannerAddress: cfg.ScannerAddress,
			QuotaBytes:     cfg.UploadQuotaBytes,
		})),
		reporting.New(db, reporting.Options{
			Dashboard: cfg.BackendFeatureFlags.Enabled(featureflags.DashboardV44),
			Reports:   cfg.BackendFeatureFlags.Enabled(featureflags.ReportingV44),
			Snapshots: cfg.BackendFeatureFlags.Any(featureflags.ReportingV44, featureflags.SettingsV44),
		}),
		routing.When(cfg.BackendFeatureFlags.Enabled(featureflags.SettingsV44), administration.New(db)),
	)
	return middleware.Security(mux, cfg.PublicURL, cfg.Environment == "production")
}
