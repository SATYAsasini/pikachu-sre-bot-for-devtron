package tools

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/devtron-labs/devtron-sre-agent/internal/devtron"
	"github.com/devtron-labs/devtron-sre-agent/internal/monitoring"
)

// asDevtronError is errors.As specialised for the Devtron transport error.
func asDevtronError(err error, target **devtron.Error) bool { return errors.As(err, target) }

// MetricNamesArgs discovers which metrics exist in this cluster.
type MetricNamesArgs struct {
	Contains string   `json:"contains,omitempty" jsonschema:"only return metric names containing this substring, for example redis or kubelink"`
	Match    []string `json:"match,omitempty" jsonschema:"optional series selectors to scope the search, for example {namespace=\"payments\"}"`
	Limit    int      `json:"limit,omitempty" jsonschema:"maximum names, default 80"`
}

// QueryArgs runs one instant query.
type QueryArgs struct {
	Expr string `json:"expr" jsonschema:"a PromQL expression; works against Prometheus, VictoriaMetrics, Thanos and Mimir alike"`
}

// QueryRangeArgs runs a range query over a recent window.
type QueryRangeArgs struct {
	Expr        string `json:"expr" jsonschema:"a PromQL expression"`
	MinutesAgo  int    `json:"minutesAgo,omitempty" jsonschema:"how far back to look, default 60"`
	StepSeconds int    `json:"stepSeconds,omitempty" jsonschema:"resolution; omit to let the server pick roughly 100 points"`
}

// RulesArgs reads configured alerting rules.
type RulesArgs struct {
	NameLike string `json:"nameLike,omitempty" jsonschema:"only rules whose name contains this text"`
}

// MetricsTools are the metrics-backend tools. They work identically against
// Prometheus and VictoriaMetrics because the query API is the same.
func MetricsTools() []Tool {
	return []Tool{
		Define[MetricNamesArgs]("prom.metrics", "prom",
			"Discover which metrics actually exist in this cluster. Call this BEFORE writing a PromQL expression: inventing a metric name that is not present here is the most common way a metrics investigation wastes a step.",
			metricNames),

		Define[QueryArgs]("prom.query", "prom",
			"Run an instant PromQL query and get the current value. Use it to check a threshold, a ratio or a count right now.",
			promQuery),

		Define[QueryRangeArgs]("prom.query_range", "prom",
			"Run a PromQL query over a recent window and get the shape of the change. Use it to tell a step change from a gradual drift, and to find when a problem started.",
			promQueryRange),

		Define[RulesArgs]("prom.rules", "prom",
			"Read the configured alerting rules, including the expression and the for-duration. Use this to explain exactly why an alert fired rather than guessing its threshold.",
			promRules),
	}
}

func metricNames(ctx context.Context, d *Deps, a MetricNamesArgs) (*Result, error) {
	m, bad := requireMetrics(d)
	if bad != nil {
		return bad, nil
	}
	limit := a.Limit
	if limit <= 0 || limit > 300 {
		limit = 80
	}
	key := Key("prom.metrics", a)
	return Cached(ctx, d, key, func() (*Result, error) {
		names, err := m.MetricNames(ctx, a.Match, 0)
		if err != nil {
			return Fail(ErrCluster, "metrics_query_failed", err.Error(), true), nil
		}
		total := len(names)
		if a.Contains != "" {
			needle := strings.ToLower(a.Contains)
			var filtered []string
			for _, n := range names {
				if strings.Contains(strings.ToLower(n), needle) {
					filtered = append(filtered, n)
				}
			}
			names = filtered
		}
		sort.Strings(names)
		truncated := false
		if len(names) > limit {
			names, truncated = names[:limit], true
		}
		summary := fmt.Sprintf("%d metric names", len(names))
		if a.Contains != "" {
			summary += fmt.Sprintf(" matching %q (of %d total)", a.Contains, total)
		}
		if len(names) == 0 {
			summary = fmt.Sprintf("no metric names match %q; this cluster does not export it", a.Contains)
		}
		return &Result{
			Summary:      summary,
			Data:         map[string]any{"names": names, "totalInCluster": total},
			Truncated:    truncated,
			Freshness:    Now(m.Flavor()),
			AgentContext: map[string]any{"flavor": m.Flavor()},
		}, nil
	})
}

func promQuery(ctx context.Context, d *Deps, a QueryArgs) (*Result, error) {
	m, bad := requireMetrics(d)
	if bad != nil {
		return bad, nil
	}
	if strings.TrimSpace(a.Expr) == "" {
		return Fail(ErrInput, "missing_expr", "a PromQL expression is required", false), nil
	}
	key := Key("prom.query", a)
	return Cached(ctx, d, key, func() (*Result, error) {
		res, err := m.Query(ctx, a.Expr, time.Time{})
		if err != nil {
			return promFail(err, a.Expr), nil
		}
		if res.Empty() {
			return &Result{
				Summary:   "query returned no data: " + a.Expr,
				Freshness: Now(m.Flavor()),
				AgentContext: map[string]any{
					"hint": "an empty result is a real answer. It usually means the metric does not exist here or the label filter matched nothing. Check with prom.metrics before assuming the value is zero.",
				},
			}, nil
		}
		samples := res.Samples
		truncated := false
		if len(samples) > maxRows {
			samples, truncated = samples[:maxRows], true
		}
		return &Result{
			Summary:   fmt.Sprintf("%d series: %s", len(res.Samples), summariseSamples(res.Samples)),
			Data:      map[string]any{"resultType": res.ResultType, "samples": samples, "warnings": res.Warnings},
			Truncated: truncated,
			Freshness: Now(m.Flavor()),
		}, nil
	})
}

