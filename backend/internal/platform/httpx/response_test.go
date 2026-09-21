package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cybercalc/internal/platform/apperror"
)

func TestWriteErrorMapsKindsAndHidesUnknownErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{"validation", apperror.New(apperror.KindValidation, "invalid", "проверьте поля", nil, nil), http.StatusBadRequest, "проверьте поля"},
		{"not found", apperror.New(apperror.KindNotFound, "missing", "не найдено", nil, nil), http.StatusNotFound, "не найдено"},
		{"conflict", apperror.New(apperror.KindConflict, "conflict", "конфликт", nil, nil), http.StatusConflict, "конфликт"},
		{"unavailable", apperror.New(apperror.KindUnavailable, "offline", "недоступно", nil, nil), http.StatusServiceUnavailable, "недоступно"},
		{"unknown", errors.New("secret database details"), http.StatusInternalServerError, "ошибка сервера"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			WriteError(response, test.err)
			if response.Code != test.wantStatus || !strings.Contains(response.Body.String(), test.wantBody) {
				t.Fatalf("got status=%d body=%q, want status=%d body containing %q", response.Code, response.Body.String(), test.wantStatus, test.wantBody)
			}
			if test.name == "unknown" && strings.Contains(response.Body.String(), "database") {
				t.Fatal("internal error details leaked to client")
			}
		})
	}
}
