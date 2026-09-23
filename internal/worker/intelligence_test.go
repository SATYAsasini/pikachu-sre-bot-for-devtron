package worker

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/devtron-labs/devtron-sre-agent/internal/runs"
)

func TestKeepThinking(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []string
		want func(t *testing.T, got []string)
	}{
		{
			name: "nil stays empty",
			in:   nil,
			want: func(t *testing.T, got []string) {
				if len(got) != 0 {
					t.Fatalf("want no steps, got %d", len(got))
				}
			},
		},
		{
			name: "blank and whitespace-only steps are dropped",
			in:   []string{"", "   ", "\t\n", "Listing pods"},
			want: func(t *testing.T, got []string) {
				if len(got) != 1 || got[0] != "Listing pods" {
					t.Fatalf("want one trimmed step, got %q", got)
				}
			},
		},
		{
			name: "steps are trimmed",
			in:   []string{"  Reading events  "},
			want: func(t *testing.T, got []string) {
				if got[0] != "Reading events" {
					t.Fatalf("want trimmed, got %q", got[0])
				}
			},
		},
		{
			name: "duplicates collapse to the first occurrence",
			in:   []string{"Listing pods", "Listing pods", " Listing pods ", "Reading events"},
			want: func(t *testing.T, got []string) {
				if len(got) != 2 {
					t.Fatalf("want 2 distinct steps, got %d: %q", len(got), got)
				}
			},
		},
		{
			name: "capped at ThinkingKept",
			in:   fxThinkingSteps(200),
			want: func(t *testing.T, got []string) {
				// fxThinkingStep cycles every 26, so dedup binds before the cap.
				if len(got) > runs.ThinkingKept {
					t.Fatalf("want at most %d, got %d", runs.ThinkingKept, len(got))
				}
			},
		},
		{
			name: "cap binds when every step is distinct",
			in: func() []string {
				out := make([]string, 0, 100)
				for i := range 100 {
					out = append(out, "step "+strings.Repeat("i", i+1))
				}
				return out
			}(),
			want: func(t *testing.T, got []string) {
				if len(got) != runs.ThinkingKept {
					t.Fatalf("want exactly %d, got %d", runs.ThinkingKept, len(got))
				}
			},
		},
		{
			name: "an over-long step is clipped and marked",
			in:   []string{fxLongStep()},
			want: func(t *testing.T, got []string) {
				if !strings.HasSuffix(got[0], "…") {
					t.Fatalf("want an ellipsis on the clipped step, got %q", got[0][:40])
				}
				if n := utf8.RuneCountInString(got[0]); n != 241 {
					t.Fatalf("want 240 runes plus the ellipsis, got %d", n)
				}
			},
		},
		{
			name: "clipping a multi-byte step leaves valid utf-8",
			in:   []string{strings.Repeat("日", 500)},
			want: func(t *testing.T, got []string) {
				if !utf8.ValidString(got[0]) {
					t.Fatal("clip split a rune")
				}
			},
		},
		{
			name: "order is preserved",
			in:   []string{"first", "second", "third"},
			want: func(t *testing.T, got []string) {
				if got[0] != "first" || got[2] != "third" {
					t.Fatalf("order changed: %q", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.want(t, keepThinking(tt.in))
		})
	}
}

func TestIntelligenceText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		in       *runs.Intelligence
		contains []string
		absent   []string
	}{
		{
			name:     "nil says it was never called",
			in:       nil,
			contains: []string{"was not called"},
			absent:   []string{"Steps Devtron took"},
		},
		{
			name: "a failed first pass says so explicitly",
			in:   fxIntelligenceFailed(),
			// A blank section would read as "it found nothing", which is a
			// different and much weaker claim than "it errored".
			contains: []string{"failed with: upstream returned 502", "deterministic facts alone"},
			absent:   []string{"Steps Devtron took"},
		},
		{
			name:     "an empty analysis is named as empty",
			in:       fxIntelligenceEmpty(),
			contains: []string{"empty analysis"},
		},
		{
			name: "the analysis carries its steps",
			in:   fxIntelligence(),
			contains: []string{
				"CrashLoopBackOff",
				"Steps Devtron took (3 in total, first 3 shown)",
				"devtron_step",
				"1. Listing pods in devtroncd",
				"3. Fetching recent events",
			},
		},
		{
			name: "a failed pass still carries any steps it managed",
			in: &runs.Intelligence{
				Failed:        "stream closed",
				ThinkingCount: 1,
				Thinking:      []string{"Listing pods"},
			},
			contains: []string{"failed with: stream closed", "Steps Devtron took", "1. Listing pods"},
		},
		{
			name:     "unicode survives intact",
			in:       fxIntelligenceUnicode(),
			contains: []string{"日本語の分析", "ポッドの状態を確認中", "🚨"},
		},
		{
			name: "the total is reported even when the kept list is shorter",
			in: &runs.Intelligence{
				Analysis:      "something",
				ThinkingCount: 49,
				Thinking:      []string{"a", "b"},
			},
			contains: []string{"(49 in total, first 2 shown)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := intelligenceText(tt.in)
			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf("want %q in output, got:\n%s", want, got)
				}
			}
			for _, no := range tt.absent {
				if strings.Contains(got, no) {
					t.Errorf("want %q absent, got:\n%s", no, got)
				}
			}
			if !utf8.ValidString(got) {
				t.Error("output is not valid utf-8")
			}
		})
	}
}
