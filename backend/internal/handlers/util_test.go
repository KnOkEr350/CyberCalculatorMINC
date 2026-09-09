package handlers

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJSONBodyBoundary(t *testing.T) {
	for _, tt := range []struct {
		body, content string
		valid         bool
	}{
		{`{"x":1}`, "application/json", true},
		{`{"x":1}`, "text/plain", false},
		{`{"x":1} {"x":2}`, "application/json", false},
		{`{"x":1}` + strings.Repeat(" ", 1<<20) + "tail", "application/json", false},
	} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(tt.body))
		r.Header.Set("Content-Type", tt.content)
		var v struct {
			X int `json:"x"`
		}
		if err := decodeJSON(r, &v); (err == nil) != tt.valid {
			t.Fatalf("valid=%v err=%v", tt.valid, err)
		}
	}
}
