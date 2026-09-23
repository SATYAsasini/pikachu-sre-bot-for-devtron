package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRouterAPIIsNotShadowedByUI(t *testing.T) {
	h := fixtureServer().Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/healthz", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok"`) {
		t.Fatalf("/v1/healthz: %d %q", rec.Code, rec.Body.String())
	}

	for _, target := range []string{"/v1/does-not-exist", "/v1/runs/x/unknown", "/v1/ünïcode"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: status %d, want 404", target, rec.Code)
		}
		if strings.Contains(rec.Body.String(), fixtureUIIndex) {
			t.Fatalf("%s: API miss fell through to index.html", target)
		}
	}
}

func TestRouterServesUIOutsideAPI(t *testing.T) {
	h := fixtureServer().Handler()
	for _, target := range []string{"/", "/runs/0b7e", "/settings", "/v1x", "/v"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != fixtureUIIndex {
			t.Fatalf("%s: %d %q", target, rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/index-abc.js", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "void 0" {
		t.Fatalf("asset: %d %q", rec.Code, rec.Body.String())
	}
}

func TestRouterDefaultUIIsEmbedded(t *testing.T) {
	// With no UI injected the embedded build answers: 200 when it was built,
	// 503 with guidance when it was not. Either way, never a 404 or a panic.
	rec := httptest.NewRecorder()
	(&Server{}).Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK && rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("default UI: status %d", rec.Code)
	}
}
