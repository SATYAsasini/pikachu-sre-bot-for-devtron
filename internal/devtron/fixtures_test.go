package devtron

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

// Deterministic fixtures for discovery.
//
// Every one is built by a function rather than shared as a package variable,
// so a test that mutates what it is handed cannot leak into the next. The
// service lists mirror the shapes a real cluster produces: a handful of real
// query APIs buried in a pile of exporters that match the same name filter.

// svc builds one Service object the way the orchestrator's filtered resource
// list returns it.
func svc(ns, name string, ports ...int) map[string]any {
	p := make([]any, 0, len(ports))
	for _, n := range ports {
		p = append(p, map[string]any{"port": float64(n)})
	}
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata":   map[string]any{"namespace": ns, "name": name, "labels": map[string]any{}},
		"spec":       map[string]any{"ports": p},
	}
}

// fxBusyCluster is the shape this whole rewrite exists for: one real metrics
// backend and two real alert sources, behind a dozen exporters that match the
// discovery filter by name and answer nothing. On the cluster this was
// written against the two alert sources sorted eighteenth and nineteenth.
func fxBusyCluster() []map[string]any {
	return []map[string]any{
		svc("monitoring", "vmsingle-victoria-metrics", 8429, 8428),
		svc("monitoring", "vmalert-victoria-metrics", 8080),
		svc("monitoring", "vmalertmanager-victoria-metrics", 9093, 9094),
		svc("kube-system", "victoria-metrics-core-dns", 9153),
		svc("kube-system", "victoria-metrics-kube-etcd", 2379),
		svc("kube-system", "victoria-metrics-kube-scheduler", 10259),
		svc("kube-system", "kube-prom-kube-prometheus-kubelet", 10250, 10255, 4194),
		svc("monitoring", "victoria-metrics-stage-mon-grafana", 80),
		svc("monitoring", "victoria-metrics-stage-mon-kube-state-metrics", 8080),
		svc("monitoring", "victoria-metrics-stage-mon-prometheus-node-exporter", 9101),
		svc("monitoring", "victoria-metrics-stage-mon-victoria-metrics-operator", 8080, 9443),
		svc("monitoring", "vmagent-victoria-metrics", 8429),
		svc("optscale", "staging-optscale-finops-prometheus-pushgateway", 9091),
		svc("monitoring", "prometheus-operated", 9090),
		svc("alpha", "alertmanager-headless", 9093),
	}
}

// fxTwoAlertSources is the minimum case for the picker: two alert sources
// that both answer, so the heuristic has to choose and can choose wrong.
func fxTwoAlertSources() []map[string]any {
	return []map[string]any{
		svc("utils", "shared-monitoring-stack-ku-prometheus", 9090),
		svc("utils", "shared-monitoring-stack-ku-alertmanager", 9093),
		svc("utils", "alertmanager-operated", 9093),
	}
}

// fxExportersOnly has nothing that serves a query API at all. Discovery must
// say so rather than pick one and report it as coverage.
func fxExportersOnly() []map[string]any {
	return []map[string]any{
		svc("monitoring", "prometheus-node-exporter", 9100),
		svc("monitoring", "victoria-metrics-kube-state-metrics", 8080),
	}
}

// fxNoPorts is a Service with no ports at all: the portless proxy target is
// the only attempt that can be made.
func fxNoPorts() []map[string]any {
	return []map[string]any{svc("monitoring", "prometheus-server")}
}

// fxManyPorts advertises more ports than the retry budget allows, so the cap
// is what stops one dead Service eating the whole walk.
func fxManyPorts() []map[string]any {
	return []map[string]any{svc("monitoring", "prometheus-server", 1, 2, 3, 4, 5, 6)}
}

// fxUnicode carries non-ASCII in the namespace and name. They reach a URL
// path and an error message verbatim.
func fxUnicode() []map[string]any {
	return []map[string]any{
		svc("監視", "プロメテウス-prometheus-server", 9090),
		svc("监控", "alertmanager-告警", 9093),
	}
}

// fxMalformed is every broken object shape one list can contain: no
// metadata, no name, no namespace, a name that matches nothing, ports of the
// wrong type, and a nil spec. None of them may panic and none may become a
// candidate.
func fxMalformed() []map[string]any {
	return []map[string]any{
		{},
		{"metadata": nil},
		{"metadata": map[string]any{"name": "prometheus-server"}},               // no namespace
		{"metadata": map[string]any{"namespace": "monitoring"}},                 // no name
		{"metadata": map[string]any{"namespace": "x", "name": "redis-primary"}}, // no flavor
		{
			"metadata": map[string]any{"namespace": "monitoring", "name": "prometheus-weird"},
			"spec":     map[string]any{"ports": []any{map[string]any{"port": true}, nil, "nope"}},
		},
		{
			"metadata": map[string]any{"namespace": "monitoring", "name": "thanos-query-frontend", "labels": "not-a-map"},
			"spec":     nil,
		},
	}
}

