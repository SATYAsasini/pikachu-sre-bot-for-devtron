package devtron

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Reach is how usable a cluster actually is, as opposed to how it is listed.
//
// Devtron will happily list clusters it cannot reach. On a live install, 14
// of 23 listed clusters hung the orchestrator, 3 returned 500, and only 2
// served workloads — so "listed" and "investigable" are entirely different
// questions and the agent has to know which it is looking at.
type Reach string

const (
	// ReachUsable means the cluster answered and returned objects.
	ReachUsable Reach = "usable"
	// ReachEmpty means it answered, but nothing came back. Either genuinely empty
	// or filtered away by RBAC — indistinguishable from here, and that
	// ambiguity must be reported rather than resolved by guessing.
	ReachEmpty Reach = "empty"
	// ReachForbidden means Devtron refused. A permissions problem, not an outage.
	ReachForbidden Reach = "forbidden"
	// ReachError means the orchestrator failed trying to serve it.
	ReachError Reach = "error"
	// ReachUnreachable means the request timed out. The orchestrator cannot talk
	// to the cluster, which is the most common state on a large install.
	ReachUnreachable Reach = "unreachable"
	// ReachUnknown means not probed yet.
	ReachUnknown Reach = "unknown"
)

// Investigable reports whether a run against this cluster can do useful work.
// Investigable reports whether a run pointed at this cluster can succeed.
//
// Only `usable` qualifies. `empty` used to count, on the reasoning that a
// genuinely empty cluster is a legitimate thing to investigate — but an empty
// probe and a token that cannot see into the cluster are indistinguishable
// from here, and in practice every `empty` cluster on a real installation is
// the second case. Offering them filled the picker with three choices that
// produce a run concluding nothing, which is worse than not offering them.
func (r Reach) Investigable() bool { return r == ReachUsable }

// Why explains the reach in one sentence, for the UI and for the model.
func (r Reach) Why() string {
	switch r {
	case ReachUsable:
		return "Reachable, and returning workloads."
	case ReachEmpty:
		return "Reachable, but nothing came back. Either the cluster is empty or this token cannot see into it — those look identical from here."
	case ReachForbidden:
		return "Devtron refused the read. This is a permissions problem, not an outage."
	case ReachError:
		return "The orchestrator failed while serving this cluster."
	case ReachUnreachable:
		return "The orchestrator timed out talking to this cluster. Nothing can be read from it."
	}
	return "Not probed yet."
}

// ProbeStep is one question put to a cluster and what came back.
//
// The verdict alone is not enough to act on. "usable, readable in the
// namespaces Devtron maps to this cluster" is true, but it does not say
// whether the cluster-wide read was refused or merely returned nothing —
// and those call for completely different fixes. The steps are kept so the
// answer to "what did you ask, and what did it say" does not have to be
// reconstructed from source.
type ProbeStep struct {
	// Ask is the question in words: "list Namespaces across the cluster".
	Ask string `json:"ask"`
	// Path is the orchestrator endpoint, so it can be repeated by hand.
	Path string `json:"path"`
	// Outcome is the short verdict for this step alone.
	Outcome   string `json:"outcome"`
	Detail    string `json:"detail,omitempty"`
	LatencyMs int64  `json:"latencyMs"`
}

// resourceListPath is the one endpoint every capability probe uses.
const resourceListPath = "POST /orchestrator/k8s/resource/list"

// KindAccess is what one Kubernetes kind yielded.
type KindAccess struct {
	Kind    string `json:"kind"`
	Allowed bool   `json:"allowed"`
	Count   int    `json:"count"`
	Detail  string `json:"detail,omitempty"`
}