func promQueryRange(ctx context.Context, d *Deps, a QueryRangeArgs) (*Result, error) {
	m, bad := requireMetrics(d)
	if bad != nil {
		return bad, nil
	}
	if strings.TrimSpace(a.Expr) == "" {
		return Fail(ErrInput, "missing_expr", "a PromQL expression is required", false), nil
	}
	mins := a.MinutesAgo
	if mins <= 0 {
		mins = 60
	}
	end := time.Now()
	start := end.Add(-time.Duration(mins) * time.Minute)

	key := Key("prom.query_range", a)
	return Cached(ctx, d, key, func() (*Result, error) {
		res, err := m.QueryRange(ctx, a.Expr, start, end, time.Duration(a.StepSeconds)*time.Second)
		if err != nil {
			return promFail(err, a.Expr), nil
		}
		if res.Empty() {
			return &Result{
				Summary:   fmt.Sprintf("no data over the last %dm: %s", mins, a.Expr),
				Freshness: Now(m.Flavor()),
			}, nil
		}
		// Summarise rather than shipping every point: min/max/last is what
		// the reasoning actually needs, and full series drown the context.
		out := make([]map[string]any, 0, len(res.Series))
		for i, s := range res.Series {
			if i >= 20 {
				break
			}
			mn, mx, last := s.Stats()
			out = append(out, map[string]any{
				"labels": s.Labels, "min": mn, "max": mx, "last": last, "points": len(s.Points),
			})
		}
		return &Result{
			Summary:   fmt.Sprintf("%d series over %dm, %s", len(res.Series), mins, trendOf(res.Series)),
			Data:      map[string]any{"window": fmt.Sprintf("%dm", mins), "series": out},
			Truncated: len(res.Series) > 20,
			Freshness: Now(m.Flavor()),
		}, nil
	})
}

func promRules(ctx context.Context, d *Deps, a RulesArgs) (*Result, error) {
	m, bad := requireMetrics(d)
	if bad != nil {
		return bad, nil
	}
	key := Key("prom.rules", a)
	return Cached(ctx, d, key, func() (*Result, error) {
		rules, err := m.Rules(ctx, a.NameLike)
		if err != nil {
			return Fail(ErrCluster, "rules_unavailable", err.Error(), false,
				"the metrics backend did not serve /api/v1/rules; explain the alert from its labels instead"), nil
		}
		if len(rules) == 0 {
			return &Result{Summary: "no matching alerting rules", Freshness: Now(m.Flavor())}, nil
		}
		if len(rules) > 25 {
			rules = rules[:25]
		}
		return &Result{
			Summary:   fmt.Sprintf("%d rules matching %q", len(rules), a.NameLike),
			Data:      map[string]any{"rules": rules},
			Freshness: Now(m.Flavor()),
		}, nil
	})
}

// requireMetrics returns the monitoring client or an error Result that says
// plainly that coverage is unknown. "No metrics backend" must never be
// reported to the model as "the metric is zero".
func requireMetrics(d *Deps) (*monitoring.Client, *Result) {
	if d == nil || d.Monitoring == nil || !d.Monitoring.MetricsAvailable() {
		note := "no metrics backend was discovered in this cluster"
		if d != nil && d.Monitoring != nil && d.Monitoring.Stack() != nil {
			if n := d.Monitoring.Stack().Notes; len(n) > 0 {
				note = strings.Join(n, "; ")
			}
		}
		return nil, Fail(ErrPlatform, "no_metrics_backend", note, false,
			"do not infer that values are zero or healthy; state that metrics coverage is unavailable",
			"continue with Kubernetes state and events instead")
	}
	return d.Monitoring, nil
}

func promFail(err error, expr string) *Result {
	msg := err.Error()
	if strings.Contains(msg, "parse") || strings.Contains(msg, "invalid") {
		return Fail(ErrInput, "bad_promql", msg, false,
			"fix the expression; check label names with prom.metrics before retrying")
	}
	return Fail(ErrCluster, "metrics_query_failed", fmt.Sprintf("%s: %s", expr, msg), true)
}

func summariseSamples(s []monitoring.Sample) string {
	if len(s) == 0 {
		return "empty"
	}
	if len(s) == 1 {
		return fmt.Sprintf("%g", s[0].Value)
	}
	mn, mx := s[0].Value, s[0].Value
	for _, x := range s {
		if x.Value < mn {
			mn = x.Value
		}
		if x.Value > mx {
			mx = x.Value
		}
	}
	return fmt.Sprintf("min %g, max %g", mn, mx)
}

// trendOf says whether the window rose, fell or stayed flat, which is the
// thing a reader wants from a range query before any numbers.
func trendOf(series []monitoring.Series) string {
	if len(series) == 0 {
		return "no data"
	}
	var first, last float64
	var n int
	for _, s := range series {
		if len(s.Points) == 0 {
			continue
		}
		first += s.Points[0].Value
		last += s.Points[len(s.Points)-1].Value
		n++
	}
	if n == 0 {
		return "no points"
	}
	switch {
	case last > first*1.2:
		return fmt.Sprintf("rising (%g to %g)", first, last)
	case last < first*0.8:
		return fmt.Sprintf("falling (%g to %g)", first, last)
	default:
		return fmt.Sprintf("flat near %g", last)
	}
}
