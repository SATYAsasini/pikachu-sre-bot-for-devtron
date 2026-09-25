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

// A cluster the orchestrator cannot reach must cost one call, not five. The
// first version of the all-kinds probe spent five on every dead cluster,
// tripled the sweep, and put enough load on the orchestrator that a healthy
// cluster timed out and was reported unreachable.
func TestDeadClusterCostsOneCall(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{
		ListKinds: map[string]int{},
		ListDelay: 2 * time.Second, // longer than the probe timeout: it never answers
	})
	defer f.Close()
	p := &Prober{c: c, Timeout: 150 * time.Millisecond, Concurrency: 4}

	cap := p.Probe(t.Context(), 7, "dead", []string{"devtroncd", "prod"})
	if cap.Reach == ReachUsable {
		t.Fatalf("a cluster that answers nothing is not usable: %+v", cap)
	}
	if n := len(cap.Kinds); n != 1 {
		t.Errorf("a cluster that cannot answer should be probed once, got %d kinds", n)
	}
}

// But a cluster that answers — even to say no — is worth the full question.
func TestAnsweringClusterIsProbedForEveryKind(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{ListKinds: map[string]int{"Node": 3}})
	defer f.Close()
	p := &Prober{c: c, Timeout: time.Second, Concurrency: 4}

	cap := p.Probe(t.Context(), 1, "nodes-only", nil)
	if cap.Reach != ReachUsable {
		t.Fatalf("a cluster with nodes is alive: %s %q", cap.Reach, cap.Detail)
	}
	if n := len(cap.Kinds); n != len(probeKinds) {
		t.Errorf("want every kind measured, got %d", n)
	}
}

// The case the namespace fallback exists for: a token that reads nothing
// across all namespaces and plenty inside one.
func TestNamespaceFallbackRescuesAScopedToken(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{
		// Empty cluster-wide; pods only when a namespace is named.
		ListKinds:     map[string]int{},
		NamespacePods: map[string]int{"devtroncd": 4},
	})
	defer f.Close()
	p := &Prober{c: c, Timeout: time.Second, Concurrency: 4}

	cap := p.Probe(t.Context(), 1, "scoped", []string{"other", "devtroncd"})
	if cap.Reach != ReachUsable {
		t.Fatalf("a cluster readable in one namespace is usable: %s", cap.Reach)
	}
	if !strings.Contains(cap.Detail, "devtroncd") {
		t.Errorf("it must say where it was readable, got %q", cap.Detail)
	}
}

// With nowhere else to look, empty stays empty.
func TestEmptyStaysEmptyWithNoNamespaces(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{ListKinds: map[string]int{}})
	defer f.Close()
	p := &Prober{c: c, Timeout: time.Second, Concurrency: 4}

	if got := p.Probe(t.Context(), 1, "idle", nil).Reach; got != ReachEmpty {
		t.Errorf("want empty, got %s", got)
	}
}
