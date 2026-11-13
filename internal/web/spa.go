package web

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// Handler serves the single-page app from an fs.FS. Requests that name a real
// file are served as-is; everything else falls back to index.html for browser
// navigations so client-side routes survive a reload. A missing asset is a 404,
// never the HTML shell, so module MIME errors stay debuggable.
type Handler struct {
	fsys fs.FS
}

func NewHandler(fsys fs.FS) *Handler {
	return &Handler{fsys: fsys}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := assetName(r.URL.Path)
	if info, err := fs.Stat(h.fsys, name); err == nil && !info.IsDir() {
		setCacheHeaders(w, name)
		http.ServeFileFS(w, r, h.fsys, name)
		return
	}

	if !wantsHTML(r) {
		http.NotFound(w, r)
		return
	}

	setCacheHeaders(w, "index.html")
	setSecurityHeaders(w, true)
	http.ServeFileFS(w, r, Dist(), "index.html")
}

func assetName(urlPath string) string {
	name := strings.TrimPrefix(path.Clean("/"+urlPath), "/")
	if name == "" || name == "." {
		return "index.html"
	}
	return name
}

func wantsHTML(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

func setCacheHeaders(w http.ResponseWriter, name string) {
	w.Header().Set("X-Content-Type-Options", "nosniff")

	switch {
	case name == "index.html":
		w.Header().Set("Cache-Control", "no-store")
		setSecurityHeaders(w, true)
	case strings.HasPrefix(name, "assets/"):
		// Vite content-hashes everything under assets/, so it is safe to cache
		// forever. The shell above is never cached, which is what makes that safe.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	default:
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
}

func setSecurityHeaders(w http.ResponseWriter, html bool) {
	if !html {
		return
	}
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
}