// Capability is what a cluster will actually let this token do.
type Capability struct {
	ClusterID   int       `json:"clusterId"`
	ClusterName string    `json:"clusterName"`
	Reach       Reach     `json:"reach"`
	Detail      string    `json:"detail,omitempty"`
	LatencyMs   int64     `json:"latencyMs"`
	ProbedAt    time.Time `json:"probedAt"`
	// Kinds records which kinds answered. Only populated when something
	// asked for them; the cheap reach check does not.
	Kinds []KindAccess `json:"kinds,omitempty"`
	// Namespaces is what this token can see in the cluster, which is the
	// answer the cheap check produces along the way. Empty means the
	// question was never reached, not that there are none.
	Namespaces []string `json:"namespaces,omitempty"`
	// Steps is what was asked to reach this verdict, in order.
	Steps []ProbeStep `json:"steps,omitempty"`
	// FromDevtron marks a verdict taken from Devtron's own connection status
	// rather than measured. Devtron already knows it cannot reach a cluster
	// and says so in the list; spending twenty seconds proving it again is
	// twenty seconds nobody gets back.
	FromDevtron bool `json:"fromDevtron,omitempty"`
}

// AllowsKind reports whether a kind is worth attempting.
func (c *Capability) AllowsKind(kind string) bool {
	if c == nil {
		return true // unprobed: let the call happen and report what it says
	}
	if !c.Reach.Investigable() {
		return false
	}
	for _, k := range c.Kinds {
		if k.Kind == kind {
			return k.Allowed
		}
	}
	return true
}

// Summary is a line for the fact pack.
func (c *Capability) Summary() string {
	if c == nil {
		return "cluster capability was not probed"
	}
	s := fmt.Sprintf("%s: %s (%s)", c.ClusterName, c.Reach, c.Reach.Why())
	var denied []string
	for _, k := range c.Kinds {
		if !k.Allowed {
			denied = append(denied, k.Kind)
		}
	}
	if len(denied) > 0 {
		s += " Cannot read: " + joinComma(denied) + "."
	}
	return s
}

// probeKinds are the kinds the agent actually reaches for, so they are the
// only ones worth the round trip.
//
// All of them decide reach, not just the first. A cluster with no pods but
// plenty of nodes is alive, and judging it on Pod alone reported it empty.
var probeKinds = []GVK{GVKPod, GVKEvent, GVKService, GVKDeployment, GVKNode}

// maxProbeNamespaces caps the namespace fallback. A cluster with forty
// environments is not worth forty extra rounds to establish one fact.
const maxProbeNamespaces = 3

// Prober measures what each cluster will serve.
type Prober struct {
	c *Client
	// Timeout bounds one probe.
	//
	// It was 8s, which is not long enough. A cluster behind a slow link, or
	// one the orchestrator has to open a fresh connection to, needs more than
	// that — and every one it cut short was reported unreachable, which is
	// the most damaging wrong answer this code can give.
	Timeout time.Duration
	// Concurrency bounds simultaneous probes.
	//
	// Low on purpose. Raising it to make the sweep finish sooner was a
	// mistake: the orchestrator proxies every one of these to a different
	// cluster, and piling requests on it made clusters that answer in 300ms
	// miss an 8s deadline. A false "unreachable" is worse than a slow sweep,
	// and results are published as they land now, so the wall clock of the
	// whole sweep matters much less than it did.
	Concurrency int
	// MaxInFlight bounds concurrent requests across the whole sweep, which is
	// the thing the orchestrator actually feels.
	//
	// Capping clusters instead was too blunt in both directions. A cluster
	// that cannot be reached costs one request, so four at a time left an
	// install with forty dead clusters crawling through them in tens of
	// waves; a cluster that answers costs five, so the same four could put
	// twenty requests in flight and cause the very timeouts the cap was
	// there to prevent. Counting requests gets both right: many dead
	// clusters proceed together, and live ones throttle themselves.
	MaxInFlight int

	// gate is the request semaphore for the sweep in progress. Nil means
	// unbounded, which is what a direct Probe call outside a sweep gets.
	gate chan struct{}
}

