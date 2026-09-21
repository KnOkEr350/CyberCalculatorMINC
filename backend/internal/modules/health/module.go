// Package health is the composition root of the health module. The server may
// import this package; nested module packages must never import it.
package health

import (
	"database/sql"
	"net/http"

	healthhttp "cybercalc/internal/modules/health/http"
	"cybercalc/internal/modules/health/repository"
	"cybercalc/internal/modules/health/service"
)

// RegisterRoutes wires repository, service and HTTP layers for this module.
func RegisterRoutes(mux *http.ServeMux, db *sql.DB) {
	probe := repository.NewSQLProbe(db)
	healthService := service.New(probe)
	healthhttp.New(healthService).RegisterRoutes(mux)
}
