package incidents

import (
	"strings"
	"testing"
)

// Deterministic report fixtures, in the shape the sre agent actually emits.

// fxCorrectedReport is the everyday case: the second pass disagreed and has
// its own root cause plus remediation.
func fxCorrectedReport() string {
	return `{
		"agrees": false,
		"confidence": 0.82,
		"correctedRootCause": "The SSL certificate for insecure.devtron.info has already expired.",
		"sreNotes": "The first pass blamed the pod; the pod is healthy.",
		"remediation": [
			{"action": "Issue a new TLS certificate and load it into the ingress secret", "risk": "low"},
			{"action": "Add a cert-expiry alert at 30 days", "risk": "none"}
		]
	}`
}

// fxAgreedReport has nothing to correct, so the notes are the finding. A run
// that agreed is still a run that concluded something.
func fxAgreedReport() string {
	return `{"agrees": true, "confidence": 0.9, "sreNotes": "  Confirmed: the node was cordoned during a drain.  "}`
}

// fxEmptyReport is a report with no conclusion in it at all.
func fxEmptyReport() string { return `{"agrees": true}` }

// fxUnicodeReport carries non-ASCII, which reaches a dashboard row verbatim.
func fxUnicodeReport() string {
	return `{"agrees": false, "correctedRootCause": "証明書の有効期限切れ", "remediation": [{"action": "証明書を更新", "risk": "低"}]}`
}

func TestFindingFromCorrectedReport(t *testing.T) {
	t.Parallel()

	f := findingFrom([]byte(fxCorrectedReport()))
	if f.Agrees {
		t.Error("the report disagreed")
	}
	if !strings.HasPrefix(f.RootCause, "The SSL certificate") {
		t.Errorf("rootCause: %q", f.RootCause)
	}
	// The first remediation is the one the row shows; the rest belong on the
	// run page.
	if f.Action != "Issue a new TLS certificate and load it into the ingress secret" {
		t.Errorf("action: %q", f.Action)
	}
	if f.Risk != "low" {
		t.Errorf("risk: %q", f.Risk)
	}
	if f.Confidence != 0.82 {
		t.Errorf("confidence: %v", f.Confidence)
	}
}

// A run that agreed has no correction to make, and rendering it as an empty
// row makes a successful investigation look like it never happened.
func TestFindingFallsBackToNotesWhenNothingWasCorrected(t *testing.T) {
	t.Parallel()

	f := findingFrom([]byte(fxAgreedReport()))
	if !f.Agrees {
		t.Error("the report agreed")
	}
	if f.RootCause != "Confirmed: the node was cordoned during a drain." {
		t.Errorf("rootCause should be the trimmed notes, got %q", f.RootCause)
	}
	if f.Action != "" {
		t.Errorf("no remediation was offered, got %q", f.Action)
	}
}

func TestFindingFromDegenerateReports(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string]string{
		"empty object":  fxEmptyReport(),
		"empty bytes":   ``,
		"null":          `null`,
		"not an object": `"a string"`,
		"truncated":     `{"agrees": false, "correctedRootCa`,
		"wrong types":   `{"agrees": "yes", "confidence": "high"}`,
		"blank strings": `{"correctedRootCause": "   ", "sreNotes": "  "}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// None of these may panic, and none may invent a conclusion.
			f := findingFrom([]byte(raw))
			if f.RootCause != "" {
				t.Errorf("a report with no conclusion produced %q", f.RootCause)
			}
			if f.Action != "" {
				t.Errorf("a report with no remediation produced %q", f.Action)
			}
		})
	}
}

func TestFindingKeepsUnicode(t *testing.T) {
	t.Parallel()

	f := findingFrom([]byte(fxUnicodeReport()))
	if f.RootCause != "証明書の有効期限切れ" {
		t.Errorf("rootCause: %q", f.RootCause)
	}
	if f.Action != "証明書を更新" {
		t.Errorf("action: %q", f.Action)
	}
}

// The notes fallback is clipped so one verbose run cannot push every other
// row off the screen.
func TestFindingClipsLongNotes(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("あ", 900)
	f := findingFrom([]byte(`{"agrees": true, "sreNotes": "` + long + `"}`))
	if len([]rune(f.RootCause)) > 401 {
		t.Errorf("notes were not clipped: %d runes", len([]rune(f.RootCause)))
	}
	if f.RootCause == "" {
		t.Error("clipping should shorten the notes, not delete them")
	}
}
