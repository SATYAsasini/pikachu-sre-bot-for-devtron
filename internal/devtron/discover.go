package devtron

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
)

// Flavor names a monitoring backend.
type Flavor string

// The monitoring backends discovery can recognise.
const (
	FlavorPrometheus      Flavor = "prometheus"
	FlavorVictoriaMetrics Flavor = "victoriametrics"
	FlavorThanos          Flavor = "thanos"
	FlavorMimir           Flavor = "mimir"
	FlavorAlertmanager    Flavor = "alertmanager"
	FlavorVMAlert         Flavor = "vmalert"
	FlavorUnknown         Flavor = "unknown"
)

// The budgets a walk runs under.
//
// These exist because discovery used to be unbounded and serial: on a real
// cluster with two dozen matching Services it took twenty-odd seconds, which
// is longer than most of its callers were prepared to wait. What made that a
// bug rather than a slow path is that the half-finished answer was then
// cached and served to everybody as fact.
const (
	// DiscoveryBudget caps one whole walk, however many candidates there are.
	DiscoveryBudget = 2 * time.Minute
	// candidateBudget caps everything spent on one candidate: every API base
	// and every port retry together, not each.
	candidateBudget = 40 * time.Second
	// DefaultDiscoveryProbeTimeout caps a single HTTP attempt.
	//
	// This was cut to 4s to make a walk finish sooner, which was a mistake.
	// The request is a service proxy hop — Devtron to the cluster's API
	// server to the pod — and on an install where a plain resource list takes
	// most of 7 seconds, 4 is not enough for any of it. Discovery is
	// single-flighted, detached from its caller and cached for fifteen
	// minutes, so being generous here costs almost nothing and being mean
	// costs an entire cluster's monitoring.
	DefaultDiscoveryProbeTimeout = 10 * time.Second
	// probeParallel is how many candidates are in flight at once. One dead
	// Service must not hold up the queue behind it.
	probeParallel = 6
	// maxPortRetries caps the portless-then-each-port fallback. A Service
	// advertising six ports is not worth thirty seconds — but it has to
	// clear the four a VictoriaMetrics install can legitimately use.
	maxPortRetries = 4
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
	// Probed records whether this candidate was actually tried. A walk stops
	// as soon as a half is satisfied, so "did not answer" and "was never
	// asked" are different facts and a picker must not draw them alike.
	Probed bool `json:"probed"`
	// ScrapeTarget marks a Service that carries a discovery keyword in its
	// name but exposes /metrics for something else to scrape rather than a
	// query API of its own — a node exporter, kube-state-metrics, Grafana.
	// They are kept as candidates, and skipped unless asked for.
	ScrapeTarget bool `json:"scrapeTarget,omitempty"`
	// Chosen marks the endpoint the operator pinned, as opposed to the one
	// the heuristic would have landed on.
	Chosen bool `json:"chosen,omitempty"`
}

// Path joins the API base with an API path.
func (e *Endpoint) Path(p string) string {
	return strings.TrimRight(e.APIBase, "/") + "/" + strings.TrimLeft(p, "/")
}

// IsAlertSource reports whether this endpoint belongs to the alerting half.
func (e *Endpoint) IsAlertSource() bool { return isAlertFlavor(e.Flavor) }

func isAlertFlavor(f Flavor) bool { return f == FlavorAlertmanager || f == FlavorVMAlert }

// Pick identifies one Service the operator chose to use.
//
// Namespace and name only. The flavor, the port and the API base are things
// discovery measures rather than things anybody should have to type, and
// pinning them would mean a chart upgrade that moved a port silently broke
// the choice.
type Pick struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// String renders the pick the way it is shown on screen.
func (p Pick) String() string { return p.Namespace + "/" + p.Name }

// Valid reports whether the pick names something.
func (p Pick) Valid() bool { return p.Namespace != "" && p.Name != "" }

func (p Pick) matches(e *Endpoint) bool {
	return e.Service.Namespace == p.Namespace && e.Service.Name == p.Name
}

