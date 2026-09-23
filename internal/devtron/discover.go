package devtron

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Flavor names a monitoring backend.
type Flavor string

const (
	FlavorPrometheus      Flavor = "prometheus"
	FlavorVictoriaMetrics Flavor = "victoriametrics"
	FlavorThanos          Flavor = "thanos"
	FlavorMimir           Flavor = "mimir"
	FlavorAlertmanager    Flavor = "alertmanager"
	FlavorVMAlert         Flavor = "vmalert"
	FlavorUnknown         Flavor = "unknown"
)

// Endpoint is a discovered monitoring service and the API base that answered.
type Endpoint struct {
	Flavor  Flavor     `json:"flavor"`
	Service ServiceRef `json:"service"`
	// APIBase is prefixed to every API path. Empty for a stock Prometheus;
	// "/prometheus" or "/select/0/prometheus" for some VictoriaMetrics and
	// vmauth layouts.
	APIBase string `json:"apiBase"`
	// Ports are the container ports the Service exposes, kept so a failed
	// portless proxy attempt can be retried against an explicit port.
	Ports     []string `json:"ports,omitempty"`
	Reachable bool     `json:"reachable"`
	Detail    string   `json:"detail,omitempty"`
}

// Path joins the API base with an API path.
func (e *Endpoint) Path(p string) string {
	return strings.TrimRight(e.APIBase, "/") + "/" + strings.TrimLeft(p, "/")
}

// MonitoringStack is what a cluster has available for metrics and alerts.
// Either half can be missing, and a missing half is a fact the agent must
// state rather than paper over.
type MonitoringStack struct {
	ClusterID   int       `json:"clusterId"`
	ClusterName string    `json:"clusterName"`
	Metrics     *Endpoint `json:"metrics,omitempty"`
	Alerts      *Endpoint `json:"alerts,omitempty"`
	// Candidates is everything that looked like a monitoring service,
	// including what did not answer. Useful when discovery gets it wrong.
	Candidates   []Endpoint `json:"candidates,omitempty"`
	DiscoveredAt time.Time  `json:"discoveredAt"`
	Notes        []string   `json:"notes,omitempty"`
}

// HasMetrics reports a usable query endpoint.
func (m *MonitoringStack) HasMetrics() bool {
	return m != nil && m.Metrics != nil && m.Metrics.Reachable
}

// HasAlerts reports a usable alert endpoint.
func (m *MonitoringStack) HasAlerts() bool { return m != nil && m.Alerts != nil && m.Alerts.Reachable }

// Summary is a one-line description for the ledger and the model.
func (m *MonitoringStack) Summary() string {
	if m == nil {
		return "no monitoring discovered"
	}
	parts := []string{}
	if m.HasMetrics() {
		parts = append(parts, fmt.Sprintf("metrics=%s (%s/%s)", m.Metrics.Flavor, m.Metrics.Service.Namespace, m.Metrics.Service.Name))
	} else {
		parts = append(parts, "metrics=none")
	}
	if m.HasAlerts() {
		parts = append(parts, fmt.Sprintf("alerts=%s (%s/%s)", m.Alerts.Flavor, m.Alerts.Service.Namespace, m.Alerts.Service.Name))
	} else {
		parts = append(parts, "alerts=none")
	}
	return strings.Join(parts, ", ")
}

// discoveryCEL matches any Service whose name looks like part of a metrics or
// alerting stack. One call returns whole objects, so labels and ports come
// back with it.
const discoveryCEL = `self.metadata.name.contains('prometheus') || ` +
	`self.metadata.name.contains('victoria') || ` +
	`self.metadata.name.contains('vmsingle') || ` +
	`self.metadata.name.contains('vmselect') || ` +
	`self.metadata.name.contains('vmauth') || ` +
	`self.metadata.name.contains('vmalert') || ` +
	`self.metadata.name.contains('alertmanager') || ` +
	`self.metadata.name.contains('thanos') || ` +
	`self.metadata.name.contains('mimir')`

// Discoverer finds and caches each cluster's monitoring stack.
type Discoverer struct {
	c   *Client
	ttl time.Duration

	mu    sync.Mutex
	cache map[int]*MonitoringStack
}

