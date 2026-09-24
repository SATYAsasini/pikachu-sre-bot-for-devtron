package runs

import (
	"testing"

	"github.com/devtron-labs/devtron-sre-agent/internal/devtron"
)

// fxPick is the everyday stored value.
func fxPick() *devtron.Pick {
	return &devtron.Pick{Namespace: "monitoring", Name: "vmalertmanager-victoria-metrics"}
}

// fxPickUnicode exercises a namespace and name that are not ASCII. They come
// back out of jsonb and go into a Kubernetes proxy path.
func fxPickUnicode() *devtron.Pick {
	return &devtron.Pick{Namespace: "監視", Name: "alertmanager-告警"}
}

func TestPickRoundTripsThroughTheColumn(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]*devtron.Pick{
		"ascii":   fxPick(),
		"unicode": fxPickUnicode(),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := decodePick(encodePick(want))
			if got == nil {
				t.Fatal("a valid pick did not survive the round trip")
			}
			if *got != *want {
				t.Errorf("want %+v, got %+v", *want, *got)
			}
		})
	}
}

func TestEncodePickStoresNullForNothing(t *testing.T) {
	t.Parallel()

	// A null column is how "use whatever discovery picks" is stored, and
	// PinnedMonitoringClusters keys off exactly that.
	for name, p := range map[string]*devtron.Pick{
		"nil":          nil,
		"empty":        {},
		"no name":      {Namespace: "monitoring"},
		"no namespace": {Name: "alertmanager"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := encodePick(p); got != nil {
				t.Errorf("want a null column, got %q", got)
			}
		})
	}
}

func TestDecodePickTreatsRubbishAsUnpinned(t *testing.T) {
	t.Parallel()

	// A row nobody can read must not be a cluster nobody can monitor. The
	// worst outcome of a malformed choice is falling back to discovery.
	for name, raw := range map[string]string{
		"empty":          ``,
		"null literal":   `null`,
		"not an object":  `"monitoring/alertmanager"`,
		"truncated":      `{"namespace":"monitoring","na`,
		"missing name":   `{"namespace":"monitoring"}`,
		"blank fields":   `{"namespace":"","name":""}`,
		"wrong types":    `{"namespace":1,"name":true}`,
		"unknown fields": `{"cluster":"a","port":"9093"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := decodePick([]byte(raw)); got != nil {
				t.Errorf("want nil, got %+v", *got)
			}
		})
	}
}

func TestDecodePickIgnoresExtraFields(t *testing.T) {
	t.Parallel()

	// A column written by a newer version must still resolve, not disable
	// the cluster's choice.
	got := decodePick([]byte(`{"namespace":"monitoring","name":"vmalert","flavor":"vmalert","port":"8080"}`))
	if got == nil || got.Namespace != "monitoring" || got.Name != "vmalert" {
		t.Fatalf("got %+v", got)
	}
}
