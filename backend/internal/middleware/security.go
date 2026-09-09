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

// Security applies to every route, including unauthenticated errors.
// Unsafe requests require a header that HTML forms cannot set. Cross-origin
// fetches require a preflight, and this application does not enable CORS.
func Security(next http.Handler, publicURL string, requireMFA ...bool) http.Handler {
	requests := make(chan struct{}, 32)
	heavy := make(chan struct{}, 2)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case requests <- struct{}{}:
			defer func() { <-requests }()
		default:
			w.Header().Set("Retry-After", "5")
			WriteError(w, 503, "сервер занят, повторите запрос")
			return
		}
		if strings.Contains(r.URL.Path, "/attachments") && r.Method == "POST" || strings.Contains(r.URL.Path, "/import") && r.Method == "POST" || strings.HasPrefix(r.URL.Path, "/api/reports/") {
			select {
			case heavy <- struct{}{}:
				defer func() { <-heavy }()
			default:
				w.Header().Set("Retry-After", "5")
				WriteError(w, 429, "обработка файлов занята, повторите запрос")
				return
			}
		}
		started := time.Now()
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		requestID := hex.EncodeToString(id[:])
		w.Header().Set("X-Request-ID", requestID)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("request panic", "request_id", requestID)
				WriteError(w, 500, "ошибка сервера")
			}
			slog.Info("request", "request_id", requestID, "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(started).Milliseconds())
		}()
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