// Choice is the operator's per-cluster override of what discovery would pick.
//
// Discovery still finds everything; this only says which of the things it
// found to use. Neither half has to be set, and an unset half means "whatever
// you would have chosen".
type Choice struct {
	Metrics *Pick `json:"metrics,omitempty"`
	Alerts  *Pick `json:"alerts,omitempty"`
}

// Empty reports a choice that overrides nothing.
func (c Choice) Empty() bool { return c.Metrics == nil && c.Alerts == nil }

// MonitoringStack is what a cluster has available for metrics and alerts.
// Either half can be missing, and a missing half is a fact the agent must
// state rather than paper over.
type MonitoringStack struct {
	ClusterID   int       `json:"clusterId"`
	ClusterName string    `json:"clusterName"`
	Metrics     *Endpoint `json:"metrics,omitempty"`
	Alerts      *Endpoint `json:"alerts,omitempty"`
	// Candidates is everything that looked like a monitoring service,
	// including what did not answer and what was never tried. This is what
	// the cluster's monitoring picker is built from.
	Candidates []Endpoint `json:"candidates,omitempty"`
	// Chosen is the operator's override, echoed back so the UI can tell a
	// pinned endpoint from a guessed one.
	Chosen       Choice    `json:"chosen,omitzero"`
	DiscoveredAt time.Time `json:"discoveredAt"`
	// Partial marks a walk that ran out of budget or was cancelled. A partial
	// answer is never cached: it is the difference between "this cluster has
	// no alert source" and "we did not get far enough to find one", and
	// serving the first when the second is true is the failure this whole
	// service exists to prevent.
	Partial bool     `json:"partial,omitempty"`
	Notes   []string `json:"notes,omitempty"`
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
		parts = append(parts, fmt.Sprintf("metrics=%s (%s/%s)%s", m.Metrics.Flavor,
			m.Metrics.Service.Namespace, m.Metrics.Service.Name, pinnedSuffix(m.Metrics)))
	} else {
		parts = append(parts, "metrics=none")
	}
	if m.HasAlerts() {
		parts = append(parts, fmt.Sprintf("alerts=%s (%s/%s)%s", m.Alerts.Flavor,
			m.Alerts.Service.Namespace, m.Alerts.Service.Name, pinnedSuffix(m.Alerts)))
	} else {
		parts = append(parts, "alerts=none")
	}
	return strings.Join(parts, ", ")
}

func pinnedSuffix(e *Endpoint) string {
	if e != nil && e.Chosen {
		return " [chosen]"
	}
	return ""
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

	// ProbeTimeout caps one HTTP attempt against a candidate. Zero uses
	// DefaultDiscoveryProbeTimeout.
	ProbeTimeout time.Duration

	// Overrides returns the operator's pinned endpoints for a cluster. Nil
	// means nothing is pinned anywhere, which is the state a fresh install
	// is in and the state most installations stay in.
	Overrides func(ctx context.Context, clusterID int) Choice

	// Persist records a completed walk so it survives a restart. Optional;
	// without it discovery is in-memory only, which is what it used to be.
	Persist func(ctx context.Context, stack *MonitoringStack)

	mu    sync.Mutex
	cache map[int]*MonitoringStack

	// flights collapses concurrent misses. The dashboard asks for the stack
	// from four places at once on first paint, and four identical walks is
	// four times the load on the orchestrator for one answer.
	flights singleflight.Group
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
//
// The walk itself is detached from the caller's context and given its own
// budget. A browser tab that navigates away used to cancel discovery
// mid-probe and leave the half-finished result in the cache for the next
// fifteen minutes, which every reader downstream then reported as "no alert
// source in this cluster". The caller can still give up early — it just no
// longer takes the answer down with it.
func (d *Discoverer) Get(ctx context.Context, clusterID int, clusterName string) (*MonitoringStack, error) {
	if m := d.cached(clusterID); m != nil {
		return m, nil
	}
	return d.run(ctx, clusterID, clusterName, false)
}

// Probe re-measures every candidate, including the ones a normal walk skips
// once it has an answer, and replaces the cached stack with the result.
//
// This is what the cluster's monitoring screen calls. Choosing between
// endpoints means seeing all of them, and "never asked" is not a useful thing
// to show somebody who is being asked to pick.
func (d *Discoverer) Probe(ctx context.Context, clusterID int, clusterName string) (*MonitoringStack, error) {
	d.Invalidate(clusterID)
	return d.run(ctx, clusterID, clusterName, true)
}

func (d *Discoverer) run(ctx context.Context, clusterID int, clusterName string, full bool) (*MonitoringStack, error) {
	key := strconv.Itoa(clusterID)
	if full {
		key += ":full"
	}
	ch := d.flights.DoChan(key, func() (any, error) {
		// Another flight may have finished and filled the cache while this
		// one queued.
		if !full {
			if m := d.cached(clusterID); m != nil {
				return m, nil
			}
		}
		bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), DiscoveryBudget)
		defer cancel()

		m, err := d.discover(bg, clusterID, clusterName, full)
		if err != nil {
			return nil, err
		}
		if !m.Partial {
			d.store(clusterID, m)
			// Kept, so a restart does not cost the whole map. Detached from
			// the caller for the same reason the walk is.
			if d.Persist != nil {
				d.Persist(context.WithoutCancel(ctx), m)
			}
		}
		return m, nil
	})

	select {
	case <-ctx.Done():
		// The caller gave up. The walk carries on and warms the cache for
		// whoever asks next.
		return nil, ctx.Err()
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		return res.Val.(*MonitoringStack), nil
	}
}

