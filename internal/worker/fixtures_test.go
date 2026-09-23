package worker

import (
	"strings"

	"github.com/devtron-labs/devtron-sre-agent/internal/runs"
)

// Deterministic fixtures for the worker package. Everything here is built
// from a function rather than declared as a package-level literal, so a test
// that mutates what it is given cannot corrupt the next one.

// fxThinkingStep is one narrated step, numbered so ordering is checkable.
func fxThinkingStep(n int) string {
	return "Checking the pod status for replica " + string(rune('a'+n%26))
}

// fxThinkingSteps builds n distinct steps.
func fxThinkingSteps(n int) []string {
	out := make([]string, 0, n)
	for i := range n {
		out = append(out, fxThinkingStep(i))
	}
	return out
}

// fxLongStep is a single step past the per-line clip, standing in for a first
// pass that quoted an entire manifest into its own narration.
func fxLongStep() string { return strings.Repeat("x", 600) }

// fxIntelligence is a first pass that answered.
func fxIntelligence() *runs.Intelligence {
	return &runs.Intelligence{
		RequestID:     "req-7f3a",
		Analysis:      "The pod is in CrashLoopBackOff because the container exits immediately.",
		ThinkingCount: 3,
		DurationMs:    1234,
		Thinking:      []string{"Listing pods in devtroncd", "Reading pod pgvector-6cccdcb6f6", "Fetching recent events"},
	}
}

// fxIntelligenceFailed is a first pass that errored. The run survives this,
// so it has to render as an explicit failure rather than as silence.
func fxIntelligenceFailed() *runs.Intelligence {
	return &runs.Intelligence{
		RequestID: "req-0000",
		Failed:    "upstream returned 502",
	}
}

// fxIntelligenceEmpty is the awkward middle case: it answered, with nothing.
func fxIntelligenceEmpty() *runs.Intelligence {
	return &runs.Intelligence{RequestID: "req-empty"}
}

// fxIntelligenceUnicode exercises multi-byte content through the clip, which
// is where a naive byte slice corrupts the output.
func fxIntelligenceUnicode() *runs.Intelligence {
	return &runs.Intelligence{
		Analysis:      "日本語の分析：コンテナが起動しません。",
		ThinkingCount: 2,
		Thinking:      []string{"ポッドの状態を確認中", "イベントを取得中 🚨"},
	}
}
