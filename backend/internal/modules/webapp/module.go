// Package webapp serves the production SPA embedded into the server binary.
// The embedded copy is refreshed from the top-level frontend directory by the
// container build and checked against it by tests in a source checkout.
package webapp

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

type Module struct {
	assets fs.FS
}

func New() *Module {
	assets, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err)
	}
	return &Module{assets: assets}
}

func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /", m.serve)
}

func (m *Module) serve(w http.ResponseWriter, r *http.Request) {
	requestPath := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
	if strings.HasPrefix(requestPath, "/api/") || requestPath == "/api" || strings.Contains(requestPath, "/.") {
		http.NotFound(w, r)
		return
	}

	name := strings.TrimPrefix(requestPath, "/")
	if name == "" {
		name = "index.html"
	}
	body, err := fs.ReadFile(m.assets, name)
	if err != nil {
		// Client-side routes have no extension and fall back to the SPA shell.
		// Missing static files must stay 404 so a broken deployment is visible.
		if path.Ext(name) != "" {
			http.NotFound(w, r)
			return
		}
		name = "index.html"
		body, err = fs.ReadFile(m.assets, name)
		if err != nil {
			http.Error(w, "embedded frontend is unavailable", http.StatusInternalServerError)
			return
		}
	}

	switch name {
	case "index.html", "version.txt":
		w.Header().Set("Cache-Control", "no-store")
	default:
		// Current assets do not carry content hashes in their names. Revalidate
		// them on every navigation so application and API versions cannot drift.
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	}
	if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
