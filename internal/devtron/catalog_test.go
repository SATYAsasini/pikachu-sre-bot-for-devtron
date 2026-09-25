package devtron

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeCatalog serves the two endpoints Clusters() reads, recording the
// auth= value of each autocomplete call so a test can assert which question
// was asked.
type fakeCatalog struct {
	srv *httptest.Server

	mu    sync.Mutex
	asked []string // auth= values, in order

	// byAuth is what /cluster/autocomplete returns for each auth= value. A
	// missing key means the call fails.
	byAuth map[string][]map[string]any
	// envs is what /env/autocomplete/helm returns.
	envs []map[string]any
	// envErr makes the environment call fail.
	envErr bool
}

func newFakeCatalog(byAuth map[string][]map[string]any, envs []map[string]any, envErr bool) (*fakeCatalog, *Client) {
	f := &fakeCatalog{byAuth: byAuth, envs: envs, envErr: envErr}
	mux := http.NewServeMux()

	mux.HandleFunc("/orchestrator/cluster/autocomplete", func(w http.ResponseWriter, r *http.Request) {
		auth := r.URL.Query().Get("auth")
		f.mu.Lock()
		f.asked = append(f.asked, auth)
		f.mu.Unlock()

		rows, ok := f.byAuth[auth]
		if !ok {
			http.Error(w, `{"errors":[{"userMessage":"unauthorized"}]}`, http.StatusForbidden)
			return
		}
		writeEnvelope(w, rows)
	})

	mux.HandleFunc("/orchestrator/env/autocomplete/helm", func(w http.ResponseWriter, _ *http.Request) {
		if f.envErr {
			http.Error(w, `{"errors":[{"userMessage":"nope"}]}`, http.StatusForbidden)
			return
		}
		writeEnvelope(w, f.envs)
	})

	f.srv = httptest.NewServer(mux)
	return f, New(Options{BaseURL: f.srv.URL, Token: "t", Timeout: 5 * time.Second})
}

func (f *fakeCatalog) Close() { f.srv.Close() }

func (f *fakeCatalog) questions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.asked...)
}

func cluster(id int, name string) map[string]any {
	return map[string]any{"id": id, "cluster_name": name}
}

func env(clusterID int, clusterName, ns string) map[string]any {
	return map[string]any{
		"clusterId":   clusterID,
		"clusterName": clusterName,
		"environments": []any{map[string]any{
			"environmentId": clusterID * 100, "environmentName": ns, "namespace": ns,
		}},
	}
}

func names(cs []Cluster) map[string]bool {
	out := map[string]bool{}
	for _, c := range cs {
		out[c.ClusterName] = true
	}
	return out
}

// The list must be the unfiltered one. Asking auth=true first hid every
// cluster the token lacked an explicit cluster-level grant on — not as
// forbidden, not as unreachable, simply absent, with nothing on screen to
// say why. Which clusters are usable is the probe's answer to give.
func TestClustersAsksForTheUnfilteredList(t *testing.T) {
	t.Parallel()

	f, c := newFakeCatalog(map[string][]map[string]any{
		"false": {cluster(1, "prod"), cluster(2, "staging"), cluster(3, "sandbox")},
		"true":  {cluster(1, "prod")},
	}, nil, false)
	defer f.Close()

	got, err := c.Clusters(t.Context())
	if err != nil {
		t.Fatalf("clusters: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want the unfiltered 3, got %d: %v", len(got), names(got))
	}
	if q := f.questions(); len(q) != 1 || q[0] != "false" {
		t.Errorf("want one auth=false call, got %v", q)
	}
}

// An install may refuse the unfiltered list to a non-admin token. That is not
// a reason to report no clusters.
func TestClustersFallsBackToTheRBACFilteredList(t *testing.T) {
	t.Parallel()

	f, c := newFakeCatalog(map[string][]map[string]any{
		// auth=false absent: the call fails.
		"true": {cluster(1, "prod")},
	}, nil, false)
	defer f.Close()

	got, err := c.Clusters(t.Context())
	if err != nil {
		t.Fatalf("clusters: %v", err)
	}
	if !names(got)["prod"] {
		t.Fatalf("want the RBAC-filtered fallback, got %v", names(got))
	}
	if q := f.questions(); len(q) != 2 || q[0] != "false" || q[1] != "true" {
		t.Errorf("want auth=false then auth=true, got %v", q)
	}
}

// An unfiltered list that comes back empty is treated the same as a refusal:
// try the narrower question before concluding there is nothing.
func TestClustersFallsBackOnAnEmptyUnfilteredList(t *testing.T) {
	t.Parallel()

	f, c := newFakeCatalog(map[string][]map[string]any{
		"false": {},
		"true":  {cluster(7, "only-one")},
	}, nil, false)
	defer f.Close()

	got, err := c.Clusters(t.Context())
	if err != nil {
		t.Fatalf("clusters: %v", err)
	}
	if !names(got)["only-one"] {
		t.Errorf("got %v", names(got))
	}
}

// A cluster the token can reach through an environment counts, whatever the
// cluster-level grant says.
func TestClustersIncludesEnvironmentOnlyClusters(t *testing.T) {
	t.Parallel()

	f, c := newFakeCatalog(
		map[string][]map[string]any{"false": {cluster(1, "prod")}},
		[]map[string]any{env(9, "env-only", "devtroncd")},
		false,
	)
	defer f.Close()

	got, err := c.Clusters(t.Context())
	if err != nil {
		t.Fatalf("clusters: %v", err)
	}
	n := names(got)
	if !n["prod"] || !n["env-only"] {
		t.Errorf("want both sources merged, got %v", n)
	}
}