// cached returns what is known about a cluster, however old.
//
// Age deliberately does not expire it. A stack that re-derives itself under
// whoever asks next cannot be the basis for filtering alerts by cluster —
// the same question gets a different answer depending on when it is asked,
// and a list built from fifty of those never settles. A stored answer stands
// until somebody asks for a new measurement, or until a read against it
// fails. Stale() is for a caller that wants to offer a refresh, not for this
// read path.
func (d *Discoverer) cached(clusterID int) *MonitoringStack {
	d.mu.Lock()
	defer d.mu.Unlock()
	if m, ok := d.cache[clusterID]; ok {
		return m
	}
	return nil
}

// Stale reports whether a cluster's stack is older than the refresh window,
// for a caller deciding whether to offer a re-probe. It never causes one.
func (d *Discoverer) Stale(clusterID int) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	m, ok := d.cache[clusterID]
	return !ok || time.Since(m.DiscoveredAt) > d.ttl
}

// Hydrate loads stored stacks at boot, so the first alert list after a
// restart does not walk every cluster before it can show anything.
func (d *Discoverer) Hydrate(stacks map[int]*MonitoringStack) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	for id, m := range stacks {
		if m != nil {
			d.cache[id] = m
		}
	}
	return len(d.cache)
}

func (d *Discoverer) store(clusterID int, m *MonitoringStack) {
	d.mu.Lock()
	d.cache[clusterID] = m
	d.mu.Unlock()
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

func (d *Discoverer) discover(ctx context.Context, clusterID int, clusterName string, full bool) (*MonitoringStack, error) {
	out := &MonitoringStack{ClusterID: clusterID, ClusterName: clusterName, DiscoveredAt: time.Now()}

	list, err := d.c.ListResources(ctx, ResourceQuery{
		ClusterID: clusterID,
		GVK:       GVKService,
		Filter:    discoveryCEL,
	})
	if err != nil {
		out.Notes = append(out.Notes, "service discovery failed: "+err.Error())
		out.Partial = ctx.Err() != nil
		return out, nil
	}
	cands := classify(list.Objects)
	if len(cands) == 0 {
		out.Notes = append(out.Notes, "no Prometheus, VictoriaMetrics, Alertmanager or vmalert Service found in this cluster")
		return out, nil
	}
	out.Candidates = cands

	metrics, alerts := halves(cands, full)

	// A pinned endpoint is resolved first and on its own. If the operator has
	// said which alert source to use, spending the budget probing seventeen
	// others to arrive at a different one is worse than pointless.
	if d.Overrides != nil {
		out.Chosen = d.Overrides(ctx, clusterID)
	}
	pinnedMetrics := d.resolvePin(ctx, clusterID, out, cands, out.Chosen.Metrics, false)
	pinnedAlerts := d.resolvePin(ctx, clusterID, out, cands, out.Chosen.Alerts, true)

	// A pin settles its half, so the rest of that half is not worth probing
	// — except during a full probe, which is the picker asking. Somebody
	// deciding whether to keep a pin needs to see whether the alternatives
	// answer, and "not probed" beside every one of them is no help at all.
	metrics = without(metrics, pinnedMetrics, cands)
	alerts = without(alerts, pinnedAlerts, cands)
	probeMetricsHalf := pinnedMetrics == nil || full
	probeAlertsHalf := pinnedAlerts == nil || full

	// The two halves are probed at the same time. They used to share one
	// serial queue ordered by name, so on a cluster with a dozen exporters
	// the alert sources were seventeenth and nineteenth in line and the
	// budget was gone before either was reached.
	var wg sync.WaitGroup
	if probeMetricsHalf {
		wg.Add(1)
		go func() { defer wg.Done(); d.probeHalf(ctx, clusterID, cands, metrics, full) }()
	}
	if probeAlertsHalf {
		wg.Add(1)
		go func() { defer wg.Done(); d.probeHalf(ctx, clusterID, cands, alerts, full) }()
	}
	wg.Wait()

	out.Metrics = pinnedMetrics
	if out.Metrics == nil {
		out.Metrics = firstReachable(cands, metrics)
	}
	out.Alerts = pinnedAlerts
	if out.Alerts == nil {
		out.Alerts = firstReachable(cands, alerts)
	}

	if out.Metrics == nil {
		out.Notes = append(out.Notes, "a metrics Service was found but none answered a query; coverage is unknown, not healthy")
	}
	if out.Alerts == nil {
		out.Notes = append(out.Notes, "no alert source answered; firing alerts cannot be listed for this cluster")
	}
	if ctx.Err() != nil {
		out.Partial = true
		out.Notes = append(out.Notes,
			"discovery ran out of time before it finished; this answer is incomplete and will be retried rather than cached")
	}
	return out, nil
}

// resolvePin finds the candidate the operator pinned and measures it.
//
// A pinned endpoint is used even when it does not answer. Being told the
// Alertmanager you chose is down is the truth; being quietly moved onto a
// different one is how somebody ends up reading another cluster's alerts and
// believing they are their own.
func (d *Discoverer) resolvePin(ctx context.Context, clusterID int, out *MonitoringStack,
	cands []Endpoint, pick *Pick, wantAlerts bool,
) *Endpoint {
	if pick == nil || !pick.Valid() {
		return nil
	}
	half := "metrics"
	if wantAlerts {
		half = "alert source"
	}
	for i := range cands {
		e := &cands[i]
		if !pick.matches(e) || e.IsAlertSource() != wantAlerts {
			continue
		}
		e.Chosen = true
		d.probe(ctx, clusterID, e)
		if !e.Reachable {
			out.Notes = append(out.Notes, fmt.Sprintf(
				"the chosen %s %s did not answer; it is still the one in use, so this cluster reports nothing rather than reporting somebody else's data",
				half, pick))
		}
		return e
	}
	out.Notes = append(out.Notes, fmt.Sprintf(
		"the chosen %s %s is no longer a Service in this cluster; falling back to discovery", half, pick))
	return nil
}

// without drops the pinned candidate from a half's queue, so a full probe
// measures the alternatives without measuring the pin twice.
func without(idx []int, pinned *Endpoint, cands []Endpoint) []int {
	if pinned == nil {
		return idx
	}
	out := idx[:0:0]
	for _, i := range idx {
		if &cands[i] != pinned {
			out = append(out, i)
		}
	}
	return out
}

// halves splits the candidate indexes into the metrics queue and the alert
// queue, dropping scrape targets unless every candidate was asked for.
func halves(cands []Endpoint, full bool) (metrics, alerts []int) {
	for i := range cands {
		if cands[i].ScrapeTarget && !full {
			continue
		}
		if cands[i].IsAlertSource() {
			alerts = append(alerts, i)
		} else {
			metrics = append(metrics, i)
		}
	}
	return metrics, alerts
}

// probeHalf measures one half's candidates in rank order, several at a time,
// and stops launching new ones once a better-ranked candidate has answered.
func (d *Discoverer) probeHalf(ctx context.Context, clusterID int, cands []Endpoint, idx []int, full bool) {
	if len(idx) == 0 {
		return
	}
	var (
		mu   sync.Mutex
		best = -1 // rank position of the best candidate that has answered
	)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(probeParallel)

	for pos, i := range idx {
		if gctx.Err() != nil {
			break
		}
		g.Go(func() error {
			if gctx.Err() != nil {
				return nil
			}
			if !full {
				mu.Lock()
				settled := best >= 0 && best < pos
				mu.Unlock()
				if settled {
					return nil
				}
			}
			if d.probe(gctx, clusterID, &cands[i]) {
				mu.Lock()
				if best < 0 || pos < best {
					best = pos
				}
				mu.Unlock()
			}
			return nil
		})
	}
	_ = g.Wait()
}

// firstReachable returns the best-ranked candidate in this half that answered.
func firstReachable(cands []Endpoint, idx []int) *Endpoint {
	for _, i := range idx {
		if cands[i].Reachable {
			return &cands[i]
		}
	}
	return nil
}

// probe measures one candidate, whichever half it belongs to.
func (d *Discoverer) probe(ctx context.Context, clusterID int, e *Endpoint) bool {
	ctx, cancel := context.WithTimeout(ctx, candidateBudget)
	defer cancel()

	e.Probed = true
	if e.IsAlertSource() {
		return d.probeAlerts(ctx, clusterID, e)
	}
	return d.probeMetrics(ctx, clusterID, e)
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
			e.Detail = probeDetail(err)
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
		e.Detail = "answered, but not with a Prometheus query result"
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
		e.Detail = probeDetail(err)
		return false
	}
	e.Reachable = true
	e.Detail = ""
	return true
}

