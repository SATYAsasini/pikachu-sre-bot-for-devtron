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
var probeKinds = []GVK{GVKPod, GVKEvent, GVKService, GVKDeployment, GVKNode}

// Prober measures what each cluster will serve.
type Prober struct {
	c *Client
	// Timeout bounds one probe. Short on purpose: an unreachable cluster
	// hangs the orchestrator, and waiting 30s to learn that costs more than
	// the answer is worth.
	Timeout time.Duration
	// Concurrency bounds simultaneous probes so a sweep of 23 clusters does
	// not become 23 simultaneous hanging requests against the orchestrator.
	Concurrency int
}

// NewProber builds a prober with sensible bounds.
func NewProber(c *Client) *Prober {
	return &Prober{c: c, Timeout: 8 * time.Second, Concurrency: 6}
}

// Probe measures one cluster: can we reach it, and which kinds answer.
func (p *Prober) Probe(ctx context.Context, clusterID int, clusterName string) Capability {
	measured := Capability{ClusterID: clusterID, ClusterName: clusterName, ProbedAt: time.Now().UTC()}
	started := time.Now()

	reachCtx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	list, err := p.c.ListResources(reachCtx, ResourceQuery{ClusterID: clusterID, GVK: GVKPod})
	measured.LatencyMs = time.Since(started).Milliseconds()

	switch {
	case err == nil && list.Len() > 0:
		measured.Reach = ReachUsable
	case err == nil:
		measured.Reach = ReachEmpty
	default:
		measured.Reach, measured.Detail = classifyProbeError(reachCtx, err)
		return measured
	}

	// Reachable: find out which kinds this token can actually read. These run
	// together because they are independent and the cluster has just proven
	// it answers quickly.
	measured.Kinds = p.probeKinds(ctx, clusterID)
	return measured
}

func (p *Prober) probeKinds(ctx context.Context, clusterID int) []KindAccess {
	out := make([]KindAccess, len(probeKinds))
	var wg sync.WaitGroup
	for i, gvk := range probeKinds {
		wg.Add(1)
		go func(i int, gvk GVK) {
			defer wg.Done()
			kctx, cancel := context.WithTimeout(ctx, p.Timeout)
			defer cancel()
			list, err := p.c.ListResources(kctx, ResourceQuery{ClusterID: clusterID, GVK: gvk})
			access := KindAccess{Kind: gvk.Kind}
			if err != nil {
				reach, detail := classifyProbeError(kctx, err)
				access.Allowed = false
				access.Detail = string(reach) + ": " + detail
			} else {
				access.Allowed = true
				access.Count = list.Len()
			}
			out[i] = access
		}(i, gvk)
	}
	wg.Wait()
	return out
}

// ProbeAll sweeps every cluster, bounded by Concurrency.
func (p *Prober) ProbeAll(ctx context.Context, clusters []Cluster) []Capability {
	out := make([]Capability, len(clusters))
	sem := make(chan struct{}, max(1, p.Concurrency))
	var wg sync.WaitGroup
	for i, cl := range clusters {
		wg.Add(1)
		go func(i int, cl Cluster) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = p.Probe(ctx, cl.ID, cl.ClusterName)
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