// The same cluster from both sources is one cluster, and the autocomplete
// row wins because it carries the full record.
func TestClustersDeduplicatesAcrossSources(t *testing.T) {
	t.Parallel()

	f, c := newFakeCatalog(
		map[string][]map[string]any{"false": {cluster(1, "prod")}},
		[]map[string]any{env(1, "prod-from-env", "devtroncd")},
		false,
	)
	defer f.Close()

	got, err := c.Clusters(t.Context())
	if err != nil {
		t.Fatalf("clusters: %v", err)
	}
	if len(got) != 1 || got[0].ClusterName != "prod" {
		t.Errorf("want one cluster named prod, got %v", names(got))
	}
}

// Everything failing is an error, not an empty list — an empty list would be
// rendered as "this Devtron has no clusters", which is a different claim.
func TestClustersSurfacesFailureWhenNothingAnswers(t *testing.T) {
	t.Parallel()

	f, c := newFakeCatalog(map[string][]map[string]any{}, nil, true)
	defer f.Close()

	if _, err := c.Clusters(t.Context()); err == nil {
		t.Fatal("want an error when every source failed")
	}
}

// An install that genuinely has no clusters is not an error.
func TestClustersEmptyInstallIsNotAnError(t *testing.T) {
	t.Parallel()

	f, c := newFakeCatalog(map[string][]map[string]any{"false": {}, "true": {}}, []map[string]any{}, false)
	defer f.Close()

	got, err := c.Clusters(t.Context())
	if err != nil {
		t.Fatalf("an install with no clusters is a fact, not a failure: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want none, got %v", names(got))
	}
}

func TestClustersSortsByName(t *testing.T) {
	t.Parallel()

	f, c := newFakeCatalog(map[string][]map[string]any{
		"false": {cluster(1, "zulu"), cluster(2, "alpha"), cluster(3, "本番")},
	}, nil, false)
	defer f.Close()

	got, err := c.Clusters(t.Context())
	if err != nil {
		t.Fatalf("clusters: %v", err)
	}
	if len(got) != 3 || got[0].ClusterName != "alpha" || got[1].ClusterName != "zulu" {
		t.Errorf("want a stable name order, got %v", []string{got[0].ClusterName, got[1].ClusterName, got[2].ClusterName})
	}
}

// A probe's detail is read off a list row that clamps it to a line. Leading
// with the request path — ninety characters of namespace and service name
// for a proxy call — means the reader sees the path and none of the reason.
func TestErrorReasonLeadsWithTheReason(t *testing.T) {
	t.Parallel()

	const proxyPath = "/orchestrator/k8s/proxy/cluster/80/api/v1/namespaces/monitoring/" +
		"services/vmalert-victoria-metrics/proxy/api/v1/alerts"

	tests := []struct {
		name   string
		err    *Error
		prefix string
		says   string
	}{
		{"forbidden names the permission", &Error{Status: 403, Path: proxyPath}, "HTTP 403", "Kubernetes Resources"},
		{"unauthorized is the same class", &Error{Status: 401, Path: proxyPath}, "HTTP 401", "Kubernetes Resources"},
		{"not found is not a permission problem", &Error{Status: 404, Path: proxyPath}, "HTTP 404", "no such Service"},
		{"503 points at the port", &Error{Status: 503, Path: proxyPath, Body: "<html>503</html>"}, "HTTP 503", "port"},
		{"500 blames the orchestrator", &Error{Status: 500, Path: proxyPath}, "HTTP 500", "orchestrator"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.err.Reason()
			if !strings.HasPrefix(got, tt.prefix) {
				t.Errorf("want it to open with %q, got %q", tt.prefix, got)
			}
			if !strings.Contains(got, tt.says) {
				t.Errorf("want it to mention %q, got %q", tt.says, got)
			}
			if strings.Contains(got, "/orchestrator/") {
				t.Errorf("the path belongs in the tooltip, not the first line: %q", got)
			}
			// Short enough to survive a clamped row.
			if len(got) > 220 {
				t.Errorf("reason is %d chars, too long for a list row: %q", len(got), got)
			}
		})
	}
}

// An unrecognised status still has to say something useful.
func TestErrorReasonFallsBackToTheBody(t *testing.T) {
	t.Parallel()

	got := (&Error{Status: 418, Path: "/x", Body: "short and pot-shaped"}).Reason()
	if !strings.Contains(got, "418") || !strings.Contains(got, "pot-shaped") {
		t.Errorf("got %q", got)
	}
}

func TestProbeDetailClassifiesNonHTTPFailures(t *testing.T) {
	t.Parallel()

	if got := probeDetail(context.DeadlineExceeded); !strings.Contains(got, "timed out") {
		t.Errorf("got %q", got)
	}
	if got := probeDetail(context.Canceled); !strings.Contains(got, "ran out of time") {
		t.Errorf("got %q", got)
	}
	if got := probeDetail(&Error{Status: 403, Path: "/x"}); !strings.HasPrefix(got, "HTTP 403") {
		t.Errorf("an HTTP error should use Reason, got %q", got)
	}
	if got := probeDetail(errors.New("dial tcp: no route to host")); got != "dial tcp: no route to host" {
		t.Errorf("an unknown error passes through, got %q", got)
	}
}
