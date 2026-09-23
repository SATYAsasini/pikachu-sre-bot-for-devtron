package api

import "testing"

// The router is the whole point of this endpoint: before it existed, "what did
// my last run find?" went to a cluster-debugging agent that has never seen a
// run. Each case below is a phrasing a person actually used.
func TestClassify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		msg  string
		want intent
	}{
		// Explicit requests for action win over everything else.
		{"investigate the pgvector crash loop", intentPropose},
		{"can you debug why checkout is slow", intentPropose},
		{"please dig into the scheduler alert", intentPropose},
		{"I want an RCA for this", intentPropose},
		{"find out why the rollout stalled", intentPropose},
		{"run an investigation on payments-api", intentPropose},

		// Our own history. A lookup beats a request for work even when it
		// contains the vocabulary of one — these were all misrouted in the
		// first version and reported from the UI.
		{"list the alerts we already debug", intentRuns},
		{"list the alerts we already debugged", intentRuns},
		{"List the history of our run debug", intentRuns},
		{"show me what we have investigated", intentRuns},
		{"how many alerts have we debugged", intentRuns},
		{"which runs did an rca", intentRuns},
		{"what did my last run find?", intentRuns},
		{"show me recent runs", intentRuns},
		{"how many runs have failed", intentRuns},
		{"anything in the run history for this cluster", intentRuns},

		// Ourselves.
		{"how do you work?", intentPlatform},
		{"what tools do you have", intentPlatform},
		{"what permissions does the api token need", intentPlatform},
		{"is it read-only?", intentPlatform},
		{"which model are you using", intentPlatform},
		{"why did you skip the deep dive", intentPlatform},
		{"tell me about the adk harness", intentPlatform},

		// Everything else is a question about the cluster.
		{"is kube-scheduler down", intentCluster},
		{"why does the pod keep restarting", intentCluster},
		{"what is firing right now", intentCluster},
		{"", intentCluster},
		{"hello", intentCluster},
	}

	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			t.Parallel()
			if got := classify(tt.msg); got != tt.want {
				t.Errorf("classify(%q) = %s, want %s", tt.msg, got, tt.want)
			}
		})
	}
}

// A request for action must beat a request for information when a sentence
// contains both, because acting on the wrong one wastes a run or withholds it.
func TestClassifyPrecedence(t *testing.T) {
	t.Parallel()

	// A bare lookup wins over the vocabulary of work…
	if got := classify("list the alerts we already debugged"); got != intentRuns {
		t.Errorf("want runs for a lookup, got %s", got)
	}
	// …but an explicit instruction with no lookup verb is still a request.
	if got := classify("investigate the last run's root cause properly"); got != intentPropose {
		t.Errorf("want propose for an instruction, got %s", got)
	}
	if got := classify("how do you work, and what did recent runs find"); got != intentRuns {
		t.Errorf("want runs ahead of platform, got %s", got)
	}
}

func TestPlatformAnswerIsAlwaysUseful(t *testing.T) {
	t.Parallel()

	tests := []struct {
		msg      string
		contains string
	}{
		{"what permissions does it need", "read"},
		{"what tools do you have", "judge"},
		{"why did you skip the deep dive", "contradicted"},
		{"who are you", "second-layer SRE"},
	}

	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			t.Parallel()
			got := platformAnswer(tt.msg, "claude-sonnet-5", "claude-opus-5", 12, 12)
			if got == "" {
				t.Fatal("platform answers must never be empty")
			}
			if !contains(got, tt.contains) {
				t.Errorf("want %q in the answer, got:\n%s", tt.contains, got)
			}
		})
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
