package tests

import (
	"cybercalc/internal/auth"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSessionCookieSecurity(t *testing.T) {
	for _, secure := range []bool{false, true} {
		w := httptest.NewRecorder()
		auth.SetSessionCookie(w, auth.Session{Token: "token", ExpiresAt: time.Now().Add(time.Hour)}, secure)
		c := w.Result().Cookies()[0]
		if c.Name != auth.CookieName || c.Path != "/" || !c.HttpOnly || c.Secure != secure || c.SameSite != http.SameSiteLaxMode {
			t.Fatal("unsafe session cookie", c)
		}
		w = httptest.NewRecorder()
		auth.ClearSessionCookie(w, secure)
		c = w.Result().Cookies()[0]
		if c.Value != "" || c.MaxAge != -1 || !c.HttpOnly || c.Secure != secure || c.Path != "/" {
			t.Fatal("incorrect cookie deletion", c)
		}
	}
}
