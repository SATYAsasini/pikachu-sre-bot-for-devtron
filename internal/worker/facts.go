// Package worker orchestrates one investigation: gather deterministic facts,
// ask Devtron's first-pass debugger, then run the two agents over both.
package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/devtron-labs/devtron-sre-agent/internal/devtron"
	"github.com/devtron-labs/devtron-sre-agent/internal/knowledge"
	"github.com/devtron-labs/devtron-sre-agent/internal/monitoring"
	"github.com/devtron-labs/devtron-sre-agent/internal/runs"
	"github.com/devtron-labs/devtron-sre-agent/internal/tools"
)

// collectFacts builds the deterministic fact pack. No model has run yet and
// none runs here: everything in this pack is something we read, not something
// anyone inferred. That is what makes it usable as a yardstick for grading
// Devtron Intelligence's analysis afterwards.
func (w *Worker) collectFacts(ctx context.Context, run *runs.Run, deps *tools.Deps, alert *monitoring.Alert, ledger runs.LedgerFunc) map[string]any {
	facts := map[string]any{
		"cluster":   run.Scope.ClusterName,
		"namespace": run.Scope.Namespace,
	}

	// What monitoring exists here, stated honestly. "No metrics backend" must
	// reach the model as a known gap, never as silence.
	if st := deps.Monitoring.Stack(); st != nil {
		facts["monitoring"] = map[string]any{
			"summary":       st.Summary(),
			"metrics":       st.Metrics,
			"alerts":        st.Alerts,
			"notes":         st.Notes,
			"metricsUsable": st.HasMetrics(),
			"alertsUsable":  st.HasAlerts(),
		}
	}

	kind, name, ns := targetOf(run, alert)
	if ns == "" {
		ns = run.Scope.Namespace
	}

	// The failing object itself, in full.
	var obj map[string]any
	if kind != "" && name != "" {
		res := w.callTool(ctx, deps, ledger, "k8s.get", tools.GetArgs{Kind: kind, Namespace: ns, Name: name})
		if res != nil {
			facts["target"] = map[string]any{"kind": kind, "namespace": ns, "name": name, "summary": res.Summary}
			if res.Error == nil {
				obj, _ = res.Data.(map[string]any)
				facts["targetObject"] = res.Data
			} else {
				facts["targetError"] = res.Error
			}
		}
	}

	// Events usually state the reason outright.
	if ns != "" {
		if res := w.callTool(ctx, deps, ledger, "k8s.events", tools.EventsArgs{Namespace: ns, Name: name, Limit: 25}); res != nil {
			facts["events"] = map[string]any{"summary": res.Summary, "data": res.Data}
		}
	}

	// Who owns this namespace, and what chart is installed. The chart name is
	// what turns an anonymous StatefulSet into a recognisable product.
	var appCtx map[string]any
	if res := w.callTool(ctx, deps, ledger, "devtron.app_context", tools.AppContextArgs{Namespace: ns}); res != nil {
		facts["devtron"] = map[string]any{"summary": res.Summary, "data": res.Data}
		appCtx, _ = res.Data.(map[string]any)
	}

	// Deterministic identification, before any model gets an opinion.
	if m := identify(deps.Knowledge, obj, appCtx, alert, kind, name, ns); m != nil {
		facts["identifiedComponent"] = map[string]any{
			"id":          m.Component.ID,
			"displayName": m.Component.Name,
			"class":       m.Component.Class,
			"skill":       m.Component.Skill,
			"summary":     m.Component.Summary,
			"matchWhy":    m.Why,
			"score":       m.Score,
		}
		_, _ = ledger(ctx, runs.EvFinding, "preflight", map[string]any{
			"kind": "component_identified", "id": m.Component.ID, "why": m.Why,
		})
	}

	// Is this one symptom of something larger?
	if deps.Monitoring != nil && deps.Monitoring.Stack().HasAlerts() {
		if res := w.callTool(ctx, deps, ledger, "alerts.list", tools.AlertsArgs{Namespace: "", Limit: 40}); res != nil {
			facts["clusterAlerts"] = map[string]any{"summary": res.Summary}
		}
	}
	return facts
}

