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
	"cybercalc/internal/modules/planning"
	"cybercalc/internal/modules/reporting"
	"cybercalc/internal/platform/routing"
)

// BuildRoutes is the composition root used by main and integration tests.
// Feature modules own their route declarations; this function only selects and
// assembles registrars, then applies process-wide middleware.
func BuildRoutes(db *sql.DB, cfg config.Config) http.Handler {
	mux := http.NewServeMux()
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
		directories.New(db),
		planning.New(db),
		documents.New(db, documents.Options{
			UploadDir:      cfg.UploadDir,
			ScannerAddress: cfg.ScannerAddress,
			QuotaBytes:     cfg.UploadQuotaBytes,
		}),
		reporting.New(db),
		administration.New(db),
	)
	return middleware.Security(mux, cfg.PublicURL, cfg.Environment == "production")
}
