package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cybercalc/internal/config"
	"cybercalc/internal/platform/featureflags"
)

func TestBuildRoutesMountsAllProtectedModuleRoutes(t *testing.T) {
	handler := BuildRoutes(nil, config.Config{})
	routes := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/auth/password"},
		{http.MethodPost, "/api/auth/mfa/enroll"},
		{http.MethodPost, "/api/auth/mfa/confirm"},
		{http.MethodGet, "/api/auth/me"},
		{http.MethodPost, "/api/auth/entity-type"},
		{http.MethodGet, "/api/partners"},
		{http.MethodPost, "/api/partners"},
		{http.MethodGet, "/api/partners/example/org-units"},
		{http.MethodPost, "/api/partners/example/org-units"},
		{http.MethodPut, "/api/org-units/example"},
		{http.MethodDelete, "/api/org-units/example"},
		{http.MethodGet, "/api/partners/example/academic-groups"},
		{http.MethodPost, "/api/partners/example/academic-groups"},
		{http.MethodPut, "/api/academic-groups/example"},
		{http.MethodDelete, "/api/academic-groups/example"},
		{http.MethodGet, "/api/partners/example/specialty-codes"},
		{http.MethodGet, "/api/it-companies"},
		{http.MethodGet, "/api/it-companies/registry-search"},
		{http.MethodPost, "/api/it-companies"},
		{http.MethodGet, "/api/it-companies/template"},
		{http.MethodPost, "/api/it-companies/import"},
		{http.MethodGet, "/api/directory"},
		{http.MethodGet, "/api/directory/stats"},
		{http.MethodPost, "/api/directory"},
		{http.MethodGet, "/api/directory/proposals"},
		{http.MethodPost, "/api/directory/example/decision"},
		{http.MethodPut, "/api/directory/example"},
		{http.MethodGet, "/api/agreements"},
		{http.MethodPost, "/api/agreements"},
		{http.MethodPut, "/api/agreements/example"},
		{http.MethodGet, "/api/legal-entity-groups"},
		{http.MethodPost, "/api/legal-entity-groups"},
		{http.MethodPut, "/api/legal-entity-groups/example"},
		{http.MethodGet, "/api/regional-authorities"},
		{http.MethodPost, "/api/regional-authorities"},
		{http.MethodPut, "/api/regional-authorities/example"},
		{http.MethodGet, "/api/mentors"},
		{http.MethodPost, "/api/mentors"},
		{http.MethodGet, "/api/admin/directory-template"},
		{http.MethodPost, "/api/admin/directory-import"},
		{http.MethodGet, "/api/obligations"},
		{http.MethodGet, "/api/entries/import-template"},
		{http.MethodPost, "/api/entries/import"},
		{http.MethodGet, "/api/categories"},
		{http.MethodGet, "/api/entries"},
		{http.MethodGet, "/api/entries/summary"},
		{http.MethodPost, "/api/entries"},
		{http.MethodPut, "/api/entries/example"},
		{http.MethodGet, "/api/entries/example/comments"},
		{http.MethodPost, "/api/entries/example/attachments"},
		{http.MethodGet, "/api/entries/example/attachments"},
		{http.MethodGet, "/api/attachments/example/download"},
		{http.MethodGet, "/api/dashboard"},
		{http.MethodPost, "/api/dashboard/target"},
		{http.MethodGet, "/api/reports/export"},
		{http.MethodGet, "/api/report-workflow"},
		{http.MethodPost, "/api/report-workflow/transition"},
		{http.MethodGet, "/api/admin/users"},
		{http.MethodGet, "/api/admin/it-company-options"},
		{http.MethodPost, "/api/admin/users"},
		{http.MethodPatch, "/api/admin/users/example"},
		{http.MethodGet, "/api/admin/settings"},
		{http.MethodPost, "/api/admin/settings"},
		{http.MethodGet, "/api/admin/logs"},
		{http.MethodGet, "/api/okz"},
		{http.MethodGet, "/api/admin/okz/versions"},
		{http.MethodGet, "/api/admin/okz/template"},
		{http.MethodPost, "/api/admin/okz/import"},
	}

	for _, route := range routes {
		name := route.method + " " + route.path
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(route.method, route.path, nil)
			request.Header.Set("X-Cybercalc-Request", "1")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("got %d for %s, want %d; body=%q", response.Code, name, http.StatusUnauthorized, response.Body.String())
			}
		})
	}
}

func TestBuildRoutesMountsPublicModuleRoutes(t *testing.T) {
	handler := BuildRoutes(nil, config.Config{})
	tests := []struct {
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{http.MethodGet, "/api/live", "", http.StatusOK},
		{http.MethodGet, "/api/ready", "", http.StatusServiceUnavailable},
		{http.MethodGet, "/api/health", "", http.StatusServiceUnavailable},
		{http.MethodGet, "/api/features", "", http.StatusOK},
		{http.MethodPost, "/api/auth/login", "", http.StatusBadRequest},
		{http.MethodPost, "/api/auth/logout", "", http.StatusOK},
	}

	for _, test := range tests {
		request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		request.Header.Set("X-Cybercalc-Request", "1")
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.wantStatus {
			t.Fatalf("%s %s returned %d, want %d; body=%q", test.method, test.path, response.Code, test.wantStatus, response.Body.String())
		}
	}
}

func TestBuildRoutesPublishesOnlyConfiguredFrontendFlags(t *testing.T) {
	frontendFlags, err := featureflags.Parse("teachers")
	if err != nil {
		t.Fatal(err)
	}
	backendFlags, err := featureflags.Parse("schools")
	if err != nil {
		t.Fatal(err)
	}
	handler := BuildRoutes(nil, config.Config{FrontendFeatureFlags: frontendFlags, BackendFeatureFlags: backendFlags})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/features", nil))

	var body struct {
		Flags map[string]bool `json:"flags"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Flags["teachers"] || body.Flags["schools"] {
		t.Fatalf("frontend snapshot leaked or lost flags: %#v", body.Flags)
	}
}