// Probe bounds, overridable through config for installs where the
// orchestrator is slower or faster than the one these were measured against.
const (
	DefaultProbeTimeout     = 20 * time.Second
	DefaultProbeConcurrency = 16
	DefaultProbeInFlight    = 12
)

// NewProber builds a prober with sensible bounds.
func NewProber(c *Client) *Prober {
	return &Prober{
		c: c, Timeout: DefaultProbeTimeout,
		Concurrency: DefaultProbeConcurrency, MaxInFlight: DefaultProbeInFlight,
	}
}

// acquire takes a slot in the request semaphore, or reports that the caller
// gave up waiting for one.
func (p *Prober) acquire(ctx context.Context) (func(), bool) {
	if p.gate == nil {
		return func() {}, true
	}
	select {
	case p.gate <- struct{}{}:
		return func() { <-p.gate }, true
	case <-ctx.Done():
		return func() {}, false
	}
}

// Probe answers one question as cheaply as it can: will this cluster serve
// this token, and what can it see?
//
// One request, not five. Listing Namespaces answers all of it at once —
// whether the cluster responds, whether the token is allowed, and which
// namespaces are visible — and the namespace list is a fact worth having
// rather than a by-product. What the agent can read *inside* a namespace is
// a different question, asked on the cluster's own page by Kinds, not as a
// gate on whether the cluster may be offered at all.
//
// fallback are the namespaces Devtron says this cluster has, used when the
// cluster-scoped Namespace list is refused — which is the ordinary state of
// a token scoped to environments rather than to whole clusters.
func (p *Prober) Probe(ctx context.Context, clusterID int, clusterName string, fallback []string) Capability {
	measured := Capability{ClusterID: clusterID, ClusterName: clusterName, ProbedAt: time.Now().UTC()}
	started := time.Now()

	list := p.probeKind(ctx, clusterID, GVKNamespace, "")
	measured.LatencyMs = time.Since(started).Milliseconds()
	measured.Steps = append(measured.Steps, list.step(""))

	// It did not answer at all. Nothing else is worth asking.
	if list.reach == ReachUnreachable || list.reach == ReachError {
		measured.Reach, measured.Detail = list.reach, list.access.Detail
		return measured
	}

	if list.access.Allowed && list.access.Count > 0 {
		measured.Reach = ReachUsable
		measured.Namespaces = list.namespaces
		return measured
	}

	// Refused, or allowed but filtered down to nothing — indistinguishable,
	// and in both cases the cluster-wide question was the wrong one. If
	// Devtron knows namespaces for this cluster, ask inside one of them
	// instead: that is the read the agent would actually perform.
	ns := limitNamespaces(fallback, maxProbeNamespaces)
	if len(ns) == 0 {
		measured.Steps = append(measured.Steps, ProbeStep{
			Ask:     "look inside a namespace instead",
			Outcome: "skipped",
			Detail:  "Devtron maps no environments to this cluster, so there was nowhere narrower to ask",
		})
		if list.reach == ReachForbidden {
			measured.Reach, measured.Detail = ReachForbidden, list.access.Detail
		} else {
			measured.Reach = ReachEmpty
		}
		return measured
	}

	// Ask in each of them until one yields something. Answering is not
	// enough on its own: a cluster where every read succeeds and returns
	// nothing produces an investigation that concludes nothing, which is the
	// exact outcome this whole measurement exists to keep out of the picker.
	// One empty namespace is also not evidence the cluster is idle, so the
	// others are tried before saying so.
	var refused bool
	for _, n := range ns {
		scoped := p.probeKind(ctx, clusterID, GVKPod, n)
		measured.Steps = append(measured.Steps, scoped.step(n))

		if scoped.access.Allowed && scoped.access.Count > 0 {
			measured.Reach = ReachUsable
			measured.Namespaces = limitNamespaces(fallback, maxNamespacesShown)
			measured.Detail = "readable in " + n + ", not across all namespaces"
			measured.LatencyMs = time.Since(started).Milliseconds()
			return measured
		}
		switch {
		case scoped.access.Allowed:
			// Answered, empty. Keep looking.
		case scoped.reach == ReachForbidden:
			refused = true
		default:
			// Could not be reached at all; that outranks anything else.
			measured.Reach, measured.Detail = scoped.reach, scoped.access.Detail
			measured.LatencyMs = time.Since(started).Milliseconds()
			return measured
		}
	}

	measured.LatencyMs = time.Since(started).Milliseconds()
	measured.Namespaces = limitNamespaces(fallback, maxNamespacesShown)
	if refused {
		measured.Reach = ReachForbidden
		measured.Detail = "the token was refused in every namespace Devtron maps to this cluster"
		return measured
	}
	measured.Reach = ReachEmpty
	measured.Detail = "every read answered and every one was empty, cluster-wide and in " +
		strings.Join(ns, ", ")
	return measured
}

