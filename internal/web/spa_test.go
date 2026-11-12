package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func testFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":        {Data: []byte("<!doctype html><div id=root></div>")},
		"assets/app-abc.js": {Data: []byte("console.log('ok')")},
		"favicon.ico":       {Data: []byte("icon")},
	}
}

func request(t *testing.T, h http.Handler, method, target, accept string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHandlerServesIndex(t *testing.T) {
	h := NewHandler(testFS())

	rec := request(t, h, http.MethodGet, "/", "text/html")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Errorf("X-Frame-Options = %q, want SAMEORIGIN", got)
	}
	if !strings.Contains(rec.Body.String(), "id=root") {
		t.Errorf("body = %q, want index.html", rec.Body.String())
	}
}

func TestHandlerFallsBackForClientRoutes(t *testing.T) {
	h := NewHandler(testFS())

	rec := request(t, h, http.MethodGet, "/web/catalog/movies", "text/html")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /web/catalog/movies = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "id=root") {
		t.Errorf("body = %q, want index.html", rec.Body.String())
	}
}

func TestHandlerServesHashedAssetsImmutable(t *testing.T) {
	h := NewHandler(testFS())

	rec := request(t, h, http.MethodGet, "/assets/app-abc.js", "*/*")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET asset = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Errorf("Cache-Control = %q, want immutable", got)
	}
	if got := rec.Body.String(); got != "console.log('ok')" {
		t.Errorf("body = %q", got)
	}
}

func TestHandlerMissingAssetIs404NotIndex(t *testing.T) {
	h := NewHandler(testFS())

	rec := request(t, h, http.MethodGet, "/assets/missing.js", "*/*")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET missing asset = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "id=root") {
		t.Error("missing asset returned index.html; browsers will report a MIME error instead of a 404")
	}
}

func TestHandlerNeverServesIndexForJSONNavigations(t *testing.T) {
	h := NewHandler(testFS())

	rec := request(t, h, http.MethodGet, "/web/catalog", "application/json")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET client route with Accept JSON = %d, want 404", rec.Code)
	}
}

func TestHandlerRejectsNonGet(t *testing.T) {
	h := NewHandler(testFS())

	rec := request(t, h, http.MethodPost, "/web/catalog", "text/html")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST = %d, want 405", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
		t.Errorf("Allow = %q, want %q", got, "GET, HEAD")
	}
}

func TestHandlerHeadHasNoBody(t *testing.T) {
	h := NewHandler(testFS())

	rec := request(t, h, http.MethodHead, "/", "text/html")
	if rec.Code != http.StatusOK {
		t.Fatalf("HEAD / = %d, want 200", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("HEAD body = %q, want empty", rec.Body.String())
	}
}

func TestAssetNameStripsTraversal(t *testing.T) {
	cases := map[string]string{
		"/":                  "index.html",
		"/assets/x.js":       "assets/x.js",
		"/../../etc/passwd":  "etc/passwd",
		"/web/../dashboard/": "dashboard",
	}
	for in, want := range cases {
		if got := assetName(in); got != want {
			t.Errorf("assetName(%q) = %q, want %q", in, got, want)
		}
	}
}
