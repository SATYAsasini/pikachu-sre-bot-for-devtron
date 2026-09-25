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

// The reach check leads with the namespaces endpoint, then one read. Two
// calls, and both questions answered honestly — which listing Kind=Namespace
// could not do, because a namespace-scoped token has every row of that list
// filtered away and gets an empty 200 that looks identical to an idle
// cluster.
func TestReachCheckAsksNamespacesThenReads(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{
		Namespaces: []string{"devtroncd", "monitoring"},
		ListKinds:  map[string]int{"Pod": 12},
	})
	defer f.Close()
	p := &Prober{c: c, Timeout: time.Second, Concurrency: 4, MaxInFlight: 4}

	cap := p.Probe(t.Context(), 1, "live", nil)
	if cap.Reach != ReachUsable {
		t.Fatalf("want usable, got %s (%s)", cap.Reach, cap.Detail)
	}
	if len(cap.Namespaces) != 2 {
		t.Errorf("the namespace grant is the useful part, got %v", cap.Namespaces)
	}
	if n, k := f.nsProbes(), f.kindProbes(); n != 1 || k != 1 {
		t.Errorf("want one namespace call and one read, got %d and %d", n, k)
	}
}

// A cluster the orchestrator cannot reach says so on the first call, with a
// 400 rather than an empty list — the one place that distinction is made.
func TestUnreachableClusterIsToldApartFromAnEmptyOne(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{
		NamespacesStatus: 400,
		NamespacesBody:   `{"errors":[{"userMessage":"cluster is not reachable"}]}`,
	})
	defer f.Close()
	p := &Prober{c: c, Timeout: time.Second, Concurrency: 4, MaxInFlight: 4}

	cap := p.Probe(t.Context(), 7, "dead", nil)
	if cap.Reach != ReachUnreachable {
		t.Fatalf("want unreachable, got %s (%s)", cap.Reach, cap.Detail)
	}
	// It stops there. No point reading objects from a cluster nobody can get to.
	if k := f.kindProbes(); k != 0 {
		t.Errorf("want no object reads against an unreachable cluster, got %d", k)
	}
}

// No namespace grant means nothing can be read here, whatever the cluster's
// own health. That is a token problem and must not be reported as an outage.
func TestNoNamespaceGrantIsForbidden(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{Namespaces: nil, ListKinds: map[string]int{"Pod": 99}})
	defer f.Close()
	p := &Prober{c: c, Timeout: time.Second, Concurrency: 4, MaxInFlight: 4}

	cap := p.Probe(t.Context(), 1, "no-grant", nil)
	if cap.Reach != ReachForbidden {
		t.Fatalf("want forbidden, got %s", cap.Reach)
	}
	if !strings.Contains(cap.Detail, "no namespace") {
		t.Errorf("it must name the cause, got %q", cap.Detail)
	}
	if k := f.kindProbes(); k != 0 {
		t.Errorf("nothing is readable, so nothing should be read; got %d", k)
	}
}

// Reachable, permitted, and nothing running. Offering that is offering an
// investigation that can only conclude nothing.
func TestGrantedButEmptyIsNotUsable(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{
		Namespaces: []string{"devtroncd"},
		ListKinds:  map[string]int{},
	})
	defer f.Close()
	p := &Prober{c: c, Timeout: time.Second, Concurrency: 4, MaxInFlight: 4}

	cap := p.Probe(t.Context(), 1, "idle", nil)
	if cap.Reach != ReachEmpty {
		t.Fatalf("want empty, got %s", cap.Reach)
	}
	if !strings.Contains(cap.Detail, "no pods are visible") {
		t.Errorf("it must say what was missing, got %q", cap.Detail)
	}
}

// Devtron already knows which clusters it cannot reach and says why. Taking
// that answer costs nothing; proving it again cost 20 seconds a cluster.
func TestDevtronStatusIsTakenWithoutARequest(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{
		Namespaces: []string{"devtroncd"},
		ListKinds:  map[string]int{"Pod": 3},
	})
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
	// The broken one cost nothing at all.
	if n := f.nsProbes(); n != 1 {
		t.Errorf("want one namespace call across both clusters, got %d", n)
	}
}

// Devtron's status is a cached opinion. An operator who has just fixed a
// cluster must be able to overrule it.
func TestForceProbesDespiteDevtronStatus(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{
		Namespaces: []string{"devtroncd"},
		ListKinds:  map[string]int{"Pod": 2},
	})
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
	if n := f.nsProbes(); n != 1 {
		t.Errorf("want the cluster actually probed, got %d namespace calls", n)
	}
}

