package devtron

import (
	"strings"
	"testing"
	"time"
)

// Deterministic per-kind probe fixtures. A cluster's verdict comes from all
// five together, so these are built as whole sets.

func kindOK(kind string, count int) kindProbe {
	return kindProbe{access: KindAccess{Kind: kind, Allowed: true, Count: count}, reach: ReachUsable}
}

func kindFail(kind string, r Reach, detail string) kindProbe {
	return kindProbe{
		access: KindAccess{Kind: kind, Allowed: false, Detail: string(r) + ": " + detail},
		reach:  r,
	}
}

// fxNodesOnly is the case that started this: nothing is deployed, but the
// cluster is plainly alive because it has nodes. Judging on Pod alone called
// this empty and hid the cluster.
func fxNodesOnly() []kindProbe {
	return []kindProbe{
		kindOK("Pod", 0), kindOK("Event", 0), kindOK("Service", 0),
		kindOK("Deployment", 0), kindOK("Node", 3),
	}
}

// fxScopedToken is a token with no cluster-wide list: every kind answers, and
// every one is filtered down to nothing.
func fxScopedToken() []kindProbe {
	return []kindProbe{
		kindOK("Pod", 0), kindOK("Event", 0), kindOK("Service", 0),
		kindOK("Deployment", 0), kindOK("Node", 0),
	}
}

func fxAllTimedOut() []kindProbe {
	var out []kindProbe
	for _, k := range []string{"Pod", "Event", "Service", "Deployment", "Node"} {
		out = append(out, kindFail(k, ReachUnreachable, "timed out"))
	}
	return out
}

func fxAllForbidden() []kindProbe {
	var out []kindProbe
	for _, k := range []string{"Pod", "Event", "Service", "Deployment", "Node"} {
		out = append(out, kindFail(k, ReachForbidden, "RBAC: no view on this cluster"))
	}
	return out
}

func TestReachFromUsesEveryKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		probes []kindProbe
		want   Reach
	}{
		{"one kind with objects is enough", fxNodesOnly(), ReachUsable},
		{"everything answered and everything was empty", fxScopedToken(), ReachEmpty},
		{"every kind timed out", fxAllTimedOut(), ReachUnreachable},
		{"every kind refused", fxAllForbidden(), ReachForbidden},
		{
			// The mixed case that matters: Pod is denied but the cluster is
			// obviously readable. Denying one kind is not denying the cluster.
			"denied on one kind, objects on another",
			[]kindProbe{
				kindFail("Pod", ReachForbidden, "no view on pods"),
				kindOK("Event", 0), kindOK("Service", 12),
				kindOK("Deployment", 4), kindOK("Node", 3),
			},
			ReachUsable,
		},
		{
			// Half timed out, the rest answered with nothing. The cluster
			// spoke, so it is not unreachable.
			"partial timeout with empty answers",
			[]kindProbe{
				kindFail("Pod", ReachUnreachable, "timed out"),
				kindFail("Event", ReachUnreachable, "timed out"),
				kindOK("Service", 0), kindOK("Deployment", 0), kindOK("Node", 0),
			},
			ReachEmpty,
		},
		{
			"orchestrator errors outnumber everything",
			[]kindProbe{
				kindFail("Pod", ReachError, "HTTP 500"),
				kindFail("Event", ReachError, "HTTP 500"),
				kindFail("Service", ReachError, "HTTP 500"),
				kindFail("Deployment", ReachUnreachable, "timed out"),
				kindFail("Node", ReachError, "HTTP 500"),
			},
			ReachError,
		},
		{"nothing was probed at all", nil, ReachUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, _ := reachFrom(tt.probes)
			if got != tt.want {
				t.Errorf("want %s, got %s", tt.want, got)
			}
		})
	}
}

// A verdict that is not usable must carry why, or the settings screen has
// nothing to show the operator.
func TestReachFromCarriesTheReason(t *testing.T) {
	t.Parallel()

	if _, detail := reachFrom(fxAllForbidden()); detail == "" {
		t.Error("a refusal must say so")
	}
	if _, detail := reachFrom(fxAllTimedOut()); detail == "" {
		t.Error("a timeout must say so")
	}
	// A usable cluster has nothing to explain.
	if _, detail := reachFrom(fxNodesOnly()); detail != "" {
		t.Errorf("a readable cluster should carry no failure detail, got %q", detail)
	}
}

func TestAccessOfKeepsEveryKind(t *testing.T) {
	t.Parallel()

	got := accessOf(fxNodesOnly())
	if len(got) != 5 {
		t.Fatalf("want 5 kinds, got %d", len(got))
	}
	// Including the ones that answered with nothing — "we asked and it was
	// empty" is a different fact from "we never asked".
	for _, k := range got {
		if !k.Allowed {
			t.Errorf("%s should be allowed", k.Kind)
		}
	}
	if accessOf(nil) == nil {
		t.Error("an empty probe set should still marshal as []")
	}
}

