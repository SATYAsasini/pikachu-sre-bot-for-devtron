package incidents

import (
	"encoding/json"
	"time"

	"github.com/devtron-labs/devtron-sre-agent/internal/rules"
)

// State is where an alert is in its life.
//
// Three, following GoAlert rather than Keep's six. `pending` and `maintenance`
// are rules about whether something should be shown, not states the thing
// itself is in, and they belong in the matcher.
type State string

const (
	StateFiring       State = "firing"
	StateAcknowledged State = "acknowledged"
	StateResolved     State = "resolved"
)

// Origin records how we came to own this alert.
type Origin string

const (
	// OriginManual: somebody hit debug on it in the live feed.
	OriginManual Origin = "manual"
	// OriginRule: an auto-investigate rule claimed it.
	OriginRule Origin = "rule"
)

// Log entry kinds. The non-events matter as much as the events: "we did not
// open a run because one is already in flight" is the question people ask.
const (
	LogTracked       = "tracked"
	LogSeen          = "seen"
	LogRunStarted    = "run_started"
	LogRunFinished   = "run_finished"
	LogAcked         = "acknowledged"
	LogResolved      = "resolved"
	LogReopened      = "reopened"
	LogNoted         = "noted"
	LogNotified      = "notified"
	LogNotifySkip    = "notify_skipped"
	LogRunSuppressed = "run_suppressed"
)

// Alert is one tracked alert.
type Alert struct {
	ID string `json:"id"`
	// Seq is the human handle: "#42". Assigned by the database on insert and
	// never reused, so it survives a rename, a re-fire and a resolve.
	Seq         int64  `json:"seq"`
	ClusterID   int    `json:"clusterId"`
	ClusterName string `json:"clusterName"`
	DedupKey    string `json:"dedupKey"`

	Name      string            `json:"name"`
	Severity  string            `json:"severity,omitempty"`
	Namespace string            `json:"namespace,omitempty"`
	Kind      string            `json:"kind,omitempty"`
	Resource  string            `json:"resource,omitempty"`
	Summary   string            `json:"summary,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	// Payload is the alert exactly as it arrived, kept because the live one
	// may change or vanish and what we investigated must not.
	Payload json.RawMessage `json:"payload,omitempty"`

	Priority rules.Priority `json:"priority"`
	State    State          `json:"state"`
	Origin   Origin         `json:"origin"`

	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
	// SeenCount is why this table exists: 23 days of the same alert is one
	// row with a count, not thousands.
	SeenCount int `json:"seenCount"`

	AckedBy    string     `json:"ackedBy,omitempty"`
	AckedAt    *time.Time `json:"ackedAt,omitempty"`
	ResolvedAt *time.Time `json:"resolvedAt,omitempty"`
	Notes      string     `json:"notes,omitempty"`

	UpdatedAt time.Time `json:"updatedAt"`

	// Runs are the investigations against this alert, newest first. Filled by
	// the detail read, left empty in a list.
	Runs []RunRef `json:"runs,omitempty"`
	// Latest is the most recent finding, if any. Present in lists too: the
	// whole point of the dashboard is seeing the conclusion without opening
	// anything.
	Latest *Finding `json:"latest,omitempty"`
}

// RunRef is one investigation, summarised.
type RunRef struct {
	RunID     string    `json:"runId"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

// Finding is what an investigation concluded, flattened for a list row.
type Finding struct {
	RunID string `json:"runId"`
	// RootCause is the corrected cause, or the SRE note when there was no
	// correction to make.
	RootCause string `json:"rootCause,omitempty"`
	// Action is the first remediation step.
	Action string `json:"action,omitempty"`
	Risk   string `json:"risk,omitempty"`
	// Agrees is whether Devtron's first pass stood up.
	Agrees bool `json:"agrees"`
	// Unverified marks a partial run: Devtron answered, we never checked it.
	Unverified bool      `json:"unverified,omitempty"`
	Confidence float64   `json:"confidence,omitempty"`
	At         time.Time `json:"at"`
}

// LogEntry is one thing that happened to an alert.
type LogEntry struct {
	ID     int64     `json:"id"`
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	Detail string    `json:"detail,omitempty"`
	Actor  string    `json:"actor,omitempty"`
}

// Open reports whether this alert still wants attention.
func (a Alert) Open() bool { return a.State != StateResolved }
