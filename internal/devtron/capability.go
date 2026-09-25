package devtron

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
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
	// Kinds records which kinds answered. Only populated when the cluster is
	// reachable; probing kinds on a dead cluster is 30 seconds of nothing.
	Kinds []KindAccess `json:"kinds,omitempty"`
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
}

// Probe bounds, overridable through config for installs where the
// orchestrator is slower or faster than the one these were measured against.
const (
	DefaultProbeTimeout     = 20 * time.Second
	DefaultProbeConcurrency = 4
)

// NewProber builds a prober with sensible bounds.
func NewProber(c *Client) *Prober {
	return &Prober{c: c, Timeout: DefaultProbeTimeout, Concurrency: DefaultProbeConcurrency}
}

// Probe measures one cluster: can we reach it, and which kinds answer.
//
// Every kind is asked at once and the verdict comes from all of them
// together. It used to hinge on a single cluster-wide Pod list, which
// answered two very different questions with the same empty array: "this
// cluster has nothing running" and "this token may not list across all
// namespaces". Clusters in the second case were reported empty and hidden
// from the picker.
//
// namespaces are the ones Devtron says this cluster has, used only when the
// cluster-wide read came back with nothing at all.
func (p *Prober) Probe(ctx context.Context, clusterID int, clusterName string, namespaces []string) Capability {
	measured := Capability{ClusterID: clusterID, ClusterName: clusterName, ProbedAt: time.Now().UTC()}
	started := time.Now()

	// One cheap question first. Most clusters on a large install cannot be
	// reached at all, and they say so on the first call — spending five on
	// each of them tripled the sweep and put enough load on the orchestrator
	// to make healthy clusters time out.
	first := p.probeKind(ctx, clusterID, GVKPod, "")
	measured.LatencyMs = time.Since(started).Milliseconds()
	if first.reach == ReachUnreachable || first.reach == ReachError {
		measured.Kinds = []KindAccess{first.access}
		measured.Reach, measured.Detail = reachFrom([]kindProbe{first})
		return measured
	}

	// It answered something — even a refusal is an answer. Now ask the rest,
	// because one kind cannot tell "nothing is running" from "this token
	// cannot list across all namespaces", and being denied Pods says nothing
	// about Services or Nodes.
	probes := p.probeRest(ctx, clusterID, "", first)
	measured.Kinds = accessOf(probes)
	measured.Reach, measured.Detail = reachFrom(probes)

	// Nothing came back, but the cluster answered. Before calling it empty,
	// look where Devtron says this cluster actually has workloads: a token
	// scoped to environments rather than to whole clusters reads nothing
	// across all namespaces and plenty inside one.
	if measured.Reach == ReachEmpty {
		for _, ns := range limitNamespaces(namespaces, maxProbeNamespaces) {
			scoped := p.probeKinds(ctx, clusterID, ns)
			if r, _ := reachFrom(scoped); r == ReachUsable {
				measured.Kinds = accessOf(scoped)
				measured.Reach = ReachUsable
				measured.Detail = "readable in namespace " + ns + ", not across all namespaces"
				break
			}
		}
	}
	return measured
}

// kindProbe is one kind's result, with the classification kept beside it so
// the verdict does not have to be re-derived by parsing a message.
type kindProbe struct {
	access KindAccess
	reach  Reach
}

// probeKind asks one kind, in one namespace ("" for every namespace).
func (p *Prober) probeKind(ctx context.Context, clusterID int, gvk GVK, namespace string) kindProbe {
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
		return kindProbe{access: access, reach: reach}
	}
	access.Allowed = true
	access.Count = list.Len()
	return kindProbe{access: access, reach: ReachUsable}
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

// ProbeAll sweeps every cluster, bounded by Concurrency.
//
// onResult is called with each cluster's verdict the moment it is known, on
// the goroutine that measured it, so a caller can publish answers while the
// rest of the sweep is still running. It may be nil.
func (p *Prober) ProbeAll(ctx context.Context, clusters []Cluster, onResult func(Capability)) []Capability {
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

	out := make([]Capability, len(clusters))
	sem := make(chan struct{}, max(1, p.Concurrency))
	var wg sync.WaitGroup
	for i, cl := range clusters {
		wg.Add(1)
		go func(i int, cl Cluster) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			c := p.Probe(ctx, cl.ID, cl.ClusterName, byCluster[cl.ID])
			out[i] = c
			if onResult != nil {
				onResult(c)
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
