// Package routing defines the contract used to compose independent HTTP modules.
package routing

import "net/http"

// Registrar owns the routes of one application module.
type Registrar interface {
	RegisterRoutes(*http.ServeMux)
}

// RegisterAll mounts modules in the explicit order chosen by the composition
// root. Duplicate route patterns are rejected by net/http.ServeMux.
func RegisterAll(mux *http.ServeMux, registrars ...Registrar) {
	if mux == nil {
		panic("routing: nil ServeMux")
	}
	for _, registrar := range registrars {
		if registrar == nil {
			panic("routing: nil registrar")
		}
		registrar.RegisterRoutes(mux)
	}
}
