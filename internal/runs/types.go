// Package runs owns an investigation's lifecycle: its identity, its status,
// its evidence ledger and the streaming of both to the UI.
//
// The ledger is append-only and is the product's audit trail: every model
// call, every tool call and every event from Devtron's own first-pass
// debugger lands in it with a sequence number, and findings cite those
// numbers as [ev:N].
package runs

import (
	"encoding/json"
	"time"
)

// Status values a run can hold.
const (
	StatusQueued         = "queued"
	StatusRunning        = "running"
	StatusSucceeded      = "succeeded"
	StatusFailed         = "failed"
	StatusCanceled       = "canceled"
	StatusBudgetExceeded = "budget_exceeded"
)

// Terminal reports that no further work will happen on this run.
func Terminal(status string) bool {
	switch status {
	case StatusSucceeded, StatusFailed, StatusCanceled, StatusBudgetExceeded:
		return true
	}
	return false
}

// Event types on the ledger and the SSE stream.
const (
	EvRunQueued            = "run_queued"
	EvStatus               = "status"
	EvIntelligenceStart    = "intelligence_start"
	EvIntelligenceThinking = "intelligence_thinking"
	EvIntelligenceAnalysis = "intelligence_analysis"
	EvAgentStart           = "agent_start"
	EvAgentEnd             = "agent_end"
	EvAgentSkipped         = "agent_skipped"
	EvModelCall            = "model_call"
	EvToolCall             = "tool_call"
	EvToolResult           = "tool_result"
	EvFinding              = "finding"
	EvBudget               = "budget"
	EvError                = "error"
)

// Scope is what the run was pointed at.
type Scope struct {
	ClusterID       int    `json:"clusterId"`
	ClusterName     string `json:"clusterName"`
	EnvironmentID   int    `json:"environmentId,omitempty"`
	EnvironmentName string `json:"environmentName,omitempty"`
	Namespace       string `json:"namespace,omitempty"`
	AppName         string `json:"appName,omitempty"`
	AppType         string `json:"appType,omitempty"` // devtron | helm
}

// Trigger is why the run exists.
type Trigger struct {
	Kind  string          `json:"kind"` // alert | ask
	Alert json.RawMessage `json:"alert,omitempty"`
	Ask   string          `json:"ask,omitempty"`
}

// Intelligence is Devtron's first-pass answer.
type Intelligence struct {
	RequestID     string `json:"requestId,omitempty"`
	Analysis      string `json:"analysis,omitempty"`
	ThinkingCount int    `json:"thinkingCount"`
	// Thinking is the trail of steps Devtron's first pass narrated as it
	// worked. It is kept because it is evidence in its own right: a step
	// records something Devtron actually looked at, which is a stronger thing
	// than the same statement asserted in the final analysis, and it tells
	// our agents what has already been inspected so they do not pay to look
	// again. Capped at ThinkingKept entries.
	Thinking   []string `json:"thinking,omitempty"`
	DurationMs int64    `json:"durationMs"`
	// Failed is set when Devtron's own agent ended in error. The run still
	// continues on deterministic facts alone, which is worth saying out loud.
	Failed string `json:"failed,omitempty"`
}

// Usage is the agent budget consumed.
type Usage struct {
	ToolCalls      int64 `json:"toolCalls"`
	ModelCalls     int64 `json:"modelCalls"`
	ModelTokens    int64 `json:"modelTokens"`
	MaxToolCalls   int   `json:"maxToolCalls"`
	MaxModelTokens int   `json:"maxModelTokens"`
}

// Run is one investigation, shaped exactly as the API serves it.
type Run struct {
	ID         string     `json:"id"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"createdAt"`
	StartedAt  *time.Time `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt"`
	DurationMs int64      `json:"durationMs"`

	Scope   Scope   `json:"scope"`
	Trigger Trigger `json:"trigger"`

	Intelligence *Intelligence   `json:"intelligence"`
	Verdict      json.RawMessage `json:"verdict"`
	Report       json.RawMessage `json:"report"`

	Usage   Usage      `json:"usage"`
	Options RunOptions `json:"options"`
	Error   string     `json:"error"`
}

// Event is one ledger entry.
type Event struct {
	Seq     int             `json:"seq"`
	At      time.Time       `json:"at"`
	Type    string          `json:"type"`
	Agent   string          `json:"agent,omitempty"`
	Payload json.RawMessage `json:"payload"`
}

// CreateRequest is the API's create-run body.
type CreateRequest struct {
	ClusterID       int             `json:"clusterId"`
	ClusterName     string          `json:"clusterName"`
	EnvironmentID   int             `json:"environmentId,omitempty"`
	EnvironmentName string          `json:"environmentName,omitempty"`
	Namespace       string          `json:"namespace,omitempty"`
	AppName         string          `json:"appName,omitempty"`
	AppType         string          `json:"appType,omitempty"`
	ResourceKind    string          `json:"resourceKind,omitempty"`
	ResourceName    string          `json:"resourceName,omitempty"`
	ResourceStatus  string          `json:"resourceStatus,omitempty"`
	Alert           json.RawMessage `json:"alert,omitempty"`
	Ask             string          `json:"ask,omitempty"`
	// Force skips the pre-flight refusal for a cluster with no monitoring, so
	// a Kubernetes-only investigation is still possible when the operator
	// knows that is what they want.
	Force bool `json:"force,omitempty"`

	// Options are the per-run knobs an operator can change before triggering.
	// They tune how far the agent goes and what it may read — never the
	// prompts, which are the product rather than configuration.
	Options RunOptions `json:"options,omitzero"`
}

// ThinkingKept bounds how many of Devtron's narrated steps are stored and
// shown to the agents. A real first pass emits fifty or more, most of them
// restatements; the ones that name what was inspected come early, and the
// tail is what makes the prompt expensive without making it better.
const ThinkingKept = 24

// Depth is how hard the run should try.
type Depth string

const (
	// DepthAuto lets the judge decide whether the deep dive runs at all.
	DepthAuto Depth = "auto"
	// DepthQuick stops after the verdict: first pass plus verification.
	DepthQuick Depth = "quick"
	// DepthDeep always runs the deep dive, even on a settled verdict.
	DepthDeep Depth = "deep"
)

// Source is whether the agent may use a data source.
type Source string

const (
	// SourceAuto uses it when it was discovered and reachable.
	SourceAuto Source = "auto"
	// SourceOn requires it; the run refuses if it is unavailable.
	SourceOn Source = "on"
	// SourceOff withholds it even when present.
	SourceOff Source = "off"
)

// RunOptions are the operator's per-run choices.
type RunOptions struct {
	Depth Depth `json:"depth,omitempty"`
	// MaxToolCalls overrides the budget for this run only. Zero uses config.
	MaxToolCalls int `json:"maxToolCalls,omitempty"`
	// Metrics and Logs gate the corresponding tool families.
	Metrics Source `json:"metrics,omitempty"`
	Logs    Source `json:"logs,omitempty"`

	// MetricsService and AlertsService override discovery for this run,
	// as "namespace/name". Discovery picks one of what are often dozens of
	// candidates by name heuristics; when it picks wrong, the operator needs
	// a way to say so without editing the cluster.
	MetricsService string `json:"metricsService,omitempty"`
	AlertsService  string `json:"alertsService,omitempty"`

	// Window is how far back in time to look, in minutes. Zero uses each
	// tool's own default. An incident that started three hours ago is not
	// visible in a one-hour window, and that is a common way an agent
	// concludes "no signal" about something plainly wrong.
	WindowMinutes int `json:"windowMinutes,omitempty"`
}
