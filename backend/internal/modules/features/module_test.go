package features

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cybercalc/internal/platform/featureflags"
)

func TestEndpointExposesOnlyFrontendSnapshot(t *testing.T) {
	set, err := featureflags.Parse("teachers,schools")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	New(set).RegisterRoutes(mux)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/features", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("got %d, want %d", response.Code, http.StatusOK)
	}
	var body struct {
		Version int             `json:"version"`
		Flags   map[string]bool `json:"flags"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Version != contractVersion || !body.Flags["teachers"] || !body.Flags["schools"] || body.Flags["practice"] {
		t.Fatalf("unexpected response: %#v", body)
	}
}
