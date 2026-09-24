package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/artifact"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	adktool "google.golang.org/adk/tool"
	"google.golang.org/adk/tool/skilltoolset"
	"google.golang.org/genai"

	"github.com/devtron-labs/devtron-sre-agent/internal/knowledge"
	"github.com/devtron-labs/devtron-sre-agent/internal/runs"
	"github.com/devtron-labs/devtron-sre-agent/internal/tools"
	"github.com/devtron-labs/devtron-sre-agent/prompts"
)

// appName identifies this application to ADK's session store.
const appName = "devtron-sre-agent"

// The two agents. There are exactly two, they run in order, and they speak
// only through session state: judge writes "verdict", sre reads it.
const agentSRE = "sre"

// sreToolNames is the allowlist bound to the SRE agent. The judge is bound
// nothing at all, on purpose: it grades an argument against a fact pack it
// already has, and giving it tools would turn a two-second step into a
// second investigation.
//
// Exported through SREToolNames so the API can describe the real harness
// rather than the UI carrying a hand-copied list that drifts.
var sreToolNames = []string{
	"k8s.list", "k8s.get", "k8s.events",
	"k8s.scrape_config",
	"prom.metrics", "prom.discover_for", "prom.query", "prom.query_range", "prom.rules",
	"alerts.list",
	"knowledge.identify",
	"devtron.app_context",
}

// Loader exposes this pipeline's agent through ADK's own registry.
//
// `agent.Loader` is how everything else in ADK asks "what agents are there,
// and give me one by name" — adkrest takes one, the A2A server takes one, and
// a config-driven builder would populate one. Building the agent and then
// wrapping it costs nothing and means we are not the only thing in the
// process that knows how to find it.
func (p *Pipeline) Loader(ctx context.Context, deps *tools.Deps, guard *Guard) (agent.Loader, error) {
	root, err := p.build(ctx, deps, guard)
	if err != nil {
		return nil, err
	}
	return agent.NewSingleLoader(root), nil
}

// Pipeline builds and runs the agent.
type Pipeline struct {
	Registry *tools.Registry
	Models   *ModelFactory
	// Skills is ADK's skill toolset over the knowledge packs. It advertises
	// each component's one-line description on every request and loads the
	// full document only when the model asks for it.
	Skills *skilltoolset.SkillToolset
	// Sessions persists agent state so a restart can resume a run.
	Sessions session.Service
	Budget   Budget
}

// Input is everything the agents are given. Nothing else reaches them.
type Input struct {
	RunID        string
	UserID       string
	Alert        any
	Scope        any
	Facts        map[string]any
	Intelligence string
}

// Output is what the pipeline produces.
type Output struct {
	Verdict json.RawMessage
	Report  json.RawMessage
	Usage   runs.Usage
	Status  string
}

