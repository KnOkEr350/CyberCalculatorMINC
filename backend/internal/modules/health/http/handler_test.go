package healthhttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cybercalc/internal/modules/health/domain"
	"cybercalc/internal/platform/apperror"
)

type serviceStub struct {
	readinessError error
}

func (serviceStub) Liveness() domain.Status { return domain.OK() }
func (s serviceStub) Readiness(context.Context) (domain.Status, error) {
	if s.readinessError != nil {
		return domain.Status{}, s.readinessError
	}
	return domain.OK(), nil
}

func TestRegisteredRoutesPreserveHealthContract(t *testing.T) {
	unavailable := apperror.New(apperror.KindUnavailable, "database_unavailable", "база данных недоступна", nil, errors.New("offline"))
	tests := []struct {
		path        string
		service     serviceStub
		wantStatus  int
		wantContent string
	}{
		{"/api/live", serviceStub{}, http.StatusOK, `"status":"ok"`},
		{"/api/ready", serviceStub{}, http.StatusOK, `"status":"ok"`},
		{"/api/health", serviceStub{}, http.StatusOK, `"status":"ok"`},
		{"/api/ready", serviceStub{readinessError: unavailable}, http.StatusServiceUnavailable, `"error":"база данных недоступна"`},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			mux := http.NewServeMux()
			New(test.service).RegisterRoutes(mux)
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			if response.Code != test.wantStatus || !strings.Contains(response.Body.String(), test.wantContent) {
				t.Fatalf("got status=%d body=%q, want status=%d body containing %q", response.Code, response.Body.String(), test.wantStatus, test.wantContent)
			}
		})
	}
}
