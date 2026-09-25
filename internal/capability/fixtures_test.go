package capability

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/devtron-labs/devtron-sre-agent/internal/devtron"
)

// Deterministic fixtures for the capability sweep. Built by functions so a
// test that mutates what it is handed cannot leak into the next.

// fxClusters is the shape of a real install: a couple of clusters that serve
// workloads and a pile that the orchestrator lists but cannot reach.
func fxClusters(usable, dead int) []map[string]any {
	out := make([]map[string]any, 0, usable+dead)
	for i := 1; i <= usable; i++ {
		out = append(out, map[string]any{"id": i, "cluster_name": nameFor("live", i)})
	}
	for i := 1; i <= dead; i++ {
		out = append(out, map[string]any{"id": 100 + i, "cluster_name": nameFor("dead", i)})
	}
	return out
}

func nameFor(prefix string, i int) string {
	return prefix + "-" + string(rune('a'+i-1))
}

// fxUnicodeClusters carries non-ASCII names, which reach a log line, a sort
// and the UI verbatim.
func fxUnicodeClusters() []map[string]any {
	return []map[string]any{
		{"id": 1, "cluster_name": "本番クラスタ"},
		{"id": 2, "cluster_name": "생산-클러스터"},
	}
}

// fxPod is one object, enough to make a probe report "usable".
func fxPod() map[string]any {
	return map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{"namespace": "devtroncd", "name": "pgvector-0"},
	}
}

// --- fake orchestrator -----------------------------------------------------

// fakeOrchestrator serves the two endpoints a sweep touches and counts what
// it was asked, so a test can assert how many sweeps actually happened.
type fakeOrchestrator struct {
	srv *httptest.Server

	mu sync.Mutex
	// listCalls counts /orchestrator/cluster/autocomplete, which is one per
	// sweep — the cheapest proxy for "how many sweeps ran".
	listCalls int
	// probeCalls counts /orchestrator/k8s/resource/list.
	probeCalls int
}

type fakeOpts struct {
	// Clusters is the cluster list returned.
	Clusters []map[string]any
	// Usable is the set of cluster ids whose probes answer with an object.
	// Anything else times out, which is what an unreachable cluster does.
	Usable map[int]bool
	// ProbeDelay stalls every probe, standing in for an orchestrator that
	// hangs on a cluster it cannot reach.
	ProbeDelay time.Duration
	// ListDelay stalls the cluster list.
	ListDelay time.Duration
}

func newFakeOrchestrator(o fakeOpts) (*fakeOrchestrator, *devtron.Client) {
	f := &fakeOrchestrator{}
	mux := http.NewServeMux()

	mux.HandleFunc("/orchestrator/cluster/autocomplete", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.listCalls++
		f.mu.Unlock()
		if o.ListDelay > 0 && !sleepCtx(r.Context(), o.ListDelay) {
			return
		}
		writeEnvelope(w, o.Clusters)
	})
	// Environments is the other half of Clusters(); an empty list is fine and
	// keeps the fixture to one source of cluster identity.
	mux.HandleFunc("/orchestrator/env/autocomplete/helm", func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelope(w, []any{})
	})

	// The reach check leads with this: which namespaces may the token use
	// here, and can the orchestrator reach the cluster at all.
	mux.HandleFunc("/orchestrator/cluster/namespaces/", func(w http.ResponseWriter, r *http.Request) {
		id := 0
		if parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/"); len(parts) > 0 {
			id, _ = strconv.Atoi(parts[len(parts)-1])
		}
		if o.ProbeDelay > 0 && !sleepCtx(r.Context(), o.ProbeDelay) {
			return
		}
		if !o.Usable[id] {
			// A cluster the orchestrator cannot get to.
			http.Error(w, `{"errors":[{"userMessage":"cluster is not reachable"}]}`, http.StatusBadRequest)
			return
		}
		writeEnvelope(w, []any{"devtroncd", "monitoring"})
	})

	mux.HandleFunc("/orchestrator/k8s/resource/list", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.probeCalls++
		f.mu.Unlock()

		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		var req struct {
			ClusterID int `json:"clusterId"`
		}
		_ = json.Unmarshal(body, &req)

		if o.ProbeDelay > 0 && !sleepCtx(r.Context(), o.ProbeDelay) {
			return
		}
		if !o.Usable[req.ClusterID] {
			// An unreachable cluster does not refuse, it never answers.
			<-r.Context().Done()
			return
		}
		writeEnvelope(w, []map[string]any{fxPod()})
	})

	f.srv = httptest.NewServer(mux)
	return f, devtron.New(devtron.Options{BaseURL: f.srv.URL, Token: "t", Timeout: 5 * time.Second})
}

// sleepCtx waits, reporting false when the caller gave up first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}

func writeEnvelope(w http.ResponseWriter, result any) {
	w.Header().Set("Content-Type", "application/json")
	body, _ := json.Marshal(map[string]any{"code": 200, "status": "OK", "result": result})
	_, _ = w.Write(body)
}

func (f *fakeOrchestrator) Close() { f.srv.Close() }

func (f *fakeOrchestrator) sweeps() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listCalls
}

func (f *fakeOrchestrator) probes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.probeCalls
}

// --- fake store ------------------------------------------------------------

// memStore is the persistence half, in memory, with the failure modes the
// real one has: a save that errors and a load that returns nothing.
type memStore struct {
	mu      sync.Mutex
	saved   []devtron.Capability
	saves   int
	SaveErr error
	LoadErr error
}

func (m *memStore) SaveCapabilities(_ context.Context, caps []devtron.Capability) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saves++
	if m.SaveErr != nil {
		return m.SaveErr
	}
	m.saved = append([]devtron.Capability(nil), caps...)
	return nil
}

func (m *memStore) LoadCapabilities(context.Context) ([]devtron.Capability, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.LoadErr != nil {
		return nil, m.LoadErr
	}
	return append([]devtron.Capability(nil), m.saved...), nil
}

func (m *memStore) ClearCapabilities(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saved = nil
	return nil
}

func (m *memStore) saveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saves
}

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// awaitSweep blocks until a sweep is actually in flight, so a test that
// means to observe one is not racing its own goroutine.
func awaitSweep(s *Service, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if s.inflight.Load() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}

// fastProber shortens the probe timeout so a test that deliberately hangs a
// cluster finishes in milliseconds rather than seconds.
func fastProber(s *Service, timeout time.Duration) {
	s.prober.Timeout = timeout
}
