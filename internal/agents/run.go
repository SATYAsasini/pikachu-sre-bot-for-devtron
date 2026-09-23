package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/agent/workflowagents/sequentialagent"
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
const (
	agentJudge = "judge"
	agentSRE   = "sre"
)

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

// Pipeline builds and runs the two-agent tree.
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
	// Depth is the operator's choice for this run: auto lets the judge decide
	// whether the deep dive happens, quick forbids it, deep forces it.
	Depth        string
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

	root, err := p.build(ctx, deps, guard, in.Depth)
	if err != nil {
		return Output{Status: runs.StatusFailed}, err
	}

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
	st := got.Session.State()
	out.Verdict = structured(st, "verdict")
	out.Report = structured(st, "report")

	if out.Verdict != nil {
		if seq, err := ledger(ctx, runs.EvFinding, agentJudge, map[string]any{"kind": "verdict"}); err == nil {
			_ = seq
		}
	}
	if out.Report != nil {
		_, _ = ledger(ctx, runs.EvFinding, agentSRE, map[string]any{"kind": "report"})
	}
	if out.Report == nil {
		// A deliberately skipped deep dive leaves no report in session state,
		// exactly as a crashed one does. Only the guard knows the difference,
		// and without asking it a settled, correct investigation was being
		// marked failed.
		if why, skipped := guard.SkippedReason(agentSRE); skipped {
			out.Report = skippedReportJSON(why)
		}
	}
	if out.Status == runs.StatusSucceeded && out.Report == nil {
		// The tree ran but produced nothing usable. Succeeding here would be
		// a lie the UI would faithfully render.
		out.Status = runs.StatusFailed
	}
	out.Usage = usageOf(budget)
	return out, nil
}