// identify gathers every signal we have about the target and asks the
// catalog. Helm chart beats image beats label beats name, and the catalog
// enforces that ordering.
func identify(cat *knowledge.Catalog, obj, appCtx map[string]any, alert *monitoring.Alert, kind, name, ns string) *knowledge.Match {
	if cat == nil {
		return nil
	}
	sig := knowledge.Signals{Name: name, Namespace: ns, Kind: kind}
	if alert != nil {
		sig.AlertName = alert.Name
		if sig.Name == "" {
			sig.Name = alert.Resource
		}
	}
	if obj != nil {
		if md, ok := obj["metadata"].(map[string]any); ok {
			sig.Labels = stringMap(md["labels"])
			if sig.Labels != nil {
				sig.HelmChart = sig.Labels["helm.sh/chart"]
			}
		}
		sig.Images = imagesOf(obj)
	}
	// A Helm release covering this namespace names the product outright.
	if appCtx != nil {
		for _, h := range asMaps(appCtx["helmApps"]) {
			chart, _ := h["chartName"].(string)
			appName, _ := h["appName"].(string)
			if chart == "" {
				continue
			}
			if sig.HelmChart == "" || strings.Contains(strings.ToLower(name), strings.ToLower(appName)) {
				sig.HelmChart = chart
			}
		}
	}
	return cat.Best(sig)
}

// callTool invokes a registered tool directly and records it on the ledger,
// so pre-flight reads are auditable exactly like the agent's own calls.
func (w *Worker) callTool(ctx context.Context, deps *tools.Deps, ledger runs.LedgerFunc, name string, args any) *tools.Result {
	t, ok := w.Registry.Get(name)
	if !ok {
		return nil
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return nil
	}
	seq, _ := ledger(ctx, runs.EvToolCall, "preflight", map[string]any{"tool": name, "args": args})
	res, err := t.Invoke(ctx, deps, raw)
	if err != nil {
		_, _ = ledger(ctx, runs.EvError, "preflight", map[string]any{"tool": name, "error": err.Error()})
		return nil
	}
	payload := map[string]any{"tool": name, "summary": res.Summary, "ev": seq}
	if res.Error != nil {
		payload["toolError"] = res.Error
	}
	_, _ = ledger(ctx, runs.EvToolResult, "preflight", payload)
	return res
}

// targetOf decides which object the run is about, preferring what the user
// picked over what the alert's labels imply.
func targetOf(run *runs.Run, alert *monitoring.Alert) (kind, name, ns string) {
	if alert != nil {
		kind, name, ns = alert.Kind, alert.Resource, alert.Namespace
	}
	if ns == "" {
		ns = run.Scope.Namespace
	}
	if kind == "Container" {
		kind = "Pod"
	}
	if kind == "" && name != "" {
		kind = "Pod"
	}
	return kind, name, ns
}

// intelligenceContext maps the run onto the only keys Devtron's agent reads.
// Anything else is dropped before the prompt is built, which is why the alert
// itself goes into the ask text instead.
func intelligenceContext(run *runs.Run, alert *monitoring.Alert) devtron.IntelligenceContext {
	c := devtron.IntelligenceContext{
		ClusterID:       run.Scope.ClusterID,
		ClusterName:     run.Scope.ClusterName,
		Namespace:       run.Scope.Namespace,
		EnvironmentID:   run.Scope.EnvironmentID,
		EnvironmentName: run.Scope.EnvironmentName,
		AppName:         run.Scope.AppName,
		AppType:         run.Scope.AppType,
	}
	if alert != nil {
		if c.Namespace == "" {
			c.Namespace = alert.Namespace
		}
		c.ResourceKind = alert.Kind
		c.ResourceName = alert.Resource
		c.ResourceStatus = alert.State
	}
	return c
}

// askFor builds the prose question. Everything the model must see has to be
// here: Devtron drops alertname, severity, labels and summary from context.
func askFor(run *runs.Run, alert *monitoring.Alert) string {
	if alert != nil {
		return alert.Ask()
	}
	if run.Trigger.Ask != "" {
		return run.Trigger.Ask
	}
	return fmt.Sprintf("Investigate the health of namespace %s on cluster %s and report anything wrong.",
		run.Scope.Namespace, run.Scope.ClusterName)
}

func stringMap(v any) map[string]string {
	m, _ := v.(map[string]any)
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, val := range m {
		if s, ok := val.(string); ok {
			out[k] = s
		}
	}
	return out
}

func imagesOf(obj map[string]any) []string {
	var out []string
	spec, _ := obj["spec"].(map[string]any)
	if spec == nil {
		return nil
	}
	collect := func(v any) {
		for _, c := range asAny(v) {
			cm, _ := c.(map[string]any)
			if img, ok := cm["image"].(string); ok {
				out = append(out, img)
			}
		}
	}
	collect(spec["containers"])
	collect(spec["initContainers"])
	if tmpl, ok := spec["template"].(map[string]any); ok {
		if ts, ok := tmpl["spec"].(map[string]any); ok {
			collect(ts["containers"])
			collect(ts["initContainers"])
		}
	}
	return out
}

func asAny(v any) []any {
	s, _ := v.([]any)
	return s
}

func asMaps(v any) []map[string]any {
	switch t := v.(type) {
	case []map[string]any:
		return t
	case []any:
		var out []map[string]any
		for _, x := range t {
			if m, ok := x.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}
