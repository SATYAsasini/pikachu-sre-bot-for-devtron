package agents

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSettles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		verdict *settledVerdict
		want    bool
		why     string
	}{
		{
			name:    "nil never settles",
			verdict: nil,
			want:    false,
			why:     "no verdict means the judge produced nothing to stand on",
		},
		{
			name:    "supported with nothing outstanding",
			verdict: fxSettled(),
			want:    true,
		},
		{
			name:    "an unverifiable claim does not force the deep dive",
			verdict: fxSettledWithUnverifiable(),
			want:    true,
			why:     "no amount of further digging settles it; it belongs in unknowns",
		},
		{
			name:    "gaps alone do not force the deep dive",
			verdict: fxSettledWithGaps(),
			want:    true,
			why:     "a gap is something the first pass never established, not a request",
		},
		{
			name:    "a contradicted claim always runs the deep dive",
			verdict: fxContradicted(),
			want:    false,
		},
		{
			name:    "a contradicted claim under a supported verdict still runs it",
			verdict: fxVerdict("supported", []string{"contradicted"}, nil, nil),
			want:    false,
		},
		{
			name:    "an explicit next check always runs the deep dive",
			verdict: fxAsksForChecks(),
			want:    false,
		},
		{
			name:    "anything short of supported runs the deep dive",
			verdict: fxVerdict("partly_supported", nil, nil, nil),
			want:    false,
		},
		{
			name:    "insufficient runs the deep dive",
			verdict: fxVerdict("insufficient", nil, nil, nil),
			want:    false,
		},
		{
			name:    "an unrecognised verdict string runs the deep dive",
			verdict: fxVerdict("¯\\_(ツ)_/¯", nil, nil, nil),
			want:    false,
		},
		{
			name:    "an empty verdict string runs the deep dive",
			verdict: fxVerdict("", nil, nil, nil),
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := settles(tt.verdict); got != tt.want {
				t.Errorf("want %v, got %v (%s)", tt.want, got, tt.why)
			}
		})
	}
}

func TestSettledWhy(t *testing.T) {
	t.Parallel()

	if got := settledWhy(nil); !strings.Contains(got, "not needed") {
		t.Errorf("want a reason even for nil, got %q", got)
	}
	if got := settledWhy(fxSettled()); strings.Contains(got, "gap(s)") {
		t.Errorf("want no gap clause when there are none, got %q", got)
	}
	got := settledWhy(fxSettledWithGaps())
	if !strings.Contains(got, "2 gap(s)") {
		t.Errorf("want the gap count named, got %q", got)
	}
}

func TestSkippedReportJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		why  string
	}{
		{"plain", "the judge asked for no further checks"},
		{"empty", ""},
		{"with quotes and newlines", "it said \"done\"\nand stopped"},
		{"unicode", "判断: 追加の確認は不要 🚦"},
		{"backslashes", `C:\path\to\nowhere`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			raw := skippedReportJSON(tt.why)
			if !json.Valid(raw) {
				t.Fatalf("not valid JSON: %s", raw)
			}
			var got struct {
				Agrees   bool   `json:"agrees"`
				SreNotes string `json:"sreNotes"`
				Skipped  bool   `json:"skipped"`
			}
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !got.Agrees {
				t.Error("a skipped dive agrees: the judge found nothing to disagree with")
			}
			if !got.Skipped {
				t.Error("want skipped:true so the UI can tell this from a real report")
			}
			if got.SreNotes != tt.why {
				t.Errorf("want the reason carried through, got %q", got.SreNotes)
			}
		})
	}
}

// A skipped deep dive and a crashed one both leave no report in session state.
// Only the guard can tell them apart, and before it could, a settled, correct
// investigation was being marked failed.
func TestGuardSkippedReason(t *testing.T) {
	t.Parallel()

	var entries int
	ledger := func(context.Context, string, string, any) (int, error) {
		entries++
		return entries, nil
	}
	g := NewGuard(&Budget{}, ledger, nil)

	if why, ok := g.SkippedReason(agentSRE); ok || why != "" {
		t.Fatalf("want nothing recorded before a skip, got %q %v", why, ok)
	}

	g.NoteSkipped(agentSRE, "nothing left to check")

	why, ok := g.SkippedReason(agentSRE)
	if !ok || why != "nothing left to check" {
		t.Errorf("want the reason back, got %q %v", why, ok)
	}
	if entries != 1 {
		t.Errorf("want one ledger entry for the skip, got %d", entries)
	}
	if _, ok := g.SkippedReason(agentJudge); ok {
		t.Error("the judge was not skipped and must not report as skipped")
	}
}

func TestParseVerdictTolerance(t *testing.T) {
	t.Parallel()

	// The judge's output arrives as whatever the model emitted, so the parser
	// has to cope with a fenced block and with prose either side of it.
	tests := []struct {
		name string
		raw  any
		want bool // whether a verdict comes back at all
	}{
		{"bare object", `{"verdict":"supported"}`, true},
		{"fenced", "```json\n{\"verdict\":\"supported\"}\n```", true},
		{"prose around it", "Here you go:\n{\"verdict\":\"supported\"}\nHope that helps.", true},
		{"raw message", json.RawMessage(`{"verdict":"supported"}`), true},
		{"not json at all", "the cluster seems fine", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseVerdict(tt.raw)
			if (got != nil) != tt.want {
				t.Errorf("want parsed=%v, got %#v", tt.want, got)
			}
		})
	}
}