// wellKnownPorts are the ports each flavor serves its API on.
//
// The Service object is supposed to carry its own ports, and on some Devtron
// versions it does — the filtered resource list returns whole objects with a
// spec. On others the same call returns table rows with no spec at all, and
// every candidate arrives with no ports whatsoever. That is not a cosmetic
// difference: without a port the Kubernetes service proxy only resolves for
// a Service with exactly one port, so a two-port vmsingle answers 503 and
// there is nothing left to retry with. Measured on a 52-cluster install,
// that was every monitoring service on every cluster.
//
// So when the object did not say, these are tried. They are the published
// defaults for each flavor, not guesses about a particular install.
var wellKnownPorts = map[Flavor][]string{
	FlavorVictoriaMetrics: {"8429", "8428", "8481", "8427"},
	FlavorPrometheus:      {"9090", "80"},
	FlavorAlertmanager:    {"9093"},
	FlavorVMAlert:         {"8080"},
	FlavorThanos:          {"10902", "9090"},
	FlavorMimir:           {"8080", "9009"},
}

// portsFor is what to try after the portless target, declared ports first.
func portsFor(e *Endpoint) []string {
	if len(e.Ports) > 0 {
		return e.Ports
	}
	return wellKnownPorts[e.Flavor]
}

// tryGet attempts the portless proxy target first, because naming a port
// requires the token to hold "*" on resource names. If that fails, it retries
// with each port the Service declared — or, when it declared none, with the
// flavor's published defaults.
func (d *Discoverer) tryGet(ctx context.Context, clusterID int, e *Endpoint, path string, q url.Values) ([]byte, error) {
	svc := e.Service
	svc.Port = ""

	// Every attempt's failure is kept. Returning only the first one meant a
	// fast rejection of the portless target hid what happened on the ports —
	// so a probe that actually timed out reported somebody else's HTTP 503,
	// and the operator went looking for a service that was never the problem.
	var failures []attemptFailure

	body, err := d.attempt(ctx, clusterID, svc, path, q)
	if err == nil {
		e.Service.Port = ""
		return body, nil
	}
	failures = append(failures, attemptFailure{port: "", err: err})

	ports := portsFor(e)
	if len(ports) > maxPortRetries {
		ports = ports[:maxPortRetries]
	}
	for _, p := range ports {
		if ctx.Err() != nil {
			break
		}
		svc.Port = p
		body, err := d.attempt(ctx, clusterID, svc, path, q)
		if err == nil {
			e.Service.Port = p
			return body, nil
		}
		failures = append(failures, attemptFailure{port: p, err: err})
	}
	return nil, &probeFailure{attempts: failures}
}

