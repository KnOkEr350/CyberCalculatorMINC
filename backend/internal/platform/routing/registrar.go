// Package routing defines the contract used to compose independent HTTP modules.
package routing

import "net/http"

// Registrar owns the routes of one application module.
type Registrar interface {
	RegisterRoutes(*http.ServeMux)
}

type conditionalRegistrar struct {
	enabled   bool
	registrar Registrar
}

// When wraps a registrar so a backend module can be enabled independently.
// A disabled module registers no routes and therefore fails closed with 404.
func When(enabled bool, registrar Registrar) Registrar {
	if registrar == nil {
		panic("routing: nil conditional registrar")
	}
	return conditionalRegistrar{enabled: enabled, registrar: registrar}
}

func (r conditionalRegistrar) RegisterRoutes(mux *http.ServeMux) {
	if r.enabled {
		r.registrar.RegisterRoutes(mux)
	}
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