func TestLimitNamespaces(t *testing.T) {
	t.Parallel()

	got := limitNamespaces([]string{"devtroncd", "", "devtroncd", "prod", "staging", "extra"}, 3)
	want := []string{"devtroncd", "prod", "staging"}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("want %v, got %v", want, got)
		}
	}
	if n := len(limitNamespaces(nil, 3)); n != 0 {
		t.Errorf("no namespaces means no fallback, got %d", n)
	}
	if n := len(limitNamespaces([]string{"", "", ""}, 3)); n != 0 {
		t.Errorf("blank namespaces are not namespaces, got %d", n)
	}
	// Unicode namespaces are legal in the API's eyes and must survive.
	if got := limitNamespaces([]string{"本番"}, 3); len(got) != 1 || got[0] != "本番" {
		t.Errorf("unicode namespace lost: %v", got)
	}
}

// The reach check is one request. It used to be five on every cluster, which
// on an install with forty of them was most of the sweep.
func TestReachCheckCostsOneRequest(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{ListKinds: map[string]int{"Namespace": 6}})
	defer f.Close()
	p := &Prober{c: c, Timeout: time.Second, Concurrency: 4, MaxInFlight: 4}

	cap := p.Probe(t.Context(), 1, "live", nil)
	if cap.Reach != ReachUsable {
		t.Fatalf("a cluster that lists namespaces is usable, got %s", cap.Reach)
	}
	if n := f.kindProbes(); n != 1 {
		t.Errorf("want one request, got %d", n)
	}
	if len(cap.Namespaces) != 6 {
		t.Errorf("the namespace list is the useful part, got %v", cap.Namespaces)
	}
}

// A cluster that cannot answer stops at the first request.
func TestDeadClusterCostsOneCall(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{
		ListKinds: map[string]int{},
		ListDelay: 2 * time.Second, // longer than the probe timeout: never answers
	})
	defer f.Close()
	p := &Prober{c: c, Timeout: 150 * time.Millisecond, Concurrency: 4, MaxInFlight: 4}

	cap := p.Probe(t.Context(), 7, "dead", []string{"devtroncd", "prod"})
	if cap.Reach == ReachUsable {
		t.Fatalf("a cluster that answers nothing is not usable: %+v", cap)
	}
	if n := f.kindProbes(); n != 1 {
		t.Errorf("want one request against a dead cluster, got %d", n)
	}
}

// A token that cannot list namespaces across the cluster but can read inside
// one is a usable cluster. That is the ordinary shape of an environment
// scoped token, and it used to be reported empty and hidden.
func TestNamespaceScopedTokenIsUsable(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{
		ListKinds:     map[string]int{}, // no namespaces visible cluster-wide
		NamespacePods: map[string]int{"devtroncd": 4},
	})
	defer f.Close()
	p := &Prober{c: c, Timeout: time.Second, Concurrency: 4, MaxInFlight: 4}

	cap := p.Probe(t.Context(), 1, "scoped", []string{"devtroncd", "other"})
	if cap.Reach != ReachUsable {
		t.Fatalf("want usable, got %s (%s)", cap.Reach, cap.Detail)
	}
	if !strings.Contains(cap.Detail, "devtroncd") || !strings.Contains(cap.Detail, "not across all namespaces") {
		t.Errorf("it must say the read was scoped, got %q", cap.Detail)
	}
	// The cluster-wide list, then namespaces until one yields. devtroncd is
	// first and has pods, so two.
	if n := f.kindProbes(); n != 2 {
		t.Errorf("want two requests, got %d", n)
	}
}

// With nowhere else to look, nothing visible stays nothing visible.
func TestNoNamespacesAnywhereIsNotUsable(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{ListKinds: map[string]int{}})
	defer f.Close()
	p := &Prober{c: c, Timeout: time.Second, Concurrency: 4, MaxInFlight: 4}

	if got := p.Probe(t.Context(), 1, "idle", nil).Reach; got == ReachUsable {
		t.Errorf("nothing was visible anywhere, got %s", got)
	}
}

// Devtron already knows which clusters it cannot reach and says why. Taking
// that answer costs nothing; proving it again cost 20 seconds a cluster.
func TestDevtronStatusIsTakenWithoutARequest(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{ListKinds: map[string]int{"Namespace": 3}})
	defer f.Close()
	p := &Prober{c: c, Timeout: time.Second, Concurrency: 4, MaxInFlight: 4}

	clusters := []Cluster{
		{ID: 1, ClusterName: "healthy"},
		{ID: 2, ClusterName: "broken", ErrorInCx: `dial tcp 34.0.2.215:16443: i/o timeout`},
	}
	caps := p.ProbeAll(t.Context(), clusters, SweepOptions{})

	byID := map[int]Capability{}
	for _, c := range caps {
		byID[c.ClusterID] = c
	}
	if byID[2].Reach != ReachUnreachable || !byID[2].FromDevtron {
		t.Errorf("want Devtron's own verdict, got %+v", byID[2])
	}
	if !strings.Contains(byID[2].Detail, "i/o timeout") {
		t.Errorf("Devtron's reason is more specific than ours; keep it: %q", byID[2].Detail)
	}
	if byID[1].Reach != ReachUsable {
		t.Errorf("the healthy cluster should still be measured, got %s", byID[1].Reach)
	}
	// One request in total: the healthy cluster's. The broken one cost none.
	if n := f.kindProbes(); n != 1 {
		t.Errorf("want one request across both clusters, got %d", n)
	}
}

