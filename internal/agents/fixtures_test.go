package agents

// Deterministic verdict fixtures for the skip decision. Each is a function so
// a test that mutates what it is handed cannot leak into the next one.

// fxVerdict builds a verdict with the given shape.
func fxVerdict(verdict string, claims []string, gaps, nextChecks []string) *settledVerdict {
	v := &settledVerdict{Verdict: verdict, Gaps: gaps, NextChecks: nextChecks}
	for _, st := range claims {
		v.Claims = append(v.Claims, struct {
			Status string `json:"status"`
		}{Status: st})
	}
	return v
}

// fxSettled is the case the skip exists for: the judge agreed and asked for
// nothing more.
func fxSettled() *settledVerdict {
	return fxVerdict("supported", []string{"supported", "supported"}, nil, nil)
}

// fxSettledWithUnverifiable is what a real settled verdict almost always looks
// like. There is nearly always one claim nothing in the cluster can settle,
// and treating that as a reason to run the deep dive is what made the skip
// never fire.
func fxSettledWithUnverifiable() *settledVerdict {
	return fxVerdict("supported", []string{"supported", "unverifiable"}, nil, nil)
}

// fxSettledWithGaps is settled but with gaps noted. Gaps are things the first
// pass never established, not things the judge is asking us to go and check.
func fxSettledWithGaps() *settledVerdict {
	return fxVerdict("supported", []string{"supported"}, []string{"no metrics for the sidecar", "log retention unknown"}, nil)
}

// fxContradicted means the first pass is wrong, which is exactly when the deep
// dive earns its cost.
func fxContradicted() *settledVerdict {
	return fxVerdict("partly_supported", []string{"supported", "contradicted"}, nil, nil)
}

// fxAsksForChecks is the judge explicitly requesting work.
func fxAsksForChecks() *settledVerdict {
	return fxVerdict("supported", []string{"supported"}, nil, []string{"query container_memory_working_set_bytes"})
}
