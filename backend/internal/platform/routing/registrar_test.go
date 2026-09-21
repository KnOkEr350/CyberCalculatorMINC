package routing

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type registrarStub struct{ pattern string }

func (r registrarStub) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc(r.pattern, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
}

func TestRegisterAllMountsEveryModule(t *testing.T) {
	mux := http.NewServeMux()
	RegisterAll(mux, registrarStub{"GET /one"}, registrarStub{"GET /two"})
	for _, path := range []string{"/one", "/two"} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusNoContent {
			t.Fatalf("%s returned %d, want %d", path, response.Code, http.StatusNoContent)
		}
	}
}

func TestRegisterAllRejectsInvalidComposition(t *testing.T) {
	tests := []struct {
		name       string
		mux        *http.ServeMux
		registrars []Registrar
	}{
		{"nil mux", nil, nil},
		{"nil registrar", http.NewServeMux(), []Registrar{nil}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected panic")
				}
			}()
			RegisterAll(test.mux, test.registrars...)
		})
	}
}
