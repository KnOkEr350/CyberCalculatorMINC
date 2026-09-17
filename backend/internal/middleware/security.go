package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *responseRecorder) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseRecorder) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(body)
	w.bytes += n
	return n, err
}

// Unwrap lets http.ResponseController reach optional interfaces implemented by
// the original writer without coupling this middleware to any HTTP server.
func (w *responseRecorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Security applies to every route, including unauthenticated errors.
// Unsafe requests require a header that HTML forms cannot set. Cross-origin
// fetches require a preflight, and this application does not enable CORS.
func Security(next http.Handler, publicURL string, requireMFA ...bool) http.Handler {
	requests := make(chan struct{}, 32)
	heavy := make(chan struct{}, 2)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		requestID := hex.EncodeToString(id[:])
		recorded := &responseRecorder{ResponseWriter: w}
		w = recorded
		w.Header().Set("X-Request-ID", requestID)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("request panic", "request_id", requestID)
				if recorded.status == 0 {
					WriteError(w, http.StatusInternalServerError, "ошибка сервера")
				}
			}
			status := recorded.status
			if status == 0 {
				status = http.StatusOK
			}
			slog.Info("request", "request_id", requestID, "method", r.Method, "path", r.URL.Path, "status", status, "bytes", recorded.bytes, "duration_ms", time.Since(started).Milliseconds())
		}()
		select {
		case requests <- struct{}{}:
			defer func() { <-requests }()
		default:
			w.Header().Set("Retry-After", "5")
			WriteError(w, 503, "сервер занят, повторите запрос")
			return
		}
		if (strings.Contains(r.URL.Path, "/attachments") && r.Method == "POST") ||
			(strings.Contains(r.URL.Path, "/import") && r.Method == "POST") ||
			strings.HasPrefix(r.URL.Path, "/api/reports/") {
			select {
			case heavy <- struct{}{}:
				defer func() { <-heavy }()
			default:
				w.Header().Set("Retry-After", "5")
				WriteError(w, 429, "обработка файлов занята, повторите запрос")
				return
			}
		}
		if len(r.URL.RawQuery) > 4096 {
			WriteError(w, 414, "слишком длинный запрос")
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
			if r.Header.Get("X-Cybercalc-Request") != "1" {
				WriteError(w, 403, "отсутствует защитный заголовок запроса; обновите страницу")
				return
			}
			if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" || site == "same-site" {
				WriteError(w, 403, "межсайтовый запрос запрещён")
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				expected := publicURL
				if expected == "" {
					scheme := "http"
					if r.TLS != nil {
						scheme = "https"
					}
					expected = scheme + "://" + r.Host
				}
				u, err := url.Parse(origin)
				if err != nil || u.User != nil || strings.TrimRight(origin, "/") != strings.TrimRight(expected, "/") {
					WriteError(w, 403, "источник запроса запрещён")
					return
				}
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		ctx = context.WithValue(ctx, ctxKey("require_mfa"), len(requireMFA) > 0 && requireMFA[0])
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
