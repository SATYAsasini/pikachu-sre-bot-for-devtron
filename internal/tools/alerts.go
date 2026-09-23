package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/devtron-labs/devtron-sre-agent/internal/monitoring"
)

// AlertsArgs lists what is currently firing.
type AlertsArgs struct {
	Namespace      string `json:"namespace,omitempty" jsonschema:"restrict to one namespace"`
	NameLike       string `json:"nameLike,omitempty" jsonschema:"only alerts whose name contains this text"`
	Severity       string `json:"severity,omitempty" jsonschema:"critical, warning or info"`
	IncludePending bool   `json:"includePending,omitempty" jsonschema:"also return alerts that are pending rather than firing"`
	Limit          int    `json:"limit,omitempty" jsonschema:"maximum alerts, default 30"`
}

// AlertTools read the cluster's alert source, whichever one it runs.
func AlertTools() []Tool {
	return []Tool{
		Define[AlertsArgs]("alerts.list", "alerts",
			"List alerts currently firing in this cluster, from Alertmanager or vmalert. Use it to see whether the alert under investigation is one symptom of a wider incident, which changes the root cause entirely.",
			listAlerts),
	}
}

func listAlerts(ctx context.Context, d *Deps, a AlertsArgs) (*Result, error) {
	if d == nil || d.Monitoring == nil {
		return Fail(ErrPlatform, "no_monitoring", "monitoring was never discovered for this cluster", false), nil
	}
	limit := a.Limit
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	ns := a.Namespace
	if ns == "" {
		ns = d.Cluster.Namespace
	}

	key := Key("alerts.list", a)
	return Cached(ctx, d, key, func() (*Result, error) {
		alerts, err := d.Monitoring.Alerts(ctx, monitoring.AlertFilter{
			Namespace:      ns,
			NameLike:       a.NameLike,
			Severity:       a.Severity,
			IncludePending: a.IncludePending,
			Limit:          limit,
		})
		if err != nil {
			return Fail(ErrPlatform, "no_alert_source", err.Error(), false,
				"state that firing alerts could not be listed; do not conclude that nothing else is wrong"), nil
		}
		source := "alertmanager"
		if st := d.Monitoring.Stack(); st != nil && st.Alerts != nil {
			source = string(st.Alerts.Flavor)
		}
		if len(alerts) == 0 {
			return &Result{
				Summary:   "no other alerts firing in " + scopeLabel(d, ns),
				Freshness: Now(source),
			}, nil
		}
		return &Result{
			Summary:   fmt.Sprintf("%d alerts firing in %s: %s", len(alerts), scopeLabel(d, ns), topAlertNames(alerts)),
			Data:      map[string]any{"alerts": alerts},
			Freshness: Now(source),
			AgentContext: map[string]any{
				"hint": "many alerts at once on one cluster usually share one cause. Look for the common denominator before debugging this one object.",
			},
		}, nil
	})
}

func topAlertNames(a []monitoring.Alert) string {
	seen := map[string]int{}
	var order []string
	for _, x := range a {
		if seen[x.Name] == 0 {
			order = append(order, x.Name)
		}
		seen[x.Name]++
	}
	var parts []string
	for i, n := range order {
		if i >= 4 {
			parts = append(parts, "…")
			break
		}
		if seen[n] > 1 {
			parts = append(parts, fmt.Sprintf("%s ×%d", n, seen[n]))
		} else {
			parts = append(parts, n)
		}
	}
	return strings.Join(parts, ", ")
}