// Every step is recorded, so "what did you ask and what did it say" is
// answerable from the row rather than from the source.
func TestProbeRecordsWhatItAsked(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{
		Namespaces: []string{"devtroncd"},
		ListKinds:  map[string]int{"Pod": 4},
	})
	defer f.Close()
	p := &Prober{c: c, Timeout: time.Second, Concurrency: 4, MaxInFlight: 4}

	cap := p.Probe(t.Context(), 1, "scoped", nil)
	if len(cap.Steps) != 2 {
		t.Fatalf("want a step per question, got %d: %+v", len(cap.Steps), cap.Steps)
	}
	if !strings.Contains(cap.Steps[0].Ask, "namespaces") || cap.Steps[0].Outcome != "answered with 1" {
		t.Errorf("first step: %+v", cap.Steps[0])
	}
	if !strings.Contains(cap.Steps[1].Ask, "Pod") || cap.Steps[1].Outcome != "answered with 4" {
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

func TestDecodeNamespaceList(t *testing.T) {
	t.Parallel()

	// Plain names, the /v2 metadata shape, and the all-clusters map — a
	// reach check must not fail because a Devtron version wraps them
	// differently.
	cases := map[string]any{
		"strings":    []any{"b", "a", "a"},
		"objects":    []any{map[string]any{"name": "b"}, map[string]any{"name": "a"}},
		"by cluster": map[string]any{"c1": []any{"a"}, "c2": []any{"b"}},
	}
	for name, raw := range cases {
		got := decodeNamespaceList(raw)
		if len(got) != 2 || got[0] != "a" || got[1] != "b" {
			t.Errorf("%s: want [a b], got %v", name, got)
		}
	}
	for name, raw := range map[string]any{"nil": nil, "string": "a", "empty": []any{}} {
		if got := decodeNamespaceList(raw); len(got) != 0 {
			t.Errorf("%s: want none, got %v", name, got)
		}
	}
}

// The reach probe must never ask for objects across the whole cluster.
//
// It looks equivalent to a namespaced read and is not: the orchestrator
// lists with its own credentials and filters afterwards, so namespace:""
// fetches every pod in every namespace before discarding all but the few
// the token can see. On a 52-cluster production install that was past the
// deadline on all 27 reachable clusters, and held megabytes of JSON per
// probe — enough, a dozen at a time, to restart the process.
func TestReachProbeNeverReadsAcrossTheWholeCluster(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{
		Namespaces:    []string{"devtroncd", "monitoring"},
		ListKinds:     map[string]int{},
		NamespacePods: map[string]int{"devtroncd": 9},
	})
	defer f.Close()
	p := &Prober{c: c, Timeout: time.Second, Concurrency: 4, MaxInFlight: 4}

	cap := p.Probe(t.Context(), 1, "big", nil)
	if cap.Reach != ReachUsable {
		t.Fatalf("want usable, got %s (%s)", cap.Reach, cap.Detail)
	}
	for _, st := range cap.Steps {
		if strings.Contains(st.Ask, "across the cluster") {
			t.Errorf("the probe asked cluster-wide: %q", st.Ask)
		}
	}
	// It stops at the first namespace that answers with something.
	if n := f.kindProbes(); n != 1 {
		t.Errorf("want one object read, got %d", n)
	}
}

// One idle namespace is not an idle cluster, so it keeps looking — but only
// as far as the cap.
func TestReachProbeTriesEachNamespaceThenStops(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{
		Namespaces:    []string{"a", "b", "c", "d", "e"},
		ListKinds:     map[string]int{},
		NamespacePods: map[string]int{"e": 4}, // past the cap on purpose
	})
	defer f.Close()
	p := &Prober{c: c, Timeout: time.Second, Concurrency: 4, MaxInFlight: 4}

	cap := p.Probe(t.Context(), 1, "idle", nil)
	if cap.Reach != ReachEmpty {
		t.Fatalf("want empty, got %s", cap.Reach)
	}
	if n := f.kindProbes(); n != maxProbeNamespaces {
		t.Errorf("want %d reads, got %d", maxProbeNamespaces, n)
	}
	// And it says which ones it looked in, so "empty" is checkable.
	if !strings.Contains(cap.Detail, "a, b, c") {
		t.Errorf("detail must name the namespaces tried, got %q", cap.Detail)
	}
}

// A cluster that listed its namespaces is not unreachable, whatever the next
// read does. Saying otherwise contradicts the evidence the same probe just
// recorded — and sends an operator to check a network path that is fine.
func TestSlowReadIsNotReportedAsUnreachable(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{
		Namespaces: []string{"devtroncd"},
		ListKinds:  map[string]int{"Pod": 5},
		KindDelay:  2 * time.Second, // only the object read is slow
	})
	defer f.Close()
	// Long enough for the namespace listing, too short for the read.
	p := &Prober{c: c, Timeout: 300 * time.Millisecond, Concurrency: 4, MaxInFlight: 4}

	cap := p.Probe(t.Context(), 1, "slow", nil)
	if cap.Reach == ReachUnreachable {
		t.Fatalf("it answered a call; it is not unreachable: %+v", cap)
	}
	if !strings.Contains(cap.Detail, "slow, not unreachable") {
		t.Errorf("the contradiction must be stated plainly, got %q", cap.Detail)
	}
	// And both steps are on the record, so the contradiction is checkable.
	if len(cap.Steps) != 2 {
		t.Errorf("want both steps recorded, got %d", len(cap.Steps))
	}
}
