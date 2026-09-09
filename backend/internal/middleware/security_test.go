package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCrossOriginProtection(t *testing.T) {
	h := Security(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }), "https://calc.example")
	for _, tt := range []struct {
		origin, site, header string
		want                 int
	}{
		{"https://calc.example", "same-origin", "1", 204},
		{"https://evil.example", "same-site", "1", 403},
		{"null", "cross-site", "1", 403},
		{"", "", "", 403},
		{"", "", "1", 204},
	} {
		r := httptest.NewRequest("POST", "https://calc.example/api/test", nil)
		r.Header.Set("Origin", tt.origin)
		r.Header.Set("Sec-Fetch-Site", tt.site)
		r.Header.Set("X-Cybercalc-Request", tt.header)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tt.want {
			t.Fatalf("%+v got %d", tt, w.Code)
		}
		if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Request-ID") == "" {
			t.Fatal("missing security headers")
		}
	}
}