// --- probe response bodies -------------------------------------------------

func fxPromOK() string      { return `{"status":"success","data":{"resultType":"vector","result":[]}}` }
func fxPromNotJSON() string { return `<html><body>login</body></html>` }
func fxAlertmanagerStatus() string {
	return `{"cluster":{"status":"ready"},"uptime":"2026-09-24T06:00:00Z"}`
}
func fxVMAlertAlerts() string { return `{"status":"success","data":{"alerts":[]}}` }
func fx503() string {
	return `<!DOCTYPE html><html><head><title>503 Service Unavailable</title></head><body>503</body></html>`
}

// --- fake orchestrator -----------------------------------------------------

// probeKey is how a test names one candidate's proxy responses.
func probeKey(ns, name string) string { return ns + "/" + name }

// fakeDevtron is an httptest orchestrator: one resource-list response and a
// per-Service answer for the Kubernetes proxy.
type fakeDevtron struct {
	srv *httptest.Server

	mu sync.Mutex
	// probes counts proxy attempts per "namespace/name", so a test can assert
	// what discovery did *not* bother to ask.
	probes map[string]int
	// listCalls counts resource-list calls, which is how single-flight is
	// observed.
	listCalls int
	// kindCalls counts the capability probe's kind-specific reads, which is
	// how "one request per cluster" is asserted.
	kindCalls int
	// nsCalls counts the namespace listings the reach check leads with.
	nsCalls int
}

// fakeOpts configures the orchestrator's behaviour.
type fakeOpts struct {
	// Objects is the service list returned to discovery.
	Objects []map[string]any
	// OK maps "namespace/name" to the body its proxy returns with a 200.
	// Anything not listed answers 503.
	OK map[string]string
	// Slow maps "namespace/name" to a delay before answering, for the
	// budget and cancellation cases.
	Slow map[string]time.Duration
	// NeedsPort, when set for a service, makes the portless target fail so
	// the port fallback is what has to work.
	NeedsPort map[string]bool
	// Namespaces is what GET /cluster/namespaces/{id} returns, which is the
	// call the reach check now leads with.
	Namespaces []string
	// NamespacesStatus makes that call fail with this HTTP status. 400 with
	// a "not reachable" body is how the orchestrator reports a cluster it
	// cannot get to.
	NamespacesStatus int
	NamespacesBody   string
	// ListKinds is how many objects each Kubernetes kind returns when listed
	// across all namespaces, for the capability probe. A kind that is absent
	// returns none; a nil map means kind-aware listing is off and Objects is
	// served instead, which is what the discovery tests want.
	ListKinds map[string]int
	// NamespacePods is how many pods a named namespace returns, for the
	// scoped-token case where the all-namespace list reads nothing.
	NamespacePods map[string]int
	// ListErr makes the resource list itself fail.
	ListErr bool
	// ListDelay stalls the resource list.
	ListDelay time.Duration
	// KindDelay stalls only the kind-specific reads, leaving the namespace
	// listing fast — the shape of a cluster that answers cheap calls and
	// times out on expensive ones.
	KindDelay time.Duration
}

