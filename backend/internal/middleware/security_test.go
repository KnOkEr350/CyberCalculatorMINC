package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"cybercalc/internal/platform/audit"
)

func TestSecurityPropagatesResponseRequestIDToAuditContext(t *testing.T) {
	var contextRequestID string
	handler := Security(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contextRequestID = audit.RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}), "")

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/health", nil))

	headerRequestID := response.Header().Get("X-Request-ID")
	if headerRequestID == "" {
		t.Fatal("security middleware must return a request id")
	}
	if contextRequestID != headerRequestID {
		t.Fatalf("audit context request id %q differs from response %q", contextRequestID, headerRequestID)
	}
}
