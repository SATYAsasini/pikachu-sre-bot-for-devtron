package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	adktool "google.golang.org/adk/tool"
	"google.golang.org/genai"

	"github.com/devtron-labs/devtron-sre-agent/internal/runs"
)

// ErrBudgetExceeded stops a run when its tool-call or token budget is spent.
var ErrBudgetExceeded = errors.New("budget exceeded")

// Budget tracks consumption against limits.
type Budget struct {
	MaxToolCalls   int
	MaxModelTokens int
	ToolCalls      atomic.Int64
	ModelCalls     atomic.Int64
	ModelTokens    atomic.Int64
}

// Usage renders the counters for runs.usage.
func (b *Budget) Usage() map[string]any {
	return map[string]any{
		"toolCalls": b.ToolCalls.Load(), "modelCalls": b.ModelCalls.Load(), "modelTokens": b.ModelTokens.Load(),
		"maxToolCalls": b.MaxToolCalls, "maxModelTokens": b.MaxModelTokens,
	}
}

// LedgerFunc appends to the run's evidence ledger and returns the sequence
// number, which findings cite as [ev:N]. It mirrors runs.LedgerFunc so the
// agents package never imports storage.
type LedgerFunc func(ctx context.Context, typ, agent string, payload any) (int, error)

// Guard is the third read-only layer plus budget metering and audit. Every
// tool call and model call in the run passes through it.
type Guard struct {
	budget *Budget
	ledger LedgerFunc
	redact func(string) string

	exceeded atomic.Bool

	mu      sync.Mutex
	allow   map[string]map[string]bool // agent -> ADK tool name -> allowed
	seen    map[string]int             // tool+args -> first ledger seq
	skipped map[string]string          // agent -> why it was not run
}

// NoteSkipped records that an agent was deliberately not run. Worth a ledger
// entry: a missing agent should read as a decision, not as something that
// silently failed.
//
// The reason is also kept in memory, because the pipeline has to tell the two
// cases apart afterwards. A run whose deep dive was skipped has no report in
// session state, and so does a run whose deep dive crashed — treating both as
// "no report" is what marked successful investigations as failed.
func (g *Guard) NoteSkipped(agentName, why string) {
	g.mu.Lock()
	if g.skipped == nil {
		g.skipped = map[string]string{}
	}
	g.skipped[agentName] = why
	g.mu.Unlock()
	_, _ = g.ledger(context.Background(), runs.EvAgentSkipped, agentName, map[string]any{
		"agent": agentName, "reason": why,
	})
}

// SkippedReason returns why an agent was deliberately not run, and whether it
// was skipped at all.
func (g *Guard) SkippedReason(agentName string) (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	why, ok := g.skipped[agentName]
	return why, ok
}

// Exceeded reports whether the budget was spent during the run.
func (g *Guard) Exceeded() bool { return g.exceeded.Load() }

// markExceeded flips the flag once and records why. After this, tool calls
// are refused and model calls are short-circuited so the tree unwinds fast
// without spending more; the investigator turns the flag into the run status.
func (g *Guard) markExceeded(ctx context.Context, agentName, what string) {
	if g.exceeded.CompareAndSwap(false, true) {
		_, _ = g.ledger(ctx, runs.EvBudget, agentName, map[string]any{"exceeded": what, "usage": g.budget.Usage()})
	}
}

const budgetStopText = "Budget exceeded. Stop: do not call more tools; the run is being finalized."

// NewGuard builds a guard for one run.
func NewGuard(b *Budget, ledger LedgerFunc, redact func(string) string) *Guard {
	if redact == nil {
		redact = func(s string) string { return s }
	}
	return &Guard{
		budget: b, ledger: ledger, redact: redact,
		allow: map[string]map[string]bool{}, seen: map[string]int{}, skipped: map[string]string{},
	}
}

// Allow records which ADK tool names an agent may call.
func (g *Guard) Allow(agentName string, adkToolNames []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	set := map[string]bool{}
	for _, n := range adkToolNames {
		set[n] = true
	}
	g.allow[agentName] = set
}