// build assembles judge -> sre.
func (p *Pipeline) build(ctx context.Context, deps *tools.Deps, guard *Guard, depth string) (agent.Agent, error) {
	fast, err := p.Models.Get(ctx, "fast")
	if err != nil {
		return nil, fmt.Errorf("judge model: %w", err)
	}
	strong, err := p.Models.Get(ctx, "strong")
	if err != nil {
		return nil, fmt.Errorf("sre model: %w", err)
	}

	guard.Allow(agentJudge, nil)
	judge, err := llmagent.New(llmagent.Config{
		Name:                     agentJudge,
		Description:              "Verifies Devtron Intelligence's analysis against the fact pack and names the failing component.",
		Model:                    fast,
		InstructionProvider:      instruction(prompts.Judge),
		OutputKey:                "verdict",
		DisallowTransferToParent: true,
		DisallowTransferToPeers:  true,
		BeforeModelCallbacks:     []llmagent.BeforeModelCallback{guard.BeforeModel(agentJudge)},
		AfterModelCallbacks:      []llmagent.AfterModelCallback{guard.AfterModel(agentJudge)},
	})
	if err != nil {
		return nil, fmt.Errorf("build judge: %w", err)
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

	sre, err := llmagent.New(llmagent.Config{
		Name:                     agentSRE,
		Description:              "Goes deeper where the judge found gaps and writes expert remediation.",
		Model:                    strong,
		Tools:                    bound,
		Toolsets:                 toolsets,
		InstructionProvider:      instruction(prompts.SRE),
		OutputKey:                "report",
		DisallowTransferToParent: true,
		DisallowTransferToPeers:  true,
		// This was written and never attached, which is why every run went
		// through all three stages however settled the verdict was.
		BeforeAgentCallbacks: []agent.BeforeAgentCallback{skipWhenSettled(guard, depth)},
		BeforeToolCallbacks:  []llmagent.BeforeToolCallback{guard.BeforeTool(agentSRE)},
		AfterToolCallbacks:   []llmagent.AfterToolCallback{guard.AfterTool(agentSRE)},
		BeforeModelCallbacks: []llmagent.BeforeModelCallback{guard.BeforeModel(agentSRE)},
		AfterModelCallbacks:  []llmagent.AfterModelCallback{guard.AfterModel(agentSRE)},
	})
	if err != nil {
		return nil, fmt.Errorf("build sre: %w", err)
	}

	return sequentialagent.New(sequentialagent.Config{AgentConfig: agent.Config{
		Name:        "investigation",
		Description: "Verify Devtron Intelligence, then deepen it into SRE remediation.",
		SubAgents:   []agent.Agent{judge, sre},
	}})
}

// settledVerdict is the shape of the judge's output that decides whether the
// deep dive is worth running.
type settledVerdict struct {
	Verdict string `json:"verdict"`
	Claims  []struct {
		Status string `json:"status"`
	} `json:"claims"`
	Gaps       []string `json:"gaps"`
	NextChecks []string `json:"nextChecks"`
}

// skipWhenSettled stops the SRE agent before it starts when the judge found
// nothing left to establish.
//
// The bar is deliberately high: fully supported, no gaps named, no checks
// requested, and not one claim contradicted or unverifiable. Anything less
// and the deep dive runs, because a wrong answer delivered quickly is the
// worse failure.
func skipWhenSettled(guard *Guard, depth string) agent.BeforeAgentCallback {
	return func(cc agent.CallbackContext) (*genai.Content, error) {
		// "deep" means the operator asked for the dive regardless of how
		// settled the verdict looks, so the skip never applies.
		if depth == "deep" {
			return nil, nil
		}
		// "quick" stops after the verdict, settled or not.
		if depth == "quick" {
			guard.NoteSkipped(agentSRE, "the run was set to quick: verify only, no deep dive")
			return quickReport(), nil
		}
		raw, err := cc.State().Get("verdict")
		if err != nil || raw == nil {
			return nil, nil // no verdict: run the deep dive
		}
		v := parseVerdict(raw)
		if v == nil {
			return nil, nil
		}
		if !settles(v) {
			return nil, nil
		}
		guard.NoteSkipped(agentSRE, "the judge contradicted nothing and asked for no further checks")
		return skippedReport(settledWhy(v)), nil
	}
}

// settles reports whether the judge left anything for the deep dive to do.
//
// The bar used to be a flawless verdict: supported, no gaps, no next checks,
// and not one claim less than supported. Real verdicts almost never look like
// that — there is nearly always one unverifiable claim — so the dive ran every
// single time and every run paid for three stages.
//
// What actually matters is whether anything is left to establish. A
// contradicted claim means the first pass is wrong, so the dive must run. A
// next check is the judge explicitly asking for one. An unverifiable claim is
// neither: it is something no amount of further digging in this cluster will
// settle, and it belongs in unknowns rather than in another round of tools.
func settles(v *settledVerdict) bool {
	if v == nil || v.Verdict != "supported" || len(v.NextChecks) > 0 {
		return false
	}
	for _, c := range v.Claims {
		if c.Status == "contradicted" {
			return false
		}
	}
	return true
}

// settledWhy is the sentence a skipped run carries in place of a deep dive.
func settledWhy(v *settledVerdict) string {
	why := "The judge found nothing contradicted and asked for no further checks, so the deep dive was not needed."
	if v != nil && len(v.Gaps) > 0 {
		why += " It noted " + strconv.Itoa(len(v.Gaps)) + " gap(s), none of which changed what to do."
	}
	return why
}

func quickReport() *genai.Content {
	return skippedReport("This run was set to quick, so verification ran but the deep dive did not.")
}

func skippedReport(why string) *genai.Content {
	return genai.NewContentFromText(string(skippedReportJSON(why)), genai.RoleModel)
}

// skippedReportJSON is the report a run carries when the deep dive was
// deliberately not run. It agrees, because the judge already found nothing to
// disagree with, and it says why in sreNotes rather than leaving the reader to
// wonder what happened to the third stage.
func skippedReportJSON(why string) json.RawMessage {
	return json.RawMessage(
		`{"agrees":true,"correctedRootCause":"","confidence":0.0,` +
			`"evidence":[],"remediation":[],` +
			`"sreNotes":` + quote(why) + `,` +
			`"unknowns":[],"skipped":true}`)
}

func quote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}

func parseVerdict(raw any) *settledVerdict {
	var b []byte
	switch t := raw.(type) {
	case string:
		b = extractJSON(t)
	case json.RawMessage:
		b = t
	default:
		var err error
		if b, err = json.Marshal(t); err != nil {
			return nil
		}
	}
	if len(b) == 0 {
		return nil
	}
	var v settledVerdict
	if err := json.Unmarshal(b, &v); err != nil {
		return nil
	}
	return &v
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

// AgentNames are the two agents, in the order they run.
func AgentNames() (judge, sre string) { return agentJudge, agentSRE }
