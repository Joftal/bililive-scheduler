package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed dist
var distFS embed.FS

func Handler() http.Handler {
	subFS, err := fs.Sub(distFS, "dist")
	if err != nil {
		// Fallback: serve a simple error page if embed is broken
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Web UI not available: "+err.Error(), http.StatusInternalServerError)
		})
	}
	return &SPAHandler{fs: subFS}
}

// SPAHandler serves a single-page application.
// Static files are served directly; all other paths fall back to index.html.
type SPAHandler struct {
	fs fs.FS
}

func (h *SPAHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}

	// Try to serve the exact file
	if f, err := h.fs.Open(path); err == nil {
		f.Close()
		http.FileServer(http.FS(h.fs)).ServeHTTP(w, r)
		return
	}

	// Fall back to index.html for SPA routing
	r.URL.Path = "/"
	http.FileServer(http.FS(h.fs)).ServeHTTP(w, r)
}
