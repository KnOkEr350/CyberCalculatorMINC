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

type Module struct {
	handler *healthhttp.Handler
}

// New wires repository, service and HTTP layers for this module.
func New(db *sql.DB) *Module {
	probe := repository.NewSQLProbe(db)
	healthService := service.New(probe)
	return &Module{handler: healthhttp.New(healthService)}
}

func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	m.handler.RegisterRoutes(mux)
}