// Devtron's status is a cached opinion. An operator who has just fixed a
// cluster must be able to overrule it.
func TestForceProbesDespiteDevtronStatus(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{ListKinds: map[string]int{"Namespace": 2}})
	defer f.Close()
	p := &Prober{c: c, Timeout: time.Second, Concurrency: 4, MaxInFlight: 4}

	clusters := []Cluster{{ID: 2, ClusterName: "just-fixed", ErrorInCx: "i/o timeout"}}
	caps := p.ProbeAll(t.Context(), clusters, SweepOptions{Force: map[int]bool{2: true}})

	if caps[0].Reach != ReachUsable {
		t.Errorf("a forced probe should measure it, got %s", caps[0].Reach)
	}
	if caps[0].FromDevtron {
		t.Error("a measured verdict is not Devtron's")
	}
	if n := f.kindProbes(); n != 1 {
		t.Errorf("want the cluster actually probed, got %d requests", n)
	}
}

// Kinds is the deep question, asked from a cluster's own page rather than as
// a gate on whether the cluster may be offered at all.
func TestKindsMeasuresEveryKind(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{ListKinds: map[string]int{"Pod": 3, "Node": 1}})
	defer f.Close()
	p := &Prober{c: c, Timeout: time.Second, Concurrency: 4, MaxInFlight: 8}

	got := p.Kinds(t.Context(), 1, "")
	if len(got) != len(probeKinds) {
		t.Fatalf("want every kind, got %d", len(got))
	}
	counts := map[string]int{}
	for _, k := range got {
		counts[k.Kind] = k.Count
	}
	if counts["Pod"] != 3 || counts["Node"] != 1 {
		t.Errorf("counts did not come through: %v", counts)
	}
}

// Answering is not enough. A cluster where every read succeeds and returns
// nothing produces an investigation that concludes nothing, which is the
// outcome this measurement exists to keep out of the picker. It was briefly
// reported usable, and the probe record is what made that visible.
func TestEverythingEmptyIsNotUsable(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{ListKinds: map[string]int{}})
	defer f.Close()
	p := &Prober{c: c, Timeout: time.Second, Concurrency: 4, MaxInFlight: 4}

	cap := p.Probe(t.Context(), 1, "idle", []string{"one", "two"})
	if cap.Reach == ReachUsable {
		t.Fatalf("nothing came back anywhere; not usable: %+v", cap)
	}
	if cap.Reach != ReachEmpty {
		t.Errorf("want empty, got %s", cap.Reach)
	}
	// Both namespaces tried before concluding: one empty namespace is not
	// evidence that a cluster is idle.
	if n := f.kindProbes(); n != 3 {
		t.Errorf("want the cluster-wide read plus both namespaces, got %d", n)
	}
	if !strings.Contains(cap.Detail, "one, two") {
		t.Errorf("it must say where it looked, got %q", cap.Detail)
	}
}

// Every step is recorded, so "what did you ask and what did it say" is
// answerable from the row rather than from the source.
func TestProbeRecordsWhatItAsked(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{
		ListKinds:     map[string]int{},
		NamespacePods: map[string]int{"devtroncd": 4},
	})
	defer f.Close()
	p := &Prober{c: c, Timeout: time.Second, Concurrency: 4, MaxInFlight: 4}

	cap := p.Probe(t.Context(), 1, "scoped", []string{"devtroncd"})
	if len(cap.Steps) != 2 {
		t.Fatalf("want a step per question, got %d: %+v", len(cap.Steps), cap.Steps)
	}
	if !strings.Contains(cap.Steps[0].Ask, "Namespace") || cap.Steps[0].Outcome != "answered with nothing" {
		t.Errorf("first step: %+v", cap.Steps[0])
	}
	if !strings.Contains(cap.Steps[1].Ask, "devtroncd") || cap.Steps[1].Outcome != "answered with 4" {
		t.Errorf("second step: %+v", cap.Steps[1])
	}
	for i, st := range cap.Steps {
		if st.Path == "" {
			t.Errorf("step %d must name the endpoint so it can be repeated by hand", i)
		}
	}

	// A verdict taken from Devtron records that too, with no request.
	d := FromDevtronStatus(Cluster{ID: 2, ClusterName: "x", ErrorInCx: "i/o timeout"})
	if len(d.Steps) != 1 || d.Steps[0].Outcome != "Devtron reports it cannot connect" {
		t.Errorf("devtron verdict steps: %+v", d.Steps)
	}
}
