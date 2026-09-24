package incidents

import (
	"strings"
	"testing"

	"github.com/devtron-labs/devtron-sre-agent/internal/monitoring"
)

func a(name, ns, resource, sev, fp string) monitoring.Alert {
	return monitoring.Alert{Name: name, Namespace: ns, Resource: resource, Severity: sev, Fingerprint: fp}
}

// Identity is what makes 23 days of the same alert one row instead of
// thousands, so the rules about what counts as "the same" are the feature.
func TestKeyIdentity(t *testing.T) {
	t.Parallel()

	base := a("KubePodCrashLooping", "prod", "payments-7d9f", "warning", "abc")

	same := []struct {
		why   string
		other monitoring.Alert
	}{
		{
			// An alert that escalates is the same problem getting worse, not
			// a new problem.
			"severity escalates",
			a("KubePodCrashLooping", "prod", "payments-7d9f", "critical", "abc"),
		},
		{
			// vmalert reuses fingerprints across distinct alerts, which is why
			// identity deliberately ignores them.
			"fingerprint changes",
			a("KubePodCrashLooping", "prod", "payments-7d9f", "warning", "zzz"),
		},
		{
			"case differs",
			a("kubepodcrashlooping", "PROD", "payments-7d9f", "warning", "abc"),
		},
		{
			"whitespace differs",
			a("  KubePodCrashLooping ", "prod ", " payments-7d9f", "warning", "abc"),
		},
	}
	for _, tt := range same {
		t.Run("same/"+tt.why, func(t *testing.T) {
			t.Parallel()
			if !SameAlert(base, tt.other) {
				t.Errorf("want the same identity when %s", tt.why)
			}
		})
	}

	different := []struct {
		why   string
		other monitoring.Alert
	}{
		{"different alert", a("KubePodNotReady", "prod", "payments-7d9f", "warning", "abc")},
		{"different namespace", a("KubePodCrashLooping", "staging", "payments-7d9f", "warning", "abc")},
		{"different resource", a("KubePodCrashLooping", "prod", "billing-1a2b", "warning", "abc")},
	}
	for _, tt := range different {
		t.Run("different/"+tt.why, func(t *testing.T) {
			t.Parallel()
			if SameAlert(base, tt.other) {
				t.Errorf("want a different identity when %s", tt.why)
			}
		})
	}
}

// The version prefix is what lets the identity rule change later without
// orphaning every row already in the table.
func TestKeyIsVersioned(t *testing.T) {
	t.Parallel()

	k := Key(a("X", "", "", "", ""))
	if !strings.HasPrefix(k, "auto:1:") {
		t.Errorf("want a versioned key, got %q", k)
	}
	if len(k) != len("auto:1:")+32 {
		t.Errorf("want a fixed-width key so it fits an index cleanly, got %d chars", len(k))
	}
}

func TestKeyIsStable(t *testing.T) {
	t.Parallel()

	x := a("KubeSchedulerDown", "kube-system", "kube-scheduler", "critical", "f1")
	if Key(x) != Key(x) {
		t.Error("the same payload must always produce the same key")
	}
	// An empty alert still produces a usable key rather than panicking.
	if Key(monitoring.Alert{}) == "" {
		t.Error("even an empty alert needs an identity")
	}
}
