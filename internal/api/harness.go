package api

import (
	"net/http"

	"github.com/devtron-labs/devtron-sre-agent/internal/agents"
	"github.com/devtron-labs/devtron-sre-agent/prompts"
)

// harness describes the agent tree the binary actually builds.
//
// It is generated from the same constants the pipeline is assembled from —
// the agent names, the SRE allowlist, the registry's own descriptions, the
// configured models and the live budget — rather than written out by hand in
// the UI. A hand-copied diagram is wrong within a week of the first tool
// being added, and a diagram nobody trusts is worse than none.
func (s *Server) harness(w http.ResponseWriter, _ *http.Request) {
	judge, sre := agents.AgentNames()
	allowed := agents.SREToolNames()

	// Resolve the allowlist against the registry so the response carries each
	// tool's real description and package, and so a name in the allowlist
	// that no longer exists shows up as missing instead of silently vanishing.
	toolRows := make([]map[string]any, 0, len(allowed))
	if s.Worker != nil && s.Worker.Registry != nil {
		reg := s.Worker.Registry
		for _, name := range reg.Select(allowed) {
			row := map[string]any{"name": name}
			if t, ok := reg.Get(name); ok {
				row["package"] = t.Package()
				row["description"] = t.Description()
			}
			toolRows = append(toolRows, row)
		}
	} else {
		for _, name := range allowed {
			toolRows = append(toolRows, map[string]any{"name": name})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"adk": map[string]any{
			"module":  "google.golang.org/adk",
			"root":    map[string]any{"name": "investigation", "type": "sequentialagent"},
			"session": "session/database (postgres)",
			// The pieces of ADK this binary actually leans on. Listing what we
			// use is more honest than listing what the library offers.
			"uses": []string{
				"agent/llmagent",
				"agent/workflowagents/sequentialagent",
				"runner",
				"session/database",
				"artifact",
				"tool/functiontool",
				"tool/skilltoolset",
			},
		},
		"agents": []map[string]any{
			{
				"name":  judge,
				"order": 1,
				"model": s.Cfg.Models.Fast,
				"role":  "Grades Devtron Intelligence's analysis against the fact pack and names the failing component.",
				// Deliberate: grading an argument against a fact pack it
				// already holds does not need a cluster, and tools would turn
				// a two-second step into a second investigation.
				"tools":     []string{},
				"outputKey": "verdict",
				"reads":     []string{"alert", "scope", "facts", "intelligence"},
				// The prompt is the product, so it is shown rather than
				// described. It is the file the binary embedded, not a copy.
				"instruction": prompts.Judge,
			},
			{
				"name":        sre,
				"order":       2,
				"model":       s.Cfg.Models.Strong,
				"role":        "Goes deeper where the judge found gaps and writes expert remediation.",
				"tools":       toolRows,
				"toolsets":    []string{"skilltoolset (knowledge packs, progressive disclosure)"},
				"outputKey":   "report",
				"reads":       []string{"alert", "scope", "facts", "intelligence", "verdict"},
				"instruction": prompts.SRE,
				// The stage that can be skipped, and why.
				"skippedWhen": "the judge returns supported, contradicts nothing and asks for no further checks",
			},
		},
		"budget": map[string]any{
			"maxToolCalls":   s.Cfg.Run.MaxToolCalls,
			"maxModelTokens": s.Cfg.Run.MaxModelTokens,
			"timeoutSeconds": s.Cfg.Run.TimeoutSeconds,
		},
		// The two ways in, and why they are different. This is a product
		// decision people ask about, so it is answered here rather than left
		// to be inferred from the absence of a history row.
		"entrypoints": []map[string]any{
			{
				"name":    "Investigation",
				"trigger": "An alert, or an alert plus per-run options",
				"path":    "POST /v1/runs",
				"creates": "A run: id, ledger, verdict, report, a row in history",
				"why":     "The cluster raised it. That is worth an auditable record somebody can read back in six months.",
				"stages":  "All six pipeline steps, both agents, full tool budget",
			},
			{
				"name":    "Chat",
				"trigger": "A free-text question typed into the ask box",
				"path":    "POST /v1/chat",
				"creates": "Nothing. The server is stateless; the browser holds the thread in sessionStorage",
				"why":     "A question somebody wondered aloud is not an investigation. Minting a run for one filled history with rows nobody wanted and could not delete.",
				"stages":  "Devtron Intelligence only — no judge, no SRE agent, no ledger",
			},
		},

		// The steps outside the agent tree. The first three are deterministic
		// and happen before any model call, which is the whole reason the
		// judge can be tool-less.
		"pipeline": []map[string]any{
			{"step": "preflight", "kind": "deterministic", "what": "Refuse the run if the cluster cannot be read."},
			{"step": "discovery", "kind": "deterministic", "what": "Find Prometheus or VictoriaMetrics, Alertmanager or vmalert, per cluster."},
			{"step": "facts", "kind": "deterministic", "what": "Gather the fact pack from the orchestrator before any model call."},
			{"step": "intelligence", "kind": "devtron", "what": "Devtron's own first pass over /proxy/athena/intelligence."},
			{"step": judge, "kind": "agent", "what": "Verify the first pass. No tools."},
			{"step": sre, "kind": "agent", "what": "Deepen it and write remediation. Read-only tools."},
		},
		// How each Devtron API surface became a tool the model can call. This
		// is the part of building an agent that nobody budgets for: a REST
		// endpoint is not a tool, and the gap between them is most of the
		// work below.
		"surfaces": []map[string]any{
			{
				"package": "k8s",
				"api":     "GET /orchestrator/k8s/proxy/… and POST /orchestrator/k8s/resource/list",
				"auth":    "Authorization: Bearer for the proxy, token: header for everything else",
				"tools":   []string{"k8s.list", "k8s.get", "k8s.events", "k8s.scrape_config"},
				"conversion": []string{
					"The proxy is GET-only, so anything that would be a POST had to be expressed as a path.",
					"Responses come back as either objects or as table rows depending on the endpoint; a single decoder promotes rows to objects, because the model cannot be asked to know the difference.",
					"A namespace list can be megabytes. Results are projected to the fields that decide an incident and truncated with the count kept, so 'there are 400' survives even when the 400 do not.",
				},
			},
			{
				"package": "prom",
				"api":     "Prometheus or VictoriaMetrics /api/v1/{query,query_range,rules,metadata,label/__name__/values,series,targets}",
				"auth":    "reached through the same Kubernetes service proxy; never configured, always discovered",
				"tools":   []string{"prom.metrics", "prom.query", "prom.query_range", "prom.rules", "prom.discover_for"},
				"conversion": []string{
					"Two products with one dialect: VictoriaMetrics answers the Prometheus API at three different base paths, so the base is probed rather than configured.",
					"An empty result means 'absent or mislabelled', never zero. The envelope says which, because a model reads an empty array as good news.",
					"prom.discover_for exists because remembered metric names are wrong: it reads the HELP and TYPE this cluster's exporter actually publishes.",
				},
			},
			{
				"package": "alerts",
				"api":     "Alertmanager /api/v2/alerts or vmalert /api/v1/alerts",
				"auth":    "service proxy, discovered per cluster",
				"tools":   []string{"alerts.list"},
				"conversion": []string{
					"Two schemas, one tool: the shapes are normalised into one alert type before the model ever sees them.",
					"Labels are archaeology. Namespace, kind and resource are derived once, centrally, rather than asking the model to guess which label names an object.",
				},
			},
			{
				"package": "devtron",
				"api":     "GET /orchestrator/app/list/v2, /orchestrator/env/autocomplete/helm, /orchestrator/application",
				"auth":    "token: header",
				"tools":   []string{"devtron.app_context"},
				"conversion": []string{
					"Several calls, one tool. The model should ask 'what does Devtron know about this namespace', not orchestrate four endpoints to find out.",
					"cluster/autocomplete returns an empty list for an environment-scoped token, so the cluster set is derived from environments instead. An agent builder cannot discover that; a human had to.",
				},
			},
			{
				"package": "knowledge",
				"api":     "none — local packs read from source at build time",
				"auth":    "n/a",
				"tools":   []string{"knowledge.identify"},
				"conversion": []string{
					"Exposed through ADK's skilltoolset: every pack advertises one line on each request and the full document loads only when asked for.",
					"That is progressive disclosure, and it is the only reason twenty packs fit in a context at all.",
				},
			},
		},

		// What had to be written around ADK. This is the honest answer to
		// "could this be built from a UI?" — the tree could; these could not,
		// yet.
		"orchestration": []map[string]any{
			{"layer": "preflight", "what": "Refuse the run when the cluster cannot be read, before spending a Devtron call and two models on it.", "declarative": false},
			{"layer": "discovery", "what": "Find the monitoring stack per cluster and pick a base path by probing three candidates.", "declarative": false},
			{"layer": "fact pack", "what": "Gather deterministic facts before any model call, so the first agent needs no tools.", "declarative": false},
			{"layer": "guard", "what": "Per-agent tool allowlist, budget metering, identical-call refusal, and a ledger entry per call.", "declarative": true},
			{"layer": "skip", "what": "Skip the deep dive when the judge contradicted nothing and asked for no further checks.", "declarative": true},
			{"layer": "redaction", "what": "Scrub secrets out of every tool argument and result before it reaches a model or the ledger.", "declarative": true},
			{"layer": "envelope", "what": "One result shape for every tool: summary, data, error, truncated, artifact ref.", "declarative": true},
		},

		// What an agent-builder platform would have to standardise before an
		// agent like this could be assembled in a UI rather than written.
		"normalise": []map[string]any{
			{
				"item": "Tool result envelope",
				"why":  "Every tool must answer in one shape, including how it failed and whether it was truncated. Without it each tool teaches the model a new dialect and the prompt absorbs the difference.",
			},
			{
				"item": "Absence versus zero",
				"why":  "The single most common wrong answer in this domain. A platform has to make 'we could not look' a first-class result, or every agent built on it will report unreachable as healthy.",
			},
			{
				"item": "Budget as a contract",
				"why":  "Tool calls, tokens and wall-clock need ceilings the runtime enforces, not instructions the prompt requests. Measured here: the agents, not the upstream call, are the latency.",
			},
			{
				"item": "Evidence identity",
				"why":  "Every claim cites the ledger entry it came from. That requires the runtime to number tool results and hand the number back — not something a prompt can arrange.",
			},
			{
				"item": "Discovery over configuration",
				"why":  "Endpoints differ per cluster and per install. A builder that asks for a Prometheus URL at design time produces an agent that works on exactly one cluster.",
			},
			{
				"item": "Progressive disclosure of knowledge",
				"why":  "Twenty component packs cannot be pasted into a system prompt. The platform needs a first-class way to advertise a catalogue and load one entry on demand.",
			},
			{
				"item": "Conditional stages",
				"why":  "A fixed graph runs every stage every time. Skipping work the previous stage settled is the difference between 90 seconds and four minutes, and it has to be expressible.",
			},
		},

		// What the Devtron API token has to be allowed to do. Kept beside the
		// surfaces it is derived from, because a permission list that lives
		// somewhere else is a permission list that goes stale.
		"token": map[string]any{
			"readOnly": true,
			"scopes": []map[string]any{
				{"area": "Clusters & environments", "permission": "View on every cluster to be investigated"},
				{"area": "Devtron apps", "permission": "View on the apps in those environments"},
				{"area": "Helm apps", "permission": "View on Helm releases in those environments"},
				{"area": "Kubernetes resources", "permission": "View on the listed kinds, in those clusters and namespaces"},
				{"area": "Monitoring", "permission": "Reach Prometheus/VictoriaMetrics and Alertmanager/vmalert through the cluster proxy"},
				{"area": "Devtron Intelligence", "permission": "Access to the Athena proxy"},
			},
			// Every kind the agent ever asks for. Nothing outside it is requested.
			"kinds": []string{
				"Pod", "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job",
				"Service", "Ingress", "Event", "Node", "PersistentVolumeClaim", "ConfigMap",
				"ServiceMonitor", "PodMonitor", "VMServiceScrape", "VMPodScrape",
			},
			"neverRequested": []string{
				"Secret — not read anywhere in the codebase",
				"Any write, patch, delete, scale, restart or exec",
			},
			"placements": []string{
				"token: — every /orchestrator/* call",
				"Authorization: Bearer — /orchestrator/k8s/proxy/* only",
				"Cookie: argocd.token — /proxy/athena/intelligence only",
			},
			"note": "The two POSTs (app/list/v2, k8s/resource/list) are reads that take a body. The Kubernetes proxy is used GET-only.",
		},

		"guarantees": []string{
			"No kubeconfig. Every cluster read goes through the Devtron orchestrator.",
			"Read-only. There are no write tools and none may be added.",
			"Every tool call and model call passes the guard: allowlist, budget, no-identical-repeat, ledger.",
			"The two agents never speak directly; they communicate only through ADK session state.",
		},
	})
}