// attemptFailure is one proxy target that did not answer.
type attemptFailure struct {
	port string // "" for the portless target
	err  error
}

// probeFailure is every target tried for one candidate, so the reason on
// screen covers all of them rather than whichever failed first.
type probeFailure struct{ attempts []attemptFailure }

func (p *probeFailure) Error() string {
	if len(p.attempts) == 0 {
		return "nothing was tried"
	}
	// All the same reason: say it once rather than three times.
	first := probeDetail(p.attempts[0].err)
	same := true
	for _, a := range p.attempts[1:] {
		if probeDetail(a.err) != first {
			same = false
			break
		}
	}
	if same {
		return first
	}
	parts := make([]string, 0, len(p.attempts))
	for _, a := range p.attempts {
		where := "no port"
		if a.port != "" {
			where = ":" + a.port
		}
		parts = append(parts, where+" → "+probeDetail(a.err))
	}
	return strings.Join(parts, "; ")
}

func (p *probeFailure) Unwrap() error {
	if len(p.attempts) == 0 {
		return nil
	}
	return p.attempts[0].err
}

// probeDetail says why a probe failed, reason first.
func probeDetail(err error) string {
	var de *Error
	if errors.As(err, &de) {
		return de.Reason()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out — the orchestrator did not answer within the probe deadline"
	}
	if errors.Is(err, context.Canceled) {
		return "not finished — discovery ran out of time before this one answered"
	}
	return err.Error()
}

