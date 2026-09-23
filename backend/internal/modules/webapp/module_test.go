package webapp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func request(t *testing.T, target string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	New().RegisterRoutes(mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder
}

func TestEmbeddedFrontendServesShellAndClientRoutes(t *testing.T) {
	for _, target := range []string{"/", "/partners", "/reports/2026"} {
		response := request(t, target)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "<!doctype html>") {
			t.Fatalf("GET %s = %d, body %q", target, response.Code, response.Body.String())
		}
		if got := response.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("GET %s Cache-Control = %q", target, got)
		}
	}
}

func TestEmbeddedFrontendDoesNotMaskMissingAPIOrAssets(t *testing.T) {
	for _, target := range []string{"/api/not-a-route", "/missing.js", "/.env"} {
		if response := request(t, target); response.Code != http.StatusNotFound {
			t.Fatalf("GET %s = %d, want 404", target, response.Code)
		}
	}
}

func TestEmbeddedFrontendCachePolicy(t *testing.T) {
	response := request(t, "/app.js")
	if response.Code != http.StatusOK {
		t.Fatalf("GET /app.js = %d", response.Code)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-cache, must-revalidate" {
		t.Fatalf("Cache-Control = %q", got)
	}
}
