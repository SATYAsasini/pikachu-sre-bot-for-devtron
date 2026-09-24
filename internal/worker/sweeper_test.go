package worker

import (
	"strings"
	"testing"

	"github.com/devtron-labs/devtron-sre-agent/internal/monitoring"
	"github.com/devtron-labs/devtron-sre-agent/internal/rules"
)

// The sweep is the only thing in this product that spends money without
// anybody asking it to, so what it claims is the part worth pinning down.
func TestAutoPicksClaimsOnlyWhatTheRulesSay(t *testing.T) {
	t.Parallel()

	cfg := fxAutoConfig()
	picks := autoPicks(cfg, []monitoring.Alert{fxCrashLoop(), fxNoisyInfo()})

	if len(picks) != 1 {
		t.Fatalf("want 1 pick, got %d", len(picks))
	}
	if picks[0].Alert.Name != "KubePodCrashLooping" {
		t.Errorf("claimed the wrong alert: %q", picks[0].Alert.Name)
	}
	// The priority must be the one the rules gave it, not the default, or the
	// dashboard disagrees with the list the alert came from.
	if picks[0].Priority != rules.P0 {
		t.Errorf("priority = %q, want P0", picks[0].Priority)
	}
}

// The master switch is the emergency brake. A cluster with auto rules written
// and the switch off must start nothing at all.
func TestAutoPicksRespectsTheMasterSwitch(t *testing.T) {
	t.Parallel()

	cfg := fxAutoConfig()
	cfg.AutoEnabled = false

	if picks := autoPicks(cfg, []monitoring.Alert{fxCrashLoop()}); len(picks) != 0 {
		t.Fatalf("switch off still claimed %d alerts", len(picks))
	}
}

// Two alert sources reporting one problem is one problem. Both arrive in the
// same poll, before either has a row in the table to collide with, so the
// database's unique index cannot save us here — this has to.
func TestAutoPicksDeduplicatesWithinOnePoll(t *testing.T) {
	t.Parallel()

	prom := fxCrashLoop()
	prom.Fingerprint = "from-prometheus"
	vm := fxCrashLoop()
	vm.Fingerprint = "from-vmalert"
	// Severity escalating between the two copies must not make them distinct:
	// it is the same pod getting worse.
	vm.Severity = "critical"

	picks := autoPicks(fxAutoConfig(), []monitoring.Alert{prom, vm})
	if len(picks) != 1 {
		t.Fatalf("want 1 pick for one problem, got %d", len(picks))
	}
}

// Muting wins. An alert somebody hid is not an alert they want a run opened
// on, and a rule set where both lists match is a mistake that must fail quiet
// rather than loud.
func TestAutoPicksSkipsMutedAlerts(t *testing.T) {
	t.Parallel()

	cfg := fxAutoConfig()
	cfg.Mute = []rules.Rule{{Name: "hush prod", Match: rules.Match{Namespace: []string{"prod"}}, Enabled: true}}

	if picks := autoPicks(cfg, []monitoring.Alert{fxCrashLoop()}); len(picks) != 0 {
		t.Fatalf("claimed a muted alert: %d picks", len(picks))
	}
}

// Nil in, nothing out, no panic. A cluster whose alert source answered with
// nothing is the common case at 3am and must not be the one that crashes the
// sweep for every other cluster behind it.
func TestAutoPicksEmptyAndNilStates(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		why    string
		cfg    rules.Config
		alerts []monitoring.Alert
	}{
		{"nil alerts", fxAutoConfig(), nil},
		{"empty alerts", fxAutoConfig(), []monitoring.Alert{}},
		{"zero config", rules.Config{}, []monitoring.Alert{fxCrashLoop()}},
		{"auto on, no rules", rules.Config{AutoEnabled: true}, []monitoring.Alert{fxCrashLoop()}},
	} {
		if picks := autoPicks(tc.cfg, tc.alerts); len(picks) != 0 {
			t.Errorf("%s: want no picks, got %d", tc.why, len(picks))
		}
	}
}

// A rule with no conditions used to claim everything, which turned one click
// into an investigation of every alert on the cluster. It stays fixed here
// too, because this is the path where nobody is watching.
func TestAutoPicksIgnoresABlankRule(t *testing.T) {
	t.Parallel()

	cfg := rules.Config{
		AutoEnabled: true,
		Auto:        []rules.Rule{{Name: "", Match: rules.Match{}, Enabled: true}},
	}
	if picks := autoPicks(cfg, []monitoring.Alert{fxCrashLoop(), fxNoisyInfo()}); len(picks) != 0 {
		t.Fatalf("a blank rule claimed %d alerts", len(picks))
	}
}

// An explicit catch-all is a decision, so it is allowed to claim everything —
// but each distinct problem only once.
func TestAutoPicksHonoursAnExplicitCatchAll(t *testing.T) {
	t.Parallel()

	cfg := rules.Config{
		AutoEnabled: true,
		Auto:        []rules.Rule{{Name: "everything", CatchAll: true, Enabled: true}},
	}
	picks := autoPicks(cfg, []monitoring.Alert{fxCrashLoop(), fxNoisyInfo(), fxCrashLoop()})
	if len(picks) != 2 {
		t.Fatalf("want 2 distinct problems, got %d", len(picks))
	}
}

// Malformed and hostile payloads. An alert name is somebody else's string and
// arrives with whatever is in it; unicode, control characters and absurd
// lengths must all reach a decision rather than an exception.
func TestAutoPicksSurvivesMalformedAlerts(t *testing.T) {
	t.Parallel()

	cfg := rules.Config{
		AutoEnabled: true,
		Auto:        []rules.Rule{{Name: "everything", CatchAll: true, Enabled: true}},
	}
	alerts := []monitoring.Alert{
		{},
		{Name: "支払いサービスがクラッシュしています", Namespace: "本番", Resource: "決済-7d9f"},
		{Name: "Crash\x00Looping", Namespace: "prod\n", Resource: strings.Repeat("x", 4096)},
		{Name: "emoji 🔥", Labels: nil},
	}

	picks := autoPicks(cfg, alerts)
	if len(picks) != len(alerts) {
		t.Fatalf("want %d distinct picks, got %d", len(alerts), len(picks))
	}
	for _, p := range picks {
		if p.Priority == "" {
			t.Errorf("alert %q came out with no priority", p.Alert.Name)
		}
	}
}