// BeforeTool enforces the allowlist, the budget and the no-identical-repeat
// rule, and writes the call to the ledger before it executes.
func (g *Guard) BeforeTool(agentName string) llmagent.BeforeToolCallback {
	return func(ctx agent.ToolContext, t adktool.Tool, args map[string]any) (map[string]any, error) {
		name := t.Name()
		g.mu.Lock()
		allowed := g.allow[agentName][name] || name == "exit_loop"
		g.mu.Unlock()
		if !allowed {
			_, _ = g.ledger(ctx, runs.EvError, agentName, map[string]any{"error": "tool not allowed", "tool": name})
			return map[string]any{"error": fmt.Sprintf("tool %s is not available to agent %s", name, agentName)}, nil
		}
		if g.exceeded.Load() {
			return map[string]any{"error": budgetStopText}, nil
		}
		if g.budget.MaxToolCalls > 0 && g.budget.ToolCalls.Load() >= int64(g.budget.MaxToolCalls) {
			g.markExceeded(ctx, agentName, "toolCalls")
			return map[string]any{"error": budgetStopText}, nil
		}
		key := name + ":" + canonical(args)
		g.mu.Lock()
		if seq, dup := g.seen[key]; dup && name != "exit_loop" {
			g.mu.Unlock()
			return map[string]any{"error": fmt.Sprintf("identical call to %s was already made; use its result [ev:%d] or change the arguments", name, seq)}, nil
		}
		g.mu.Unlock()

		g.budget.ToolCalls.Add(1)
		seq, err := g.ledger(ctx, runs.EvToolCall, agentName, map[string]any{"tool": name, "args": g.redactArgs(args)})
		if err != nil {
			return nil, fmt.Errorf("ledger: %w", err)
		}
		g.mu.Lock()
		g.seen[key] = seq
		g.mu.Unlock()
		// The arguments are NOT modified here. ADK validates them against the
		// tool's JSON schema before dispatch, so an extra property makes every
		// call fail. The ledger sequence the model cites is attached to the
		// result instead, in AfterTool.
		return nil, nil
	}
}

// AfterTool records the result summary and strips the internal seq argument.
func (g *Guard) AfterTool(agentName string) llmagent.AfterToolCallback {
	return func(ctx agent.ToolContext, t adktool.Tool, args, result map[string]any, err error) (map[string]any, error) {
		payload := map[string]any{"tool": t.Name()}
		outcome := "ok"
		if err != nil {
			payload["error"] = err.Error()
			outcome = "error"
		} else if result != nil {
			if e, ok := result["error"]; ok && e != nil {
				outcome = "error"
			}
		}
		payload["outcome"] = outcome
		if result != nil {
			if s, ok := result["summary"].(string); ok {
				payload["summary"] = g.redact(s)
			}
			if tr, ok := result["truncated"].(bool); ok && tr {
				payload["truncated"] = true
			}
			if e, ok := result["error"]; ok && e != nil {
				payload["toolError"] = e
			}
			if ref, ok := result["artifactRef"].(string); ok && ref != "" {
				payload["artifactRef"] = ref
			}
		}
		seq, lerr := g.ledger(ctx, runs.EvToolResult, agentName, payload)
		if lerr == nil && result != nil {
			// The model cites this seq when it uses the result.
			result["ev"] = seq
		}
		return nil, nil
	}
}

// BeforeModel stops the run when the token budget is spent.
func (g *Guard) BeforeModel(agentName string) llmagent.BeforeModelCallback {
	return func(ctx agent.CallbackContext, req *model.LLMRequest) (*model.LLMResponse, error) {
		if g.budget.MaxModelTokens > 0 && g.budget.ModelTokens.Load() >= int64(g.budget.MaxModelTokens) {
			g.markExceeded(ctx, agentName, "modelTokens")
		}
		if g.exceeded.Load() {
			return &model.LLMResponse{
				Content:      &genai.Content{Role: string(genai.RoleModel), Parts: []*genai.Part{{Text: budgetStopText}}},
				TurnComplete: true, FinishReason: genai.FinishReasonStop,
			}, nil
		}
		g.budget.ModelCalls.Add(1)
		return nil, nil
	}
}

// AfterModel accounts tokens and logs a redacted preview.
func (g *Guard) AfterModel(agentName string) llmagent.AfterModelCallback {
	return func(ctx agent.CallbackContext, resp *model.LLMResponse, respErr error) (*model.LLMResponse, error) {
		payload := map[string]any{}
		if respErr != nil {
			payload["error"] = respErr.Error()
		}
		if resp != nil {
			if resp.UsageMetadata != nil {
				g.budget.ModelTokens.Add(int64(resp.UsageMetadata.TotalTokenCount))
				payload["tokens"] = resp.UsageMetadata.TotalTokenCount
			}
			if resp.Partial {
				return nil, nil
			}
			var calls []string
			var text strings.Builder
			if resp.Content != nil {
				for _, p := range resp.Content.Parts {
					if p.FunctionCall != nil {
						calls = append(calls, p.FunctionCall.Name)
					}
					if p.Text != "" {
						text.WriteString(p.Text)
					}
				}
			}
			if len(calls) > 0 {
				payload["functionCalls"] = calls
			}
			if t := text.String(); t != "" {
				preview := g.redact(t)
				if len(preview) > 600 {
					preview = preview[:600] + "…"
				}
				payload["text"] = preview
			}
			payload["model"] = resp.ModelVersion
		}
		_, _ = g.ledger(ctx, runs.EvModelCall, agentName, payload)
		return nil, nil
	}
}

func (g *Guard) redactArgs(args map[string]any) map[string]any {
	out := make(map[string]any, len(args))
	for k, v := range args {
		if s, ok := v.(string); ok {
			out[k] = g.redact(s)
		} else {
			out[k] = v
		}
	}
	return out
}

func canonical(args map[string]any) string {
	cp := make(map[string]any, len(args))
	for k, v := range args {
		cp[k] = v
	}
	b, _ := json.Marshal(cp) // encoding/json sorts map keys
	return string(b)
}