func (d *Discoverer) attempt(ctx context.Context, clusterID int, svc ServiceRef, path string, q url.Values) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, d.probeTimeout())
	defer cancel()
	return d.c.ServiceProxyGet(ctx, ScopeCluster, clusterID, svc, path, q)
}

func (d *Discoverer) probeTimeout() time.Duration {
	if d.ProbeTimeout > 0 {
		return d.ProbeTimeout
	}
	return DefaultDiscoveryProbeTimeout
}

// classify turns Service objects into ranked endpoint candidates. Ordering
// matters: a purpose-built query service beats an operator's headless
// "-operated" Service, which often has no usable HTTP route, and both beat an
// exporter that only happens to have "prometheus" in its name.
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
			Flavor:       f,
			Service:      ServiceRef{Namespace: ns, Name: name},
			Ports:        portsOf(o),
			ScrapeTarget: isScrapeTarget(name),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ScrapeTarget != out[j].ScrapeTarget {
			return !out[i].ScrapeTarget
		}
		if ri, rj := rank(out[i]), rank(out[j]); ri != rj {
			return ri < rj
		}
		if out[i].Service.Namespace != out[j].Service.Namespace {
			return out[i].Service.Namespace < out[j].Service.Namespace
		}
		return out[i].Service.Name < out[j].Service.Name
	})
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

// scrapeTargetNames are the Services that match the discovery filter by name
// but serve /metrics for something else to scrape rather than a query API of
// their own.
//
// On the cluster this was written against there were twelve of them and two
// real alert sources, and because they were probed first the two real ones
// were never reached. They stay in the candidate list — the operator can pin
// one if this list is wrong about their install — but they are not probed
// unless every candidate was asked for.
var scrapeTargetNames = []string{
	"node-exporter", "kube-state-metrics", "grafana", "pushgateway",
	"kubelet", "kube-etcd", "kube-scheduler", "kube-controller-manager",
	"kube-proxy", "core-dns", "coredns", "vmagent", "exporter", "operator",
	"blackbox", "statsd",
}

func isScrapeTarget(name string) bool {
	n := strings.ToLower(name)
	for _, s := range scrapeTargetNames {
		if strings.Contains(n, s) {
			return true
		}
	}
	return false
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
