package handlers

import (
	"net/http"
	"strings"

	"github.com/floxflix/floxflix/internal/utils"
	"github.com/go-chi/chi/v5"
)

// RegisterFallback wires the router-level 404/405 handlers. API paths keep
// their JSON error shape; every other path is handed to the SPA, which serves
// index.html for browser navigations so client-side routes survive a reload.
// Passing a nil spa keeps the API-only behaviour.
func RegisterFallback(r chi.Router, spa http.Handler) {
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		if spa != nil && !isAPIPath(req.URL.Path) {
			spa.ServeHTTP(w, req)
			return
		}
		utils.Error(w, http.StatusNotFound, "Not found")
	})

	r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		utils.Error(w, http.StatusMethodNotAllowed, "Method not allowed")
	})
}

func isAPIPath(p string) bool {
	return p == "/v1/api" ||
		strings.HasPrefix(p, "/v1/api/") ||
		p == "/healthz" ||
		strings.HasPrefix(p, "/healthz/")
}