// Run executes judge then sre and returns their structured outputs.
func (p *Pipeline) Run(ctx context.Context, in Input, deps *tools.Deps, ledger LedgerFunc) (Output, error) {
	budget := &Budget{MaxToolCalls: p.Budget.MaxToolCalls, MaxModelTokens: p.Budget.MaxModelTokens}
	guard := NewGuard(budget, ledger, deps.RedactString)

	// Through ADK's loader rather than straight off build(): the runner takes
	// the root agent either way, but going through the registry is what lets
	// adkrest and the A2A server serve the same agent without a second path
	// to construct it.
	loader, err := p.Loader(ctx, deps, guard)
	if err != nil {
		return Output{Status: runs.StatusFailed}, err
	}
	root := loader.RootAgent()

	sessSvc := p.Sessions
	if sessSvc == nil {
		sessSvc = session.InMemoryService()
	}
	r, err := runner.New(runner.Config{
		AppName:         appName,
		Agent:           root,
		SessionService:  sessSvc,
		ArtifactService: artifact.InMemoryService(),
	})
	if err != nil {
		return Output{Status: runs.StatusFailed}, fmt.Errorf("runner: %w", err)
	}

	userID := in.UserID
	if userID == "" {
		userID = "platform"
	}
	state := map[string]any{
		"alert":        in.Alert,
		"scope":        in.Scope,
		"facts":        in.Facts,
		"intelligence": in.Intelligence,
	}
	if _, err := sessSvc.Create(ctx, &session.CreateRequest{
		AppName: appName, UserID: userID, SessionID: in.RunID, State: state,
	}); err != nil {
		return Output{Status: runs.StatusFailed}, fmt.Errorf("create session: %w", err)
	}

	msg := genai.NewContentFromText(
		"Investigate this alert. Follow your instructions exactly and return only the JSON object they specify.",
		genai.RoleUser)

	out := Output{Status: runs.StatusSucceeded}
	var current string
	for ev, err := range r.Run(ctx, userID, in.RunID, msg, agent.RunConfig{}) {
		if err != nil {
			if errors.Is(err, ErrBudgetExceeded) || guard.Exceeded() {
				out.Status = runs.StatusBudgetExceeded
				break
			}
			if tools.IsCanceled(err) || ctx.Err() != nil {
				out.Usage = usageOf(budget)
				return out, ctx.Err()
			}
			_, _ = ledger(context.WithoutCancel(ctx), runs.EvError, current,
				map[string]any{"error": err.Error()})
			out.Status = runs.StatusFailed
			out.Usage = usageOf(budget)
			return out, fmt.Errorf("agent run: %w", err)
		}
		if ev == nil {
			continue
		}
		if ev.Author != "" && ev.Author != current && ev.Author != "user" {
			if current != "" {
				_, _ = ledger(ctx, runs.EvAgentEnd, current, map[string]any{"agent": current})
			}
			current = ev.Author
			_, _ = ledger(ctx, runs.EvAgentStart, current, map[string]any{"agent": current})
		}
	}
	if guard.Exceeded() {
		out.Status = runs.StatusBudgetExceeded
	}
	if current != "" {
		_, _ = ledger(ctx, runs.EvAgentEnd, current, map[string]any{"agent": current})
	}

	// Read the agents' outputs back out of session state.
	got, err := sessSvc.Get(ctx, &session.GetRequest{AppName: appName, UserID: userID, SessionID: in.RunID})
	if err != nil || got == nil || got.Session == nil {
		out.Usage = usageOf(budget)
		return out, fmt.Errorf("read session state: %w", err)
	}
	// One agent, one object. It is split back into verdict and report here
	// because that is how the API, the database and every panel already read
	// it — the merge is in how the answer is produced, not in how it is
	// stored.
	out.Verdict, out.Report = splitAnalysis(structured(got.Session.State(), "analysis"))

	if out.Report != nil {
		_, _ = ledger(ctx, runs.EvFinding, agentSRE, map[string]any{"kind": "report"})
	}
	if out.Status == runs.StatusSucceeded && out.Report == nil {
		// The tree ran but produced nothing usable. Succeeding here would be
		// a lie the UI would faithfully render.
		out.Status = runs.StatusFailed
	}
	out.Usage = usageOf(budget)
	return out, nil
}

// build assembles the one agent this pipeline runs.
//
// It used to be a sequentialagent over a judge and an SRE. The judge existed
// to grade Devtron's analysis cheaply before spending the strong model, and
// in practice it bought nothing: its verdict was almost never settled enough
// to skip the dive, so every run paid for two models to produce one answer,
// and a run that died before the second agent showed two stages that had
// never started.
//
// One agent now verifies and remediates in a single pass, emitting both in
// one object. That deletes the tree, the second model, the skip callback and
// the state handoff between them — and the output is unchanged, because the
// verdict was always going to be read next to the report anyway.
func (p *Pipeline) build(ctx context.Context, deps *tools.Deps, guard *Guard) (agent.Agent, error) {
	strong, err := p.Models.Get(ctx, "strong")
	if err != nil {
		return nil, fmt.Errorf("sre model: %w", err)
	}

	var bound []adktool.Tool
	var allowed []string
	for _, name := range p.Registry.Select(sreToolNames) {
		t, ok := p.Registry.Get(name)
		if !ok {
			continue
		}
		bt, err := t.Bind(deps)
		if err != nil {
			return nil, fmt.Errorf("bind %s: %w", name, err)
		}
		bound = append(bound, bt)
		allowed = append(allowed, bt.Name())
	}
	var toolsets []adktool.Toolset
	if p.Skills != nil {
		toolsets = append(toolsets, p.Skills)
		// The skill toolset's own tools are legitimate, so the guard must not
		// refuse them. It still meters every call against the budget.
		if ts, err := p.Skills.Tools(nil); err == nil {
			for _, t := range ts {
				allowed = append(allowed, t.Name())
			}
		}
	}
	guard.Allow(agentSRE, allowed)

	return llmagent.New(llmagent.Config{
		Name:                     agentSRE,
		Description:              "Verifies Devtron's first pass against the facts and writes expert remediation.",
		Model:                    strong,
		Tools:                    bound,
		Toolsets:                 toolsets,
		InstructionProvider:      instruction(prompts.SRE),
		OutputKey:                "analysis",
		DisallowTransferToParent: true,
		DisallowTransferToPeers:  true,
		BeforeToolCallbacks:      []llmagent.BeforeToolCallback{guard.BeforeTool(agentSRE)},
		AfterToolCallbacks:       []llmagent.AfterToolCallback{guard.AfterTool(agentSRE)},
		BeforeModelCallbacks:     []llmagent.BeforeModelCallback{guard.BeforeModel(agentSRE)},
		AfterModelCallbacks:      []llmagent.AfterModelCallback{guard.AfterModel(agentSRE)},
	})
}

