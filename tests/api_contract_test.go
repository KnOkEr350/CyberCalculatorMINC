package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cybercalc/internal/config"
	"cybercalc/internal/platform/featureflags"
	appserver "cybercalc/internal/server"
)

func allBackendFeatures(t testing.TB) featureflags.Set {
	t.Helper()
	flags, err := featureflags.Parse("all")
	if err != nil {
		t.Fatal(err)
	}
	return flags
}

func TestProtectedAPIRoutesRejectAnonymousRequests(t *testing.T) {
	h := appserver.BuildRoutes(nil, config.Config{BackendFeatureFlags: allBackendFeatures(t)})
	for _, path := range []string{
		"/api/auth/me",
		"/api/partners",
		"/api/entries",
		"/api/report-workflow",
		"/api/admin/users",
	} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			res := httptest.NewRecorder()
			h.ServeHTTP(res, req)

			if res.Code != http.StatusUnauthorized {
				t.Fatalf("got status %d, want %d", res.Code, http.StatusUnauthorized)
			}
			var body map[string]string
			if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil || body["error"] == "" {
				t.Fatalf("expected JSON error, body=%q err=%v", res.Body.String(), err)
			}
			if res.Header().Get("Cache-Control") != "no-store" || res.Header().Get("X-Request-ID") == "" {
				t.Fatal("security headers are missing from an authentication error")
			}
		})
	}
}

func TestLoginEndpointRequiresRequestProtectionAndMethod(t *testing.T) {
	h := appserver.BuildRoutes(nil, config.Config{})

	withoutProtection := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, withoutProtection)
	if res.Code != http.StatusForbidden {
		t.Fatalf("unprotected login got %d, want %d", res.Code, http.StatusForbidden)
	}

	wrongMethod := httptest.NewRequest(http.MethodGet, "/api/auth/login", nil)
	res = httptest.NewRecorder()
	h.ServeHTTP(res, wrongMethod)
	if res.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET login got %d, want %d", res.Code, http.StatusMethodNotAllowed)
	}
}

func TestLivenessDoesNotDependOnDatabase(t *testing.T) {
	h := appserver.BuildRoutes(nil, config.Config{})
	req := httptest.NewRequest(http.MethodGet, "/api/live", nil)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("liveness got %d, want %d", res.Code, http.StatusOK)
	}
	var body map[string]string
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil || body["status"] != "ok" {
		t.Fatalf("unexpected liveness body %q: %v", res.Body.String(), err)
	}
}

func TestReadinessRequiresDatabase(t *testing.T) {
	h := appserver.BuildRoutes(nil, config.Config{})
	for _, path := range []string{"/api/ready", "/api/health"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			res := httptest.NewRecorder()
			h.ServeHTTP(res, req)

			if res.Code != http.StatusServiceUnavailable {
				t.Fatalf("got %d, want %d", res.Code, http.StatusServiceUnavailable)
			}
		})
	}
}
