package rules

import (
	"testing"

	"github.com/devtron-labs/devtron-sre-agent/internal/monitoring"
)

func alert(name, sev, ns, kind string, labels map[string]string) monitoring.Alert {
	return monitoring.Alert{Name: name, Severity: sev, Namespace: ns, Kind: kind, Labels: labels}
}

func fxCrash() monitoring.Alert {
	return alert("KubePodCrashLooping", "critical", "prod", "Pod", map[string]string{
		"reason": "CrashLoopBackOff", "pod": "payments-api-7d9f", "team": "payments",
	})
}

func fxNoise() monitoring.Alert {
	return alert("KubeHpaMaxedOut", "warning", "utils", "Pod", map[string]string{"team": "platform"})
}

func TestMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		match Match
		alert monitoring.Alert
		want  bool
	}{
		{"empty matches anything", Match{}, fxCrash(), true},
		{"name substring", Match{Name: "crashloop"}, fxCrash(), true},
		{"name is case-insensitive", Match{Name: "CRASHLOOPING"}, fxCrash(), true},
		{"name miss", Match{Name: "scheduler"}, fxCrash(), false},
		{"severity or", Match{Severity: []string{"warning", "critical"}}, fxCrash(), true},
		{"severity miss", Match{Severity: []string{"info"}}, fxCrash(), false},
		{"namespace", Match{Namespace: []string{"prod"}}, fxCrash(), true},
		{"kind", Match{Kind: []string{"Pod"}}, fxCrash(), true},
		{"labels exact", Match{Labels: map[string]string{"team": "payments"}}, fxCrash(), true},
		{"labels miss", Match{Labels: map[string]string{"team": "platform"}}, fxCrash(), false},
		{"regex", Match{LabelsRegex: map[string]string{"pod": "^payments-"}}, fxCrash(), true},
		{"regex miss", Match{LabelsRegex: map[string]string{"pod": "^billing-"}}, fxCrash(), false},
		{
			// A typo must never widen a rule. An unparseable pattern matches
			// nothing rather than everything.
			"a broken pattern matches nothing",
			Match{LabelsRegex: map[string]string{"pod": "^(unclosed"}},
			fxCrash(),
			false,
		},
		{
			"clauses are ANDed",
			Match{Severity: []string{"critical"}, Namespace: []string{"utils"}},
			fxCrash(),
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.match.Matches(tt.alert); got != tt.want {
				t.Errorf("Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShowAndMute(t *testing.T) {
	t.Parallel()

	// No rules at all must not make a cluster look silent.
	if d := (Config{}).Decide(fxCrash()); !d.Show {
		t.Error("an empty config must show everything")
	}

	// A disabled rule is not a rule.
	off := Config{Show: []Rule{{Match: Match{Name: "nothing-matches-this"}, Enabled: false}}}
	if d := off.Decide(fxCrash()); !d.Show {
		t.Error("a disabled show rule must not narrow the list")
	}

	// Any enabled show rule makes the list opt-in.
	on := Config{Show: []Rule{{Name: "prod only", Match: Match{Namespace: []string{"prod"}}, Enabled: true}}}
	if d := on.Decide(fxCrash()); !d.Show {
		t.Error("matching alert should show")
	}
	if d := on.Decide(fxNoise()); d.Show {
		t.Error("non-matching alert should be hidden once a show rule exists")
	}

	// Mute subtracts, and says which rule did it.
	muted := Config{Mute: []Rule{{Name: "hpa noise", Match: Match{Name: "hpa"}, Enabled: true}}}
	d := muted.Decide(fxNoise())
	if d.Show {
		t.Error("mute must win over an empty show list")
	}
	if d.MutedBy != "hpa noise" {
		t.Errorf("want the muting rule named, got %q", d.MutedBy)
	}
}

func TestPriorityIsFirstMatchWins(t *testing.T) {
	t.Parallel()

	cfg := Config{Priority: []Rule{
		{Name: "prod critical", Match: Match{Severity: []string{"critical"}, Namespace: []string{"prod"}}, Priority: P0, Enabled: true},
		{Name: "any critical", Match: Match{Severity: []string{"critical"}}, Priority: P1, Enabled: true},
		{Name: "catch-all", Match: Match{}, Priority: P1, Enabled: true},
	}}

	d := cfg.Decide(fxCrash())
	if d.Priority != P0 {
		t.Errorf("want the first matching rule to win, got %s", d.Priority)
	}
	if d.Why != "prod critical" {
		t.Errorf("want the deciding rule named so it can be pointed at, got %q", d.Why)
	}

	// The catch-all still claims what the specific rules did not.
	if got := cfg.Decide(fxNoise()).Priority; got != P1 {
		t.Errorf("want the catch-all to apply, got %s", got)
	}

	// Unclassified is the lowest, never the highest.
	if got := (Config{}).Decide(fxCrash()).Priority; got != P2 {
		t.Errorf("want unclassified to default to P2, got %s", got)
	}
}

func TestAutoTrigger(t *testing.T) {
	t.Parallel()

	rule := Rule{Name: "prod P0", Match: Match{Severity: []string{"critical"}}, Enabled: true}

	// The master switch is off by default, because this one spends money.
	off := Config{Auto: []Rule{rule}}
	if off.Decide(fxCrash()).Auto {
		t.Error("auto must not fire while the master switch is off")
	}

	on := Config{Auto: []Rule{rule}, AutoEnabled: true}
	if !on.Decide(fxCrash()).Auto {
		t.Error("auto should fire on a matching alert when enabled")
	}
	if on.Decide(fxNoise()).Auto {
		t.Error("auto must not fire on a non-matching alert")
	}

	// An alert not worth showing is not worth spending a run on.
	hidden := Config{
		Auto:        []Rule{{Match: Match{}, Enabled: true}},
		AutoEnabled: true,
		Mute:        []Rule{{Name: "muted", Match: Match{}, Enabled: true}},
	}
	if hidden.Decide(fxCrash()).Auto {
		t.Error("auto must never fire on a muted alert")
	}
}

func TestApply(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Mute:     []Rule{{Name: "noise", Match: Match{Name: "hpa"}, Enabled: true}},
		Priority: []Rule{{Name: "crit", Match: Match{Severity: []string{"critical"}}, Priority: P0, Enabled: true}},
	}

	alerts, decisions := cfg.Apply([]monitoring.Alert{fxCrash(), fxNoise()})
	if len(alerts) != 1 || len(decisions) != 1 {
		t.Fatalf("want the muted alert dropped, got %d", len(alerts))
	}
	if decisions[0].Priority != P0 {
		t.Errorf("want P0 carried through, got %s", decisions[0].Priority)
	}
}

func TestRank(t *testing.T) {
	t.Parallel()
	if !(Rank(P0) < Rank(P1) && Rank(P1) < Rank(P2)) {
		t.Error("P0 must sort ahead of P1 ahead of P2")
	}
}