func quote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}

// splitAnalysis pulls the two halves out of the single object the agent
// emits. A missing or unparseable verdict is not fatal: the remediation is
// the part someone acts on, and withholding it because the grading half was
// malformed would be the wrong trade.
func splitAnalysis(raw json.RawMessage) (verdict, report json.RawMessage) {
	if len(raw) == 0 {
		return nil, nil
	}
	var both struct {
		Verdict json.RawMessage `json:"verdict"`
		Report  json.RawMessage `json:"report"`
	}
	if err := json.Unmarshal(raw, &both); err != nil {
		return nil, nil
	}
	if len(both.Report) == 0 {
		// An agent that emitted a bare report rather than the wrapper still
		// produced something usable.
		return both.Verdict, raw
	}
	return both.Verdict, both.Report
}

// instruction renders a prompt template against session state at invocation
// time, so the sre agent sees the judge's verdict even though the tree was
// built before the judge ran.
func instruction(tmpl string) llmagent.InstructionProvider {
	return func(rc agent.ReadonlyContext) (string, error) {
		state := map[string]any{}
		for k, v := range rc.ReadonlyState().All() {
			state[k] = v
		}
		return Render(tmpl, state), nil
	}
}

var placeholder = regexp.MustCompile(`\{([a-zA-Z_][a-zA-Z0-9_]*)\??\}`)

// Render substitutes {key} with the pretty-printed JSON of state[key].
// A missing key becomes an explicit marker rather than an empty string: a
// silently blank section reads to a model as "nothing was found", which is
// the exact confusion this whole product exists to prevent.
func Render(tmpl string, state map[string]any) string {
	return placeholder.ReplaceAllStringFunc(tmpl, func(m string) string {
		key := strings.Trim(m, "{}?")
		v, ok := state[key]
		if !ok || v == nil {
			return "(not available for this run)"
		}
		if s, isStr := v.(string); isStr {
			if strings.TrimSpace(s) == "" {
				return "(empty)"
			}
			return s
		}
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	})
}

// structured pulls an agent's output out of session state and normalises it
// to JSON. Models fence JSON in markdown often enough that stripping it here
// is cheaper than failing the run over formatting.
func structured(st session.State, key string) json.RawMessage {
	v, err := st.Get(key)
	if err != nil || v == nil {
		return nil
	}
	switch t := v.(type) {
	case string:
		return extractJSON(t)
	case json.RawMessage:
		return t
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return nil
		}
		return b
	}
}

var fence = regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*\\})\\s*```")

func extractJSON(s string) json.RawMessage {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if json.Valid([]byte(s)) && strings.HasPrefix(s, "{") {
		return json.RawMessage(s)
	}
	if m := fence.FindStringSubmatch(s); len(m) == 2 && json.Valid([]byte(m[1])) {
		return json.RawMessage(m[1])
	}
	// Last resort: the outermost braces.
	if i, j := strings.Index(s, "{"), strings.LastIndex(s, "}"); i >= 0 && j > i {
		if cand := s[i : j+1]; json.Valid([]byte(cand)) {
			return json.RawMessage(cand)
		}
	}
	return nil
}

func usageOf(b *Budget) runs.Usage {
	return runs.Usage{
		ToolCalls: b.ToolCalls.Load(), ModelCalls: b.ModelCalls.Load(),
		ModelTokens:  b.ModelTokens.Load(),
		MaxToolCalls: b.MaxToolCalls, MaxModelTokens: b.MaxModelTokens,
	}
}

// KnowledgeCatalog is re-exported so wiring code does not import the
// knowledge package just to hand it to tools.Deps.
type KnowledgeCatalog = knowledge.Catalog

// SREToolNames is the allowlist the SRE agent is bound to, in order. It is a
// copy: the harness endpoint must not be able to widen what an agent may call.
func SREToolNames() []string {
	out := make([]string, len(sreToolNames))
	copy(out, sreToolNames)
	return out
}

// AgentName is the one agent this pipeline runs.
func AgentName() string { return agentSRE }