// NewDiscoverer builds a discoverer. A 15 minute TTL is long enough that a
// burst of runs costs one discovery and short enough to notice a new install.
func NewDiscoverer(c *Client, ttl time.Duration) *Discoverer {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return &Discoverer{c: c, ttl: ttl, cache: map[int]*MonitoringStack{}}
}

// Get returns the cluster's monitoring stack, discovering it when the cached
// answer is missing or stale.
func (d *Discoverer) Get(ctx context.Context, clusterID int, clusterName string) (*MonitoringStack, error) {
	d.mu.Lock()
	if m, ok := d.cache[clusterID]; ok && time.Since(m.DiscoveredAt) < d.ttl {
		d.mu.Unlock()
		return m, nil
	}
	d.mu.Unlock()

	m, err := d.discover(ctx, clusterID, clusterName)
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	d.cache[clusterID] = m
	d.mu.Unlock()
	return m, nil
}

// InvalidateAll drops every cached stack, which is what must happen when the
// client is pointed at a different Devtron installation.
func (d *Discoverer) InvalidateAll() {
	d.mu.Lock()
	d.cache = map[int]*MonitoringStack{}
	d.mu.Unlock()
}

// Invalidate drops a cluster's cached stack.
func (d *Discoverer) Invalidate(clusterID int) {
	d.mu.Lock()
	delete(d.cache, clusterID)
	d.mu.Unlock()
}

func (d *Discoverer) discover(ctx context.Context, clusterID int, clusterName string) (*MonitoringStack, error) {
	out := &MonitoringStack{ClusterID: clusterID, ClusterName: clusterName, DiscoveredAt: time.Now()}

	list, err := d.c.ListResources(ctx, ResourceQuery{
		ClusterID: clusterID,
		GVK:       GVKService,
		Filter:    discoveryCEL,
	})
	if err != nil {
		out.Notes = append(out.Notes, "service discovery failed: "+err.Error())
		return out, nil
	}
	cands := classify(list.Objects)
	if len(cands) == 0 {
		out.Notes = append(out.Notes, "no Prometheus, VictoriaMetrics, Alertmanager or vmalert Service found in this cluster")
		return out, nil
	}

	// Probe metrics candidates best-first and keep the first that answers.
	for i := range cands {
		e := &cands[i]
		switch e.Flavor {
		case FlavorPrometheus, FlavorVictoriaMetrics, FlavorThanos, FlavorMimir:
			if d.probeMetrics(ctx, clusterID, e) && out.Metrics == nil {
				out.Metrics = e
			}
		case FlavorAlertmanager, FlavorVMAlert:
			if d.probeAlerts(ctx, clusterID, e) && out.Alerts == nil {
				out.Alerts = e
			}
		}
	}
	out.Candidates = cands
	if out.Metrics == nil {
		out.Notes = append(out.Notes, "a metrics Service was found but none answered a query; coverage is unknown, not healthy")
	}
	if out.Alerts == nil {
		out.Notes = append(out.Notes, "no alert source answered; firing alerts cannot be listed for this cluster")
	}
	return out, nil
}

// probeMetrics tries each plausible API base until one answers a trivial
// query. VictoriaMetrics installs differ in whether the Prometheus-compatible
// API sits at the root or under a prefix, and guessing wrong looks exactly
// like an unreachable service.
func (d *Discoverer) probeMetrics(ctx context.Context, clusterID int, e *Endpoint) bool {
	bases := []string{""}
	if e.Flavor == FlavorVictoriaMetrics {
		bases = []string{"", "/prometheus", "/select/0/prometheus"}
	}
	q := url.Values{}
	q.Set("query", "1")
	for _, base := range bases {
		e.APIBase = base
		body, err := d.tryGet(ctx, clusterID, e, e.Path("api/v1/query"), q)
		if err != nil {
			e.Detail = err.Error()
			continue
		}
		var probe struct {
			Status string `json:"status"`
		}
		if json.Unmarshal(body, &probe) == nil && probe.Status == "success" {
			e.Reachable = true
			e.Detail = ""
			return true
		}
	}
	e.APIBase = ""
	return false
}

