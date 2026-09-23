package webui

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serve(t *testing.T, fsys fs.FS, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	Handler(fsys).ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestHandlerServesIndexForRoutes(t *testing.T) {
	for _, target := range []string{"/", "/index.html", "/runs/0b7e", "/settings", "/runs/", "/café", longRoute} {
		rec := serve(t, fixtureBuild(), http.MethodGet, target)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", target, rec.Code)
		}
		if rec.Body.String() != fixtureIndex {
			t.Fatalf("%s: body is not index.html: %q", target, rec.Body.String())
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
			t.Fatalf("%s: index Cache-Control = %q", target, got)
		}
		if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
			t.Fatalf("%s: Content-Type = %q", target, got)
		}
	}
}

func TestHandlerServesFiles(t *testing.T) {
	cases := []struct {
		target, body, cache string
	}{
		{"/assets/index-BYGcTU1T.js", fixtureJS, "public, max-age=31536000, immutable"},
		{"/assets/index-Bo2m9jmI.css", fixtureCSS, "public, max-age=31536000, immutable"},
		{"/favicon.svg", `<svg xmlns="http://www.w3.org/2000/svg"/>`, ""},
		{"/agent/caf%C3%A9-%C3%BC.png", "\x89PNG", ""},
	}
	for _, c := range cases {
		rec := serve(t, fixtureBuild(), http.MethodGet, c.target)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", c.target, rec.Code)
		}
		if rec.Body.String() != c.body {
			t.Fatalf("%s: body %q", c.target, rec.Body.String())
		}
		if got := rec.Header().Get("Cache-Control"); got != c.cache {
			t.Fatalf("%s: Cache-Control = %q, want %q", c.target, got, c.cache)
		}
	}
}

func TestHandlerMissingAssetIs404(t *testing.T) {
	for _, target := range []string{"/assets/index-OLD.js", "/nope.png", "/agent/missing.svg"} {
		rec := serve(t, fixtureBuild(), http.MethodGet, target)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: status %d, want 404", target, rec.Code)
		}
	}
}

func TestHandlerMalformedPathsStayInside(t *testing.T) {
	for _, target := range malformedPaths {
		rec := serve(t, fixtureBuild(), http.MethodGet, target)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", target, rec.Code)
		}
		body := rec.Body.String()
		if body != fixtureIndex && body != fixtureJS {
			t.Fatalf("%s: served something outside the build: %q", target, body)
		}
	}
}

func TestHandlerDirectoryFallsBackToIndex(t *testing.T) {
	rec := serve(t, fixtureBuild(), http.MethodGet, "/assets")
	if rec.Code != http.StatusOK || rec.Body.String() != fixtureIndex {
		t.Fatalf("directory listing leaked: %d %q", rec.Code, rec.Body.String())
	}
}

func TestHandlerHead(t *testing.T) {
	rec := serve(t, fixtureBuild(), http.MethodHead, "/runs/abc")
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("HEAD: %d body=%d bytes", rec.Code, rec.Body.Len())
	}
}

func TestHandlerRejectsWrites(t *testing.T) {
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		rec := serve(t, fixtureBuild(), m, "/")
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s: status %d", m, rec.Code)
		}
		if rec.Header().Get("Allow") != "GET, HEAD" {
			t.Fatalf("%s: Allow = %q", m, rec.Header().Get("Allow"))
		}
	}
}

func TestHandlerUnbuilt(t *testing.T) {
	for name, fsys := range map[string]fs.FS{"unbuilt": fixtureUnbuilt(), "empty": fixtureEmpty()} {
		rec := serve(t, fsys, http.MethodGet, "/runs/abc")
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: status %d, want 503", name, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "make web") {
			t.Fatalf("%s: body does not say how to fix it: %q", name, rec.Body.String())
		}
	}
}

func TestHandlerEmptyIndex(t *testing.T) {
	rec := serve(t, fixtureEmptyIndex(), http.MethodGet, "/")
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("empty index: %d %q", rec.Code, rec.Body.String())
	}
}

func TestHandlerBrokenFS(t *testing.T) {
	rec := serve(t, brokenFS{}, http.MethodGet, "/")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), errBroken.Error()) {
		t.Fatalf("internal error leaked to the client: %q", rec.Body.String())
	}
}

func TestDistIsValid(t *testing.T) {
	// Whatever the checkout embedded, Dist must be a readable root.
	if _, err := fs.ReadDir(Dist(), "."); err != nil {
		t.Fatalf("Dist: %v", err)
	}
}