func newFakeDevtron(o fakeOpts) (*fakeDevtron, *Client) {
	f := &fakeDevtron{probes: map[string]int{}}

	mux := http.NewServeMux()
	mux.HandleFunc("/orchestrator/cluster/namespaces/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.nsCalls++
		f.mu.Unlock()
		if o.ListDelay > 0 && !sleepCtxFake(r.Context(), o.ListDelay) {
			return
		}
		if o.NamespacesStatus != 0 {
			body := o.NamespacesBody
			if body == "" {
				body = `{"errors":[{"userMessage":"cluster is not reachable"}]}`
			}
			http.Error(w, body, o.NamespacesStatus)
			return
		}
		out := make([]any, 0, len(o.Namespaces))
		for _, n := range o.Namespaces {
			out = append(out, n)
		}
		writeEnvelope(w, out)
	})

	mux.HandleFunc("/orchestrator/k8s/resource/list", func(w http.ResponseWriter, r *http.Request) {
		// Read and count before any stall, so a fake that deliberately never
		// answers still records that it was asked.
		kindTarget := [2]string{}
		if o.ListKinds != nil {
			k, ns := listTarget(r)
			kindTarget = [2]string{k, ns}
		}
		f.mu.Lock()
		f.listCalls++
		if o.ListKinds != nil {
			f.kindCalls++
		}
		f.mu.Unlock()

		delay := o.ListDelay
		if o.ListKinds != nil && kindTarget[0] != "" && o.KindDelay > 0 {
			delay = o.KindDelay
		}
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
				return
			}
		}
		if o.ListErr {
			http.Error(w, `{"errors":[{"userMessage":"cluster unreachable"}]}`, http.StatusBadGateway)
			return
		}

		// The capability probe asks for a specific kind, optionally inside a
		// namespace. Discovery asks with a CEL filter and no kind, and wants
		// Objects.
		if o.ListKinds != nil {
			kind, ns := kindTarget[0], kindTarget[1]
			n := o.ListKinds[kind]
			if ns != "" {
				// A namespaced read finds what that namespace holds. When a
				// test says nothing about namespaces, the cluster's counts
				// stand in — the reach probe is namespace-scoped now, so a
				// fixture that only set ListKinds still means "this is what
				// is there".
				if len(o.NamespacePods) > 0 || kind != "Pod" {
					n = 0
				}
				if kind == "Pod" && len(o.NamespacePods) > 0 {
					n = o.NamespacePods[ns]
				}
			}
			writeEnvelope(w, objectsOfKind(kind, n))
			return
		}

		objs := o.Objects
		if objs == nil {
			objs = []map[string]any{}
		}
		body, _ := json.Marshal(map[string]any{"code": 200, "status": "OK", "result": objs})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})

	// /orchestrator/k8s/proxy/cluster/{id}/api/v1/namespaces/{ns}/services/{target}/proxy/{path}
	mux.HandleFunc("/orchestrator/k8s/proxy/", func(w http.ResponseWriter, r *http.Request) {
		ns, target, ok := parseProxyPath(r.URL.Path)
		if !ok {
			http.Error(w, "bad proxy path", http.StatusBadRequest)
			return
		}
		// "https:name:port" and "name:port" both reduce to the Service name.
		name, port := splitTarget(target)
		key := probeKey(ns, name)

		f.mu.Lock()
		f.probes[key]++
		f.mu.Unlock()

		if d := o.Slow[key]; d > 0 {
			select {
			case <-time.After(d):
			case <-r.Context().Done():
				return
			}
		}
		if o.NeedsPort[key] && port == "" {
			http.Error(w, fx503(), http.StatusServiceUnavailable)
			return
		}
		body, served := o.OK[key]
		if !served {
			http.Error(w, fx503(), http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})

	f.srv = httptest.NewServer(mux)
	c := New(Options{BaseURL: f.srv.URL, Token: "t", Timeout: 5 * time.Second})
	return f, c
}

func (f *fakeDevtron) Close() { f.srv.Close() }

// listTarget pulls the kind and namespace back out of a resource-list body.
func listTarget(r *http.Request) (kind, namespace string) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var req struct {
		K8sRequest struct {
			ResourceIdentifier struct {
				Namespace        string `json:"namespace"`
				GroupVersionKind struct {
					Kind string `json:"Kind"`
				} `json:"groupVersionKind"`
			} `json:"resourceIdentifier"`
		} `json:"k8sRequest"`
	}
	_ = json.Unmarshal(body, &req)
	return req.K8sRequest.ResourceIdentifier.GroupVersionKind.Kind,
		req.K8sRequest.ResourceIdentifier.Namespace
}

// objectsOfKind builds n listable objects, because a slice of nil maps
// marshals to [null, null, …] and is rightly counted as nothing.
func objectsOfKind(kind string, n int) []map[string]any {
	out := make([]map[string]any, 0, n)
	for i := range n {
		out = append(out, map[string]any{
			"apiVersion": "v1",
			"kind":       kind,
			"metadata":   map[string]any{"namespace": "devtroncd", "name": fmt.Sprintf("%s-%d", kind, i)},
		})
	}
	return out
}

func writeEnvelope(w http.ResponseWriter, result any) {
	w.Header().Set("Content-Type", "application/json")
	body, _ := json.Marshal(map[string]any{"code": 200, "status": "OK", "result": result})
	_, _ = w.Write(body)
}

func (f *fakeDevtron) probed(ns, name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.probes[probeKey(ns, name)]
}

// sleepCtxFake waits, reporting false when the caller gave up first.
func sleepCtxFake(ctx context.Context, d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}

// nsProbes is how many namespace listings were served.
func (f *fakeDevtron) nsProbes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.nsCalls
}

// kindProbes is how many kind-specific resource lists were served.
func (f *fakeDevtron) kindProbes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.kindCalls
}

func (f *fakeDevtron) lists() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listCalls
}

// parseProxyPath pulls the namespace and the proxy target back out of the
// path the client built.
func parseProxyPath(p string) (ns, target string, ok bool) {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "namespaces" {
			ns = parts[i+1]
		}
		if parts[i] == "services" {
			target = parts[i+1]
		}
	}
	if ns == "" || target == "" {
		return "", "", false
	}
	unescaped, err := unescape(ns)
	if err == nil {
		ns = unescaped
	}
	if t, err := unescape(target); err == nil {
		target = t
	}
	return ns, target, true
}

func splitTarget(target string) (name, port string) {
	t := strings.TrimPrefix(strings.TrimPrefix(target, "https:"), "http:")
	if i := strings.LastIndex(t, ":"); i >= 0 {
		return t[:i], t[i+1:]
	}
	return t, ""
}

func unescape(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			var v int
			if _, err := fmt.Sscanf(s[i+1:i+3], "%02x", &v); err == nil {
				b.WriteByte(byte(v))
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String(), nil
}
