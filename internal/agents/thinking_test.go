package agents

import "testing"

// Adaptive thinking is a 400 from Haiku 4.5 and everything before it. The
// model id is configuration and the request shape is code, so the pairing has
// to be decided rather than assumed — this is the test that keeps the default
// model and the default request shape from drifting apart.
func TestAdaptiveThinkingByModelFamily(t *testing.T) {
	t.Parallel()

	adaptive := []string{
		"claude-opus-5", "claude-sonnet-5", "claude-fable-5-1",
		"claude-opus-4-8", "claude-opus-4-7", "claude-opus-4-6", "claude-sonnet-4-6",
		// Unknown ids are assumed new, because every current model is
		// adaptive and the next one will be too.
		"claude-something-unreleased",
	}
	for _, id := range adaptive {
		if !adaptiveThinking(id) {
			t.Errorf("%s should take adaptive thinking", id)
		}
	}

	legacy := []string{
		"claude-haiku-4-5", "claude-haiku-4-5-20251001", "CLAUDE-HAIKU-4-5",
		"claude-sonnet-4-5", "claude-opus-4-5", "claude-3-opus-20240229",
	}
	for _, id := range legacy {
		if adaptiveThinking(id) {
			t.Errorf("%s predates adaptive thinking and must get a token budget", id)
		}
	}
}

func TestThinkingForShape(t *testing.T) {
	t.Parallel()

	if got := thinkingFor("claude-opus-5", 16000); got.OfAdaptive == nil {
		t.Error("a current model should get the adaptive config")
	}

	got := thinkingFor("claude-haiku-4-5", 16000)
	if got.OfEnabled == nil {
		t.Fatalf("Haiku 4.5 needs an explicit budget, got %+v", got)
	}
	if b := got.OfEnabled.BudgetTokens; b != 8000 {
		t.Errorf("budget %d, want half of max_tokens", b)
	}
}

// budget_tokens must be at least 1024 and strictly below max_tokens. Both
// bounds are the API's, and breaking either fails the whole call rather than
// degrading — so the squeeze cases matter more than the ordinary one.
func TestThinkingBudgetStaysLegal(t *testing.T) {
	t.Parallel()

	for _, maxTokens := range []int64{1, 2, 1024, 1025, 2048, 2049, 4096, 16000, 128000} {
		got := thinkingFor("claude-haiku-4-5", maxTokens)
		if got.OfDisabled != nil {
			if maxTokens > minThinkingBudget {
				t.Errorf("max_tokens=%d has room to think but thinking was turned off", maxTokens)
			}
			continue
		}
		if got.OfEnabled == nil {
			t.Fatalf("max_tokens=%d produced neither a budget nor a disable: %+v", maxTokens, got)
		}
		b := got.OfEnabled.BudgetTokens
		if b < minThinkingBudget {
			t.Errorf("max_tokens=%d: budget %d is below the API floor of %d", maxTokens, b, minThinkingBudget)
		}
		if b >= maxTokens {
			t.Errorf("max_tokens=%d: budget %d must be strictly lower", maxTokens, b)
		}
	}
}

// A zero or negative max_tokens should not produce a negative budget. It
// cannot reach the API — NewAnthropic floors max_tokens — but a helper that
// returns nonsense off the happy path is a trap for the next caller.
func TestThinkingForDegenerateMaxTokens(t *testing.T) {
	t.Parallel()

	for _, maxTokens := range []int64{0, -1, -16000} {
		got := thinkingFor("claude-haiku-4-5", maxTokens)
		if got.OfDisabled == nil {
			t.Errorf("max_tokens=%d should disable thinking, got %+v", maxTokens, got)
		}
	}
}
