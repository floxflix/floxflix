package handlers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/floxflix/floxflix/internal/config"
	"github.com/go-chi/chi/v5"
)

func TestFallbackKeepsAPIPathsJSON(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/v1/api/videos", func(w http.ResponseWriter, r *http.Request) {})
	RegisterFallback(r, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("SPA handled API path %s", r.URL.Path)
	}))

	for _, target := range []string{"/v1/api/nope", "/v1/api", "/healthz/nope"} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))

		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", target, rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
			t.Errorf("GET %s Content-Type = %q, want JSON", target, got)
		}
		if !strings.Contains(rec.Body.String(), `"status":"failed"`) {
			t.Errorf("GET %s body = %q, want JSON error", target, rec.Body.String())
		}
	}
}

func TestFallbackSendsClientRoutesToSPA(t *testing.T) {
	r := chi.NewRouter()
	RegisterFallback(r, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SPA", "1")
		io.WriteString(w, "spa")
	}))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dashboard/catalog/movies", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET client route = %d, want 200", rec.Code)
	}
	if rec.Header().Get("X-SPA") != "1" {
		t.Error("client route was not handed to the SPA")
	}
}

func TestFallbackAPIOnlyWithoutSPA(t *testing.T) {
	r := chi.NewRouter()
	RegisterFallback(r, nil)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /login = %d, want 404", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Errorf("Content-Type = %q, want JSON", got)
	}
}

func TestMethodNotAllowedStaysJSON(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/v1/api/videos", func(w http.ResponseWriter, r *http.Request) {})
	RegisterFallback(r, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("SPA handled method-not-allowed path %s", r.URL.Path)
	}))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/api/videos", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /v1/api/videos = %d, want 405", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"status":"failed"`) {
		t.Errorf("body = %q, want JSON error", rec.Body.String())
	}
}

func TestRuntimeConfig(t *testing.T) {
	r := chi.NewRouter()
	RegisterRuntimeConfig(r, &config.Config{
		UIStreamAPIURL:             "",
		UIStreamAPIPrefix:          "/v1/api",
		UIStreamAPIPrefixAdmin:     "/dashboard",
		UIDefaultNextEpisodeOffset: "15",
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/config.js", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /config.js = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/javascript") {
		t.Errorf("Content-Type = %q, want application/javascript", got)
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}

	body := rec.Body.String()
	for _, want := range []string{
		"window._env_",
		`"VITE_STREAMAPI_URL": ""`,
		`"VITE_STREAMAPI_PREFIX": "/v1/api"`,
		`"VITE_DEFAULT_NEXT_EPISODE_OFFSET": "15"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("config.js missing %q in:\n%s", want, body)
		}
	}
}
