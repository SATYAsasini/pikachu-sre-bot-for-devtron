package agents

import (
	"encoding/json"
	"testing"
)

// The two agents were merged into one that emits both halves in a single
// object. Splitting it back apart is the only new seam, and the report half is
// the part someone acts on — so a malformed verdict must never cost them the
// remediation.
func TestSplitAnalysis(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		raw        string
		wantVerdct bool
		wantReport bool
		check      func(t *testing.T, verdict, report json.RawMessage)
	}{
		{
			name:       "both halves",
			raw:        `{"verdict":{"verdict":"supported"},"report":{"agrees":true,"confidence":0.8}}`,
			wantVerdct: true,
			wantReport: true,
			check: func(t *testing.T, v, r json.RawMessage) {
				var got struct {
					Confidence float64 `json:"confidence"`
				}
				if err := json.Unmarshal(r, &got); err != nil || got.Confidence != 0.8 {
					t.Errorf("report did not survive the split: %s", r)
				}
			},
		},
		{
			name: "a bare report is still usable",
			// An agent that ignored the wrapper and emitted the report shape
			// directly has still done the work. Discarding it because the
			// envelope was missing would be the wrong trade.
			raw:        `{"agrees":false,"correctedRootCause":"the pod never completes initdb"}`,
			wantVerdct: false,
			wantReport: true,
		},
		{
			name:       "verdict only",
			raw:        `{"verdict":{"verdict":"insufficient"}}`,
			wantVerdct: true,
			wantReport: true, // falls back to the whole object
		},
		{name: "empty", raw: ``},
		{name: "not an object", raw: `"hello"`},
		{name: "malformed", raw: `{ this is not json`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v, r := splitAnalysis(json.RawMessage(tt.raw))
			if (len(v) > 0) != tt.wantVerdct {
				t.Errorf("verdict present = %v, want %v (%s)", len(v) > 0, tt.wantVerdct, v)
			}
			if (len(r) > 0) != tt.wantReport {
				t.Errorf("report present = %v, want %v (%s)", len(r) > 0, tt.wantReport, r)
			}
			if tt.check != nil {
				tt.check(t, v, r)
			}
		})
	}
}

// extractJSON is what turns whatever the model emitted into the object above.
func TestExtractJSONTolerance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"bare object", `{"report":{}}`, true},
		{"fenced", "```json\n{\"report\":{}}\n```", true},
		{"prose either side", "Here you go:\n{\"report\":{}}\nHope that helps.", true},
		{"not json", "the cluster seems fine", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := len(extractJSON(tt.in)) > 0; got != tt.want {
				t.Errorf("extractJSON(%q) produced=%v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
