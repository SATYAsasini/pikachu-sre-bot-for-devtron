// Package rules decides, per cluster, which alerts are worth showing, which
// are worth investigating on their own, and how urgent each one is.
//
// All three questions are answered by the same matcher against the alert
// payload. That is deliberate: an operator who has written "severity=critical
// and namespace=prod" to define P0 should be able to paste the same clause to
// decide what auto-triggers, and three subtly different matching languages in
// one product is how a filter ends up meaning something different from the
// rule that shares its wording.
package rules

import (
	"regexp"
	"strings"

	"github.com/devtron-labs/devtron-sre-agent/internal/monitoring"
)

// Priority is how urgent a matched alert is. Three levels, because a scale
// people cannot hold in their head is a scale they stop setting.
type Priority string

const (
	P0 Priority = "P0" // wake someone
	P1 Priority = "P1" // today
	P2 Priority = "P2" // the backlog
)

// DefaultPriority is what an alert gets when no priority rule claims it.
// Deliberately the lowest: an unclassified alert is unclassified, not urgent.
const DefaultPriority = P2

// Match is one condition against an alert payload.
//
// Every field is optional and they are ANDed. An empty Match matches
// **nothing** — see IsEmpty. A deliberate catch-all is Rule.CatchAll.
type Match struct {
	// Name matches the alert name. Substring, case-insensitive.
	Name string `json:"name,omitempty"`
	// Severity is an OR over the alert's severity, case-insensitive.
	Severity []string `json:"severity,omitempty"`
	// Namespace is an OR over the resolved namespace.
	Namespace []string `json:"namespace,omitempty"`
	// Kind is an OR over the resolved resource kind.
	Kind []string `json:"kind,omitempty"`
	// Labels are exact label matches, all of which must hold.
	Labels map[string]string `json:"labels,omitempty"`
	// LabelsRegex are pattern matches on labels, all of which must hold.
	// An unparseable pattern never matches rather than matching everything —
	// a typo must not silently widen a rule.
	LabelsRegex map[string]string `json:"labelsRegex,omitempty"`
}

// Rule is a match plus what to do about it.
type Rule struct {
	// CatchAll makes a rule with no conditions match every alert.
	//
	// It exists because an empty match used to do that implicitly, and "Add
	// rule" creates an empty rule — so the instant anyone clicked it, every
	// alert on the cluster was claimed by a half-written rule and the whole
	// preview turned one colour. A catch-all is a real thing to want at the
	// bottom of a priority list, but it has to be asked for.
	CatchAll bool `json:"catchAll,omitempty"`

	// Name is for the operator, not the matcher.
	Name string `json:"name,omitempty"`
	// Match is the condition. Empty matches everything.
	Match Match `json:"match"`
	// Priority is set on priority rules only.
	Priority Priority `json:"priority,omitempty"`
	// Enabled is honoured by every list. Disabled rules are kept rather than
	// deleted so an operator can switch one off without losing what it said.
	Enabled bool `json:"enabled"`
}

// Config is one cluster's rules.
type Config struct {
	ClusterID int `json:"clusterId"`

	// Show narrows the alert list. Empty means show everything, which is the
	// right default: a new cluster should not appear silent because nobody
	// has written a rule yet.
	Show []Rule `json:"show"`

	// Mute drops alerts even when Show accepted them. A subtract list is far
	// easier to reason about than encoding "everything except" into a match.
	Mute []Rule `json:"mute"`

	// Auto opens an investigation without being asked. Off unless a rule says
	// otherwise, because this one spends money.
	Auto []Rule `json:"auto"`
	// AutoEnabled is the master switch. A cluster with auto rules but the
	// switch off is a cluster someone is still drafting.
	AutoEnabled bool `json:"autoEnabled"`

	// Priority is evaluated in order, first match wins. Ordered rather than
	// scored because "why is this P1" has to be answerable by pointing at one
	// line.
	Priority []Rule `json:"priority"`

	// Notify is where this cluster's findings go.
	Notify Notify `json:"notify"`
}

// Normalise replaces nil slices with empty ones.
//
// A nil slice marshals to `null`, and a browser doing `rules.length` on null
// throws — which is exactly what happened: a cluster with no saved rules
// crashed the whole page with "Cannot read properties of null". Fixed here
// rather than only in the client, because every consumer would otherwise have
// to know.
func (c Config) Normalise() Config {
	c.Show = orEmpty(c.Show)
	c.Mute = orEmpty(c.Mute)
	c.Auto = orEmpty(c.Auto)
	c.Priority = orEmpty(c.Priority)
	return c
}

func orEmpty(r []Rule) []Rule {
	if r == nil {
		return []Rule{}
	}
	return r
}