// Kinds measures what the token may read in one cluster, optionally inside
// one namespace. This is the deep question, asked from the cluster's own
// page rather than as a gate on the picker.
func (p *Prober) Kinds(ctx context.Context, clusterID int, namespace string) []KindAccess {
	return accessOf(p.probeKinds(ctx, clusterID, namespace))
}

// kindProbe is one kind's result, with the classification kept beside it so
// the verdict does not have to be re-derived by parsing a message.
type kindProbe struct {
	access KindAccess
	reach  Reach
	// namespaces is filled only when the kind was Namespace, because that is
	// the one list whose contents are themselves the answer.
	namespaces []string
	latencyMs  int64
}

// step renders this probe as a line in the record.
func (k kindProbe) step(namespace string) ProbeStep {
	where := "across the cluster"
	if namespace != "" {
		where = "in namespace " + namespace
	}
	st := ProbeStep{
		Ask:       "list " + k.access.Kind + "s " + where,
		Path:      resourceListPath,
		LatencyMs: k.latencyMs,
		Detail:    k.access.Detail,
	}
	switch {
	case k.access.Allowed && k.access.Count > 0:
		st.Outcome = fmt.Sprintf("answered with %d", k.access.Count)
	case k.access.Allowed:
		st.Outcome = "answered with nothing"
	case k.reach == ReachForbidden:
		st.Outcome = "refused"
	case k.reach == ReachUnreachable:
		st.Outcome = "no answer before the deadline"
	default:
		st.Outcome = "failed"
	}
	return st
}

// maxNamespacesShown caps what is carried on a capability row. A cluster with
// four hundred namespaces does not need all of them on a setup screen.
const maxNamespacesShown = 50

// probeKind asks one kind, in one namespace ("" for every namespace).
func (p *Prober) probeKind(ctx context.Context, clusterID int, gvk GVK, namespace string) kindProbe {
	started := time.Now()
	release, ok := p.acquire(ctx)
	if !ok {
		access := KindAccess{Kind: gvk.Kind, Detail: string(ReachUnknown) + ": not reached before the sweep ended"}
		return kindProbe{access: access, reach: ReachUnknown, latencyMs: time.Since(started).Milliseconds()}
	}
	defer release()

	kctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()

	list, err := p.c.ListResources(kctx, ResourceQuery{
		ClusterID: clusterID, GVK: gvk, Namespace: namespace,
	})
	access := KindAccess{Kind: gvk.Kind}
	if err != nil {
		reach, detail := classifyProbeError(kctx, err)
		access.Allowed = false
		access.Detail = string(reach) + ": " + detail
		return kindProbe{access: access, reach: reach, latencyMs: time.Since(started).Milliseconds()}
	}
	access.Allowed = true
	access.Count = list.Len()
	out := kindProbe{access: access, reach: ReachUsable, latencyMs: time.Since(started).Milliseconds()}
	if gvk.Kind == GVKNamespace.Kind {
		out.namespaces = namesOf(list.Objects, maxNamespacesShown)
	}
	return out
}

