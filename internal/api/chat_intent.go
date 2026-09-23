package api

import (
	"fmt"
	"regexp"
	"strings"
)

// intent is what a chat message is actually asking for.
//
// The first version of this endpoint sent everything to Devtron Intelligence,
// which meant "what did my last run find?" and "how do you decide to skip the
// deep dive?" both went to a cluster-debugging agent that knows neither. Those
// answers are already in this process; routing to them is cheaper, faster and
// correct, and it leaves /intelligence for the thing it is good at.
type intent int

const (
	// intentCluster asks about the cluster itself. Devtron's first pass.
	intentCluster intent = iota
	// intentRuns asks about what this agent has already done.
	intentRuns
	// intentPlatform asks how the agent itself works.
	intentPlatform
	// intentPropose asks for something to be investigated properly.
	intentPropose
)

func (i intent) String() string {
	switch i {
	case intentRuns:
		return "runs"
	case intentPlatform:
		return "platform"
	case intentPropose:
		return "propose"
	default:
		return "cluster"
	}
}

var (
	// Questions about a decision this agent already made. Checked before
	// everything else because they contain the same vocabulary as a request
	// for work — "why did you skip the deep dive" is a question about the
	// harness, not an instruction to go and do a deep dive.
	reAboutSelf = regexp.MustCompile(`(?i)\b(why did you|why didn.t you|how do you (decide|choose|know)|what do you do when)\b`)

	// An explicit request to go and investigate, rather than to be told
	// something. These earn a run rather than an answer.
	rePropose = regexp.MustCompile(`(?i)\b(investigate|debug|dig into|look into|root cause|rca|find out why|deep dive|run an? (investigation|analysis))\b`)

	// Questions about this agent's own history.
	reRuns = regexp.MustCompile(`(?i)\b(last run|recent runs?|previous runs?|run history|what did (you|the run) find|earlier (run|investigation)|past runs?|how many runs?)\b`)

	// Questions about the agent itself rather than the cluster.
	rePlatform = regexp.MustCompile(`(?i)\b(how do you work|how does (this|it) work|what can you do|who are you|what are you|` +
		`your (tools|prompt|agents?|models?|architecture)|(which|what) (tools?|models?|agents?)|` +
		`permissions?|api token|scopes?|read[- ]only|adk|harness|architecture|pipeline stages?)\b`)
)

// classify picks a route for one message.
//
// Deliberately regex rather than a model call. A router that costs a second
// and a token budget before the real work starts is a router that makes the
// product feel slower than it is, and the categories here are separated by
// vocabulary that people genuinely use — not by nuance.
//
// Order matters: an explicit "investigate X" wins over everything, because it
// is a request for action and the others are requests for information.
func classify(msg string) intent {
	switch {
	case reAboutSelf.MatchString(msg):
		return intentPlatform
	case rePropose.MatchString(msg):
		return intentPropose
	case reRuns.MatchString(msg):
		return intentRuns
	case rePlatform.MatchString(msg):
		return intentPlatform
	default:
		return intentCluster
	}
}

// platformAnswer is composed from the same constants the harness endpoint
// serves, so the chat cannot drift from the About page.
func platformAnswer(msg string, fast, strong string, maxToolCalls int, toolCount int) string {
	var b strings.Builder

	switch {
	case regexp.MustCompile(`(?i)\b(permissions?|api token|scopes?|read[- ]only)\b`).MatchString(msg):
		b.WriteString("**Everything I do is a read.** There are no write tools and none can be added.\n\n")
		b.WriteString("The Devtron API token needs *view* on:\n\n")
		b.WriteString("- Clusters and environments you want investigated\n")
		b.WriteString("- Devtron apps and Helm releases in those environments\n")
		b.WriteString("- Kubernetes resources — 16 kinds, from Pod and Deployment through to ServiceMonitor\n")
		b.WriteString("- The cluster proxy, to reach Prometheus/VictoriaMetrics and Alertmanager/vmalert\n")
		b.WriteString("- The Athena proxy, for Devtron Intelligence\n\n")
		b.WriteString("`Secret` is never read. Neither is any write, patch, delete, scale, restart or exec — ")
		b.WriteString("the Kubernetes proxy is used GET-only.\n\n")
		b.WriteString("Settings → Configuration has the full list, endpoint by endpoint.")

	case regexp.MustCompile(`(?i)\b(tools?|adk|agents?|architecture|harness|pipeline|model)\b`).MatchString(msg):
		fmt.Fprintf(&b, "A run is six steps. Three are deterministic and happen before any model is called — ")
		b.WriteString("a preflight that refuses unreadable clusters, monitoring discovery, and a fact pack ")
		b.WriteString("gathered from the orchestrator.\n\n")
		b.WriteString("Then **Devtron Intelligence** takes the first pass, and two agents follow it:\n\n")
		fmt.Fprintf(&b, "1. **judge** (`%s`) — grades that analysis against the facts. No tools at all, on purpose: "+
			"grading an argument against a fact pack it already holds does not need a cluster.\n", fast)
		fmt.Fprintf(&b, "2. **sre** (`%s`) — goes deeper where the judge found gaps and writes remediation. "+
			"%d read-only tools, capped at %d calls.\n\n", strong, toolCount, maxToolCalls)
		b.WriteString("They never call each other. The judge writes `verdict` into ADK session state and the SRE ")
		b.WriteString("reads it — that is the entire protocol between them.\n\n")
		b.WriteString("Settings → About the agent has the whole thing as YAML, including both system prompts.")

	case regexp.MustCompile(`(?i)\bwhy did you skip\b`).MatchString(msg):
		b.WriteString("The deep dive is skipped when the judge contradicted nothing and asked for no further ")
		b.WriteString("checks. That is a decision, not a failure — the run still succeeds and the report says so.\n\n")
		b.WriteString("It runs whenever a claim is contradicted, or the judge names a check worth doing. ")
		b.WriteString("Gaps and unverifiable claims on their own do not trigger it: no amount of further digging ")
		b.WriteString("in the cluster settles those, so they belong in `unknowns`.")

	default:
		b.WriteString("I am a second-layer SRE for Devtron. Devtron Intelligence debugs first; I check that ")
		b.WriteString("answer against deterministic facts and add remediation worth acting on.\n\n")
		b.WriteString("Two ways to use me:\n\n")
		b.WriteString("- **Ask me things here.** Nothing is recorded; close the panel and it is gone.\n")
		b.WriteString("- **Trigger a run** on an alert. That produces an auditable record — ledger, verdict, ")
		b.WriteString("report — that stays in your history.\n\n")
		b.WriteString("I can also tell you about recent runs, my tools, my prompts and exactly what my API ")
		b.WriteString("token is allowed to touch.")
	}

	return b.String()
}