func (d *Discoverer) probeAlerts(ctx context.Context, clusterID int, e *Endpoint) bool {
	path := "api/v2/status" // Alertmanager
	if e.Flavor == FlavorVMAlert {
		path = "api/v1/alerts" // vmalert has no v2 API
	}
	if _, err := d.tryGet(ctx, clusterID, e, path, nil); err != nil {
		e.Detail = err.Error()
		return false
	}
	e.Reachable = true
	e.Detail = ""
	return true
}

// tryGet attempts the portless proxy target first, because naming a port
// requires the token to hold "*" on resource names. If that fails and the
// Service advertises ports, it retries with each one.
func (d *Discoverer) tryGet(ctx context.Context, clusterID int, e *Endpoint, path string, q url.Values) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	svc := e.Service
	svc.Port = ""
	body, err := d.c.ServiceProxyGet(ctx, ScopeCluster, clusterID, svc, path, q)
	if err == nil {
		e.Service.Port = ""
		return body, nil
	}
	firstErr := err
	for _, p := range e.Ports {
		svc.Port = p
		body, err := d.c.ServiceProxyGet(ctx, ScopeCluster, clusterID, svc, path, q)
		if err == nil {
			e.Service.Port = p
			return body, nil
		}
	}
	return nil, firstErr
}

// classify turns Service objects into ranked endpoint candidates. Ordering
// matters: a purpose-built query service beats an operator's headless
// "-operated" Service, which often has no usable HTTP route.
func classify(objects []map[string]any) []Endpoint {
	var out []Endpoint
	for _, o := range objects {
		md, _ := o["metadata"].(map[string]any)
		if md == nil {
			continue
		}
		name, _ := md["name"].(string)
		ns, _ := md["namespace"].(string)
		if name == "" || ns == "" {
			continue
		}
		f := flavorOf(name, labelsOf(md))
		if f == FlavorUnknown {
			continue
		}
		out = append(out, Endpoint{
			Flavor:  f,
			Service: ServiceRef{Namespace: ns, Name: name},
			Ports:   portsOf(o),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	return out
}

func flavorOf(name string, labels map[string]string) Flavor {
	n := strings.ToLower(name)
	ln := strings.ToLower(labels["app.kubernetes.io/name"] + " " + labels["app"])
	all := n + " " + ln
	switch {
	case strings.Contains(all, "vmalert") && !strings.Contains(all, "vmalertmanager"):
		return FlavorVMAlert
	case strings.Contains(all, "alertmanager"):
		return FlavorAlertmanager
	case strings.Contains(all, "vmsingle"), strings.Contains(all, "vmselect"),
		strings.Contains(all, "victoria"), strings.Contains(all, "vmauth"):
		return FlavorVictoriaMetrics
	case strings.Contains(all, "thanos"):
		return FlavorThanos
	case strings.Contains(all, "mimir"):
		return FlavorMimir
	case strings.Contains(all, "prometheus"):
		return FlavorPrometheus
	}
	return FlavorUnknown
}

// rank orders candidates so the most likely to answer is probed first.
func rank(e Endpoint) int {
	n := strings.ToLower(e.Service.Name)
	switch {
	case strings.Contains(n, "operated"), strings.Contains(n, "headless"):
		return 90 // operator-managed, usually not a good HTTP target
	case strings.Contains(n, "server"), strings.Contains(n, "vmsingle"),
		strings.Contains(n, "vmselect"), strings.Contains(n, "query-frontend"):
		return 10
	default:
		return 50
	}
}

func labelsOf(md map[string]any) map[string]string {
	out := map[string]string{}
	l, _ := md["labels"].(map[string]any)
	for k, v := range l {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

func portsOf(o map[string]any) []string {
	spec, _ := o["spec"].(map[string]any)
	if spec == nil {
		return nil
	}
	var out []string
	for _, p := range asSlice(spec["ports"]) {
		pm, _ := p.(map[string]any)
		if pm == nil {
			continue
		}
		switch v := pm["port"].(type) {
		case float64:
			out = append(out, fmt.Sprintf("%d", int(v)))
		case string:
			out = append(out, v)
		}
	}
	return out
}
