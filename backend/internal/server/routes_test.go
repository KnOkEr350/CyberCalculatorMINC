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
	allFlags, err := featureflags.Parse("all")
	if err != nil {
		t.Fatal(err)
	}
	handler := BuildRoutes(nil, config.Config{BackendFeatureFlags: allFlags})
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
		{http.MethodGet, "/api/entries/example/cost-history"},
		{http.MethodGet, "/api/staff-members"},
		{http.MethodPost, "/api/staff-members"},
		{http.MethodPut, "/api/staff-members/example"},
		{http.MethodGet, "/api/teaching-payouts"},
		{http.MethodPost, "/api/teaching-payouts"},
		{http.MethodPut, "/api/teaching-payouts/example"},
		{http.MethodPost, "/api/entries/example/attachments"},
		{http.MethodGet, "/api/entries/example/attachments"},
		{http.MethodGet, "/api/attachments/example/download"},
		{http.MethodGet, "/api/dashboard"},
		{http.MethodPost, "/api/dashboard/target"},
		{http.MethodGet, "/api/reports/export"},
		{http.MethodGet, "/api/reports/generated"},
		{http.MethodGet, "/api/reports/generated/example"},
		{http.MethodGet, "/api/report-calendar"},
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
		{http.MethodGet, "/api/admin/normative-sources"},
		{http.MethodPost, "/api/admin/normative-sources/import"},
		{http.MethodGet, "/api/admin/normative-sources/diff"},
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

func TestBuildRoutesFailsClosedForDisabledBackendModules(t *testing.T) {
	handler := BuildRoutes(nil, config.Config{})
	for _, path := range []string{
		"/api/partners",
		"/api/entries",
		"/api/dashboard",
		"/api/reports/export",
		"/api/admin/users",
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("X-Cybercalc-Request", "1")
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("GET %s returned %d, want 404", path, response.Code)
		}
	}
}

func TestBuildRoutesEnablesOnlyRequestedBackendCapabilities(t *testing.T) {
	flags, err := featureflags.Parse("teachers")
	if err != nil {
		t.Fatal(err)
	}
	handler := BuildRoutes(nil, config.Config{BackendFeatureFlags: flags})

	for _, path := range []string{"/api/entries", "/api/staff-members", "/api/partners", "/api/okz"} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("X-Cybercalc-Request", "1")
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("GET %s returned %d, want 401", path, response.Code)
		}
	}

	for _, path := range []string{"/api/dashboard", "/api/reports/export", "/api/admin/users"} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("X-Cybercalc-Request", "1")
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("GET %s returned %d, want 404", path, response.Code)
		}
	}
}

func TestSharedReferenceRoutesFollowAnyEnabledCapability(t *testing.T) {
	flags, err := featureflags.Parse("dashboard_v44")
	if err != nil {
		t.Fatal(err)
	}
	handler := BuildRoutes(nil, config.Config{BackendFeatureFlags: flags})
	for _, path := range []string{"/api/categories", "/api/partners"} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("X-Cybercalc-Request", "1")
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("GET %s returned %d, want 401", path, response.Code)
		}
	}
}

func TestEveryBackendFeatureEnablesItsCapability(t *testing.T) {
	tests := []struct {
		flag featureflags.Name
		path string
	}{
		{featureflags.DashboardV44, "/api/dashboard"},
		{featureflags.PartnersV44, "/api/partners"},
		{featureflags.Teachers, "/api/staff-members"},
		{featureflags.OOPRPD, "/api/entries"},
		{featureflags.Internships, "/api/entries"},
		{featureflags.Practice, "/api/entries"},
		{featureflags.TopITAI, "/api/entries"},
		{featureflags.Schools, "/api/entries"},
		{featureflags.MinistryDecision, "/api/entries"},
		{featureflags.ReportingV44, "/api/reports/export"},
		{featureflags.SettingsV44, "/api/admin/users"},
	}
	for _, test := range tests {
		t.Run(string(test.flag), func(t *testing.T) {
			flags, err := featureflags.Parse(string(test.flag))
			if err != nil {
				t.Fatal(err)
			}
			handler := BuildRoutes(nil, config.Config{BackendFeatureFlags: flags})
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			request.Header.Set("X-Cybercalc-Request", "1")
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("GET %s returned %d, want 401", test.path, response.Code)
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