// namesOf pulls metadata.name out of a resource list.
func namesOf(objects []map[string]any, limit int) []string {
	out := make([]string, 0, min(len(objects), limit))
	for _, o := range objects {
		md, _ := o["metadata"].(map[string]any)
		if md == nil {
			continue
		}
		if n, _ := md["name"].(string); n != "" {
			out = append(out, n)
			if len(out) == limit {
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// probeRest asks every kind except the one already measured, keeping the
// declared order so a sweep is reproducible.
func (p *Prober) probeRest(ctx context.Context, clusterID int, namespace string, done kindProbe) []kindProbe {
	out := make([]kindProbe, len(probeKinds))
	var wg sync.WaitGroup
	for i, gvk := range probeKinds {
		if gvk.Kind == done.access.Kind {
			out[i] = done
			continue
		}
		wg.Add(1)
		go func(i int, gvk GVK) {
			defer wg.Done()
			out[i] = p.probeKind(ctx, clusterID, gvk, namespace)
		}(i, gvk)
	}
	wg.Wait()
	return out
}

func (p *Prober) probeKinds(ctx context.Context, clusterID int, namespace string) []kindProbe {
	out := make([]kindProbe, len(probeKinds))
	var wg sync.WaitGroup
	for i, gvk := range probeKinds {
		wg.Add(1)
		go func(i int, gvk GVK) {
			defer wg.Done()
			out[i] = p.probeKind(ctx, clusterID, gvk, namespace)
		}(i, gvk)
	}
	wg.Wait()
	return out
}

func accessOf(probes []kindProbe) []KindAccess {
	out := make([]KindAccess, 0, len(probes))
	for _, k := range probes {
		out = append(out, k.access)
	}
	return out
}

// reachFrom turns per-kind results into one verdict.
//
// Anything returning objects settles it: the cluster is readable, whatever
// the other kinds did. Empty is only reported when every kind answered and
// every one of them was empty — a single kind's silence is not evidence that
// a cluster is idle.
func reachFrom(probes []kindProbe) (Reach, string) {
	var answered, objects, forbidden, timedOut, errored int
	var firstErr string
	for _, k := range probes {
		if k.access.Allowed {
			answered++
			objects += k.access.Count
			continue
		}
		if firstErr == "" {
			firstErr = k.access.Detail
		}
		switch k.reach {
		case ReachForbidden:
			forbidden++
		case ReachUnreachable:
			timedOut++
		default:
			errored++
		}
	}

	switch {
	case objects > 0:
		return ReachUsable, ""
	case answered > 0:
		// Every kind that answered came back with nothing. Genuinely idle, or
		// filtered down to nothing — indistinguishable from here, which is
		// what ReachEmpty means.
		return ReachEmpty, ""
	case forbidden >= timedOut && forbidden >= errored && forbidden > 0:
		return ReachForbidden, firstErr
	case timedOut >= errored && timedOut > 0:
		return ReachUnreachable, "timed out; the orchestrator could not reach the cluster"
	case errored > 0:
		return ReachError, firstErr
	}
	return ReachUnknown, "nothing was probed"
}

// limitNamespaces deduplicates and caps the fallback list, keeping order so a
// sweep is reproducible.
func limitNamespaces(in []string, max int) []string {
	seen := map[string]bool{}
	out := make([]string, 0, max)
	for _, n := range in {
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
		if len(out) == max {
			break
		}
	}
	return out
}

// SweepOptions configure one pass over every cluster.
type SweepOptions struct {
	// OnResult is called with each cluster's verdict the moment it is known,
	// on the goroutine that produced it, so a caller can publish answers
	// while the rest of the sweep is still running.
	OnResult func(Capability)
	// Force probes these clusters even when Devtron says it cannot connect
	// to them. Devtron's status can be stale, and an operator who knows
	// better should be able to say so.
	Force map[int]bool
}

// FromDevtronStatus turns Devtron's own verdict on a cluster into a
// capability, without spending a request on it.
//
// Devtron records why it cannot reach a cluster and hands that back in the
// cluster list — "dial tcp 34.0.2.215:16443: i/o timeout", "connection not
// setup for isolated clusters". Measured against a live install, that
// agreed with the probe on all 25 clusters and disagreed on none, while the
// probe spent 347 seconds establishing it. Its sentence is also more
// specific than anything a timeout of ours can say.
func FromDevtronStatus(c Cluster) Capability {
	return Capability{
		ClusterID: c.ID, ClusterName: c.ClusterName,
		Reach: ReachUnreachable, Detail: c.ErrorInCx,
		ProbedAt: time.Now().UTC(), FromDevtron: true,
		Steps: []ProbeStep{{
			Ask:     "read Devtron's own connection status for this cluster",
			Path:    "GET /orchestrator/cluster/autocomplete",
			Outcome: "Devtron reports it cannot connect",
			Detail:  c.ErrorInCx,
		}},
	}
}

// ProbeAll sweeps every cluster, bounded by MaxInFlight.
func (p *Prober) ProbeAll(ctx context.Context, clusters []Cluster, opts SweepOptions) []Capability {
	// Where each cluster keeps its workloads, fetched once for the whole
	// sweep. Only used for the clusters whose cluster-wide read came back
	// with nothing; a token scoped to environments has no other way to prove
	// it can read anything at all.
	byCluster := map[int][]string{}
	if envs, err := p.c.Environments(ctx); err == nil {
		for _, e := range envs {
			if e.Namespace != "" {
				byCluster[e.ClusterID] = append(byCluster[e.ClusterID], e.Namespace)
			}
		}
	}

	// One semaphore for the whole sweep, counting requests rather than
	// clusters. Rebuilt per sweep so a retuned bound takes effect.
	p.gate = make(chan struct{}, max(1, p.MaxInFlight))

	out := make([]Capability, len(clusters))
	sem := make(chan struct{}, max(1, p.Concurrency))
	var wg sync.WaitGroup
	for i, cl := range clusters {
		wg.Add(1)
		go func(i int, cl Cluster) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			// Devtron has already said it cannot connect. Believe it, unless
			// somebody asked us not to.
			var c Capability
			if cl.ErrorInCx != "" && !opts.Force[cl.ID] {
				c = FromDevtronStatus(cl)
			} else {
				c = p.Probe(ctx, cl.ID, cl.ClusterName, byCluster[cl.ID])
			}
			out[i] = c
			if opts.OnResult != nil {
				opts.OnResult(c)
			}
		}(i, cl)
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Reach != out[j].Reach {
			return reachRank(out[i].Reach) < reachRank(out[j].Reach)
		}
		return out[i].ClusterName < out[j].ClusterName
	})
	return out
}

func reachRank(r Reach) int {
	switch r {
	case ReachUsable:
		return 0
	case ReachEmpty:
		return 1
	case ReachForbidden:
		return 2
	case ReachError:
		return 3
	case ReachUnreachable:
		return 4
	}
	return 5
}

// classifyProbeError separates "you may not" from "it did not answer", which
// call for completely different responses from an operator.
func classifyProbeError(ctx context.Context, err error) (Reach, string) {
	var de *Error
	if errors.As(err, &de) {
		switch {
		case de.Unauthorized():
			return ReachForbidden, de.Body
		case de.Status >= 500:
			return ReachError, fmt.Sprintf("orchestrator returned HTTP %d", de.Status)
		case de.Status == http.StatusNotFound:
			return ReachError, "the orchestrator does not know this cluster"
		}
		return ReachError, de.Error()
	}
	if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) {
		return ReachUnreachable, "timed out; the orchestrator could not reach the cluster"
	}
	return ReachError, err.Error()
}

func joinComma(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ", "
		}
		out += v
	}
	return out
}
