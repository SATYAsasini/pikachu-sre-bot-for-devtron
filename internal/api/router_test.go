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
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/healthz", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok"`) {
		t.Fatalf("/v1/healthz: %d %q", rec.Code, rec.Body.String())
	}

	for _, target := range []string{"/v1/does-not-exist", "/v1/runs/x/unknown", "/v1/ünïcode"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil))
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
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != fixtureUIIndex {
			t.Fatalf("%s: %d %q", target, rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/assets/index-abc.js", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "void 0" {
		t.Fatalf("asset: %d %q", rec.Code, rec.Body.String())
	}
}

func TestRouterDefaultUIIsEmbedded(t *testing.T) {
	// With no UI injected the embedded build answers: 200 when it was built,
	// 503 with guidance when it was not. Either way, never a 404 or a panic.
	rec := httptest.NewRecorder()
	(&Server{}).Handler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK && rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("default UI: status %d", rec.Code)
	}
}

// The monitoring picker writes with PUT and clears with DELETE. Both were
// added to a router whose CORS allow-list still said GET and POST.
func TestRouterAllowsTheVerbsItServes(t *testing.T) {
	rec := httptest.NewRecorder()
	fixtureServer().Handler().ServeHTTP(rec,
		httptest.NewRequestWithContext(t.Context(), http.MethodOptions, "/v1/clusters/1/monitoring", nil))

	allow := rec.Header().Get("Access-Control-Allow-Methods")
	for _, verb := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		if !strings.Contains(allow, verb) {
			t.Errorf("%s is served but not advertised: %q", verb, allow)
		}
	}
}

// A bad cluster id is rejected before any dependency is touched, which is
// also what makes these routes testable without a database.
func TestMonitoringChoiceRejectsABadClusterID(t *testing.T) {
	h := fixtureServer().Handler()
	for _, tc := range []struct{ method, target string }{
		{http.MethodPut, "/v1/clusters/0/monitoring"},
		{http.MethodPut, "/v1/clusters/abc/monitoring"},
		{http.MethodDelete, "/v1/clusters/-1/monitoring"},
		{http.MethodDelete, "/v1/clusters/ünïcode/monitoring"},
		{http.MethodGet, "/v1/clusters/0/monitoring"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), tc.method, tc.target,
			strings.NewReader(`{"alerts":{"namespace":"monitoring","name":"vmalert"}}`)))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s %s: status %d, want 400", tc.method, tc.target, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "bad_cluster_id") {
			t.Errorf("%s %s: body %q", tc.method, tc.target, rec.Body.String())
		}
	}
}