// IsEmpty reports whether this match has no conditions at all.
func (m Match) IsEmpty() bool {
	return m.Name == "" &&
		len(m.Severity) == 0 &&
		len(m.Namespace) == 0 &&
		len(m.Kind) == 0 &&
		len(m.Labels) == 0 &&
		len(m.LabelsRegex) == 0
}

// Matches reports whether the alert satisfies every clause present.
//
// An empty match returns false. A rule that claims everything by accident is
// far more damaging than one that claims nothing: it hides every alert, or
// relabels every alert, the moment it is created.
func (m Match) Matches(a monitoring.Alert) bool {
	if m.IsEmpty() {
		return false
	}
	if m.Name != "" && !strings.Contains(strings.ToLower(a.Name), strings.ToLower(m.Name)) {
		return false
	}
	if !oneOf(m.Severity, a.Severity) {
		return false
	}
	if !oneOf(m.Namespace, a.Namespace) {
		return false
	}
	if !oneOf(m.Kind, a.Kind) {
		return false
	}
	for k, want := range m.Labels {
		if !strings.EqualFold(a.Labels[k], want) {
			return false
		}
	}
	for k, pattern := range m.LabelsRegex {
		re, err := regexp.Compile("(?i)" + pattern)
		if err != nil || !re.MatchString(a.Labels[k]) {
			return false
		}
	}
	return true
}

// oneOf is an empty-means-any OR, case-insensitive.
func oneOf(want []string, got string) bool {
	if len(want) == 0 {
		return true
	}
	for _, w := range want {
		if strings.EqualFold(strings.TrimSpace(w), got) {
			return true
		}
	}
	return false
}

// anyMatch reports whether any enabled rule matches, and which one.
func anyMatch(rs []Rule, a monitoring.Alert) (Rule, bool) {
	for _, r := range rs {
		if !r.Enabled {
			continue
		}
		if r.CatchAll && r.Match.IsEmpty() {
			return r, true
		}
		if r.Match.Matches(a) {
			return r, true
		}
	}
	return Rule{}, false
}

// Decision is what the rules concluded about one alert.
type Decision struct {
	Show     bool     `json:"show"`
	Priority Priority `json:"priority"`
	Auto     bool     `json:"auto"`
	// Why names the rule that decided the priority, so an operator can point
	// at the line rather than guess.
	Why string `json:"why,omitempty"`
	// MutedBy names the rule that hid it, for the same reason.
	MutedBy string `json:"mutedBy,omitempty"`
}

// Decide evaluates one alert against a cluster's rules.
func (c Config) Decide(a monitoring.Alert) Decision {
	d := Decision{Show: true, Priority: DefaultPriority}

	// No show rules means show everything. Any enabled show rule makes the
	// list opt-in.
	if hasEnabled(c.Show) {
		_, ok := anyMatch(c.Show, a)
		d.Show = ok
	}
	if r, ok := anyMatch(c.Mute, a); ok {
		d.Show = false
		d.MutedBy = ruleLabel(r)
	}

	// First match wins, in the order the operator wrote them.
	if r, ok := anyMatch(c.Priority, a); ok && r.Priority != "" {
		d.Priority = r.Priority
		d.Why = ruleLabel(r)
	}

	// Auto never fires on something the rules already hid: an alert not worth
	// showing is not worth spending a run on.
	if c.AutoEnabled && d.Show {
		_, d.Auto = anyMatch(c.Auto, a)
	}
	return d
}

// Apply decides for a whole list and returns what should be displayed, each
// with its decision attached.
func (c Config) Apply(alerts []monitoring.Alert) ([]monitoring.Alert, []Decision) {
	outA := make([]monitoring.Alert, 0, len(alerts))
	outD := make([]Decision, 0, len(alerts))
	for _, a := range alerts {
		d := c.Decide(a)
		if !d.Show {
			continue
		}
		outA = append(outA, a)
		outD = append(outD, d)
	}
	return outA, outD
}

// hasEnabled reports whether any rule can actually match something.
//
// An enabled but empty rule does not count. Otherwise adding a blank show rule
// would switch the list to opt-in and hide every alert on the cluster while
// somebody was still typing the first condition.
func hasEnabled(rs []Rule) bool {
	for _, r := range rs {
		if r.Enabled && (r.CatchAll || !r.Match.IsEmpty()) {
			return true
		}
	}
	return false
}

func ruleLabel(r Rule) string {
	if strings.TrimSpace(r.Name) != "" {
		return r.Name
	}
	return "unnamed rule"
}

// Rank orders priorities for sorting. Lower is more urgent.
func Rank(p Priority) int {
	switch p {
	case P0:
		return 0
	case P1:
		return 1
	default:
		return 2
	}
}
