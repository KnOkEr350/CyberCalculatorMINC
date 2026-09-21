// Package httpx contains transport helpers shared by modular HTTP adapters.
package httpx

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"cybercalc/internal/platform/apperror"
)

// WriteJSON writes a JSON response using the same wire format as the legacy
// handlers. BASE-02 may version the envelope later without changing services.
func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if value == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("json encode error: %v", err)
	}
}

// WriteError converts a typed application error to HTTP. Unknown errors are
// deliberately hidden from the client.
func WriteError(w http.ResponseWriter, err error) {
	var applicationError *apperror.Error
	if !errors.As(err, &applicationError) {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "ошибка сервера"})
		return
	}

	message := applicationError.Message
	if message == "" {
		message = "ошибка сервера"
	}
	WriteJSON(w, statusFor(applicationError.Kind), map[string]string{"error": message})
}

func statusFor(kind apperror.Kind) int {
	switch kind {
	case apperror.KindValidation:
		return http.StatusBadRequest
	case apperror.KindUnauthorized:
		return http.StatusUnauthorized
	case apperror.KindForbidden:
		return http.StatusForbidden
	case apperror.KindNotFound:
		return http.StatusNotFound
	case apperror.KindConflict:
		return http.StatusConflict
	case apperror.KindUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
