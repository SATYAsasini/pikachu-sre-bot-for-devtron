package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/devtron-labs/devtron-sre-agent/internal/knowledge"
)

// IdentifyArgs recognises a workload from its own signals.
type IdentifyArgs struct {
	Name      string            `json:"name,omitempty" jsonschema:"workload or pod name"`
	Namespace string            `json:"namespace,omitempty" jsonschema:"its namespace"`
	Kind      string            `json:"kind,omitempty" jsonschema:"its kind"`
	HelmChart string            `json:"helmChart,omitempty" jsonschema:"the Helm chart name, which is the strongest identification signal"`
	Labels    map[string]string `json:"labels,omitempty" jsonschema:"its labels, especially app.kubernetes.io/name"`
	Images    []string          `json:"images,omitempty" jsonschema:"container images"`
	AlertName string            `json:"alertName,omitempty" jsonschema:"the firing alert's name"`
}

// KnowledgeTools expose deterministic component identification.
//
// Reading a component's document is NOT here: ADK's skill toolset already
// does that, and better, by advertising every entry's one-line description on
// every request and fetching the body only when the model asks. What ADK has
// no equivalent for is matching a workload to a component from its Helm
// chart, labels and images, so that is what this tool does.
func KnowledgeTools() []Tool {
	return []Tool{
		Define[IdentifyArgs]("knowledge.identify", "knowledge",
			"Identify what a workload actually is from its Helm chart, labels and images. Use it when the object is anonymous: knowing a StatefulSet is Redis changes the whole investigation.",
			knowledgeIdentify),
	}
}

func knowledgeIdentify(ctx context.Context, d *Deps, a IdentifyArgs) (*Result, error) {
	cat, bad := requireKnowledge(d)
	if bad != nil {
		return bad, nil
	}
	matches := cat.Identify(knowledge.Signals{
		Name:      a.Name,
		Namespace: a.Namespace,
		Kind:      a.Kind,
		Labels:    a.Labels,
		HelmChart: a.HelmChart,
		Images:    a.Images,
		AlertName: a.AlertName,
	})
	if len(matches) == 0 {
		return &Result{
			Summary:   fmt.Sprintf("%s is not a component this catalog recognises", orUnnamed(a.Name)),
			Freshness: Now("knowledge"),
			AgentContext: map[string]any{
				"hint": "say so plainly in the report. Generic Kubernetes reasoning still applies; product-specific advice does not.",
			},
		}, nil
	}
	best := matches[0]
	out := make([]map[string]any, 0, len(matches))
	for i, m := range matches {
		if i >= 3 {
			break
		}
		out = append(out, map[string]any{
			"id": m.Component.ID, "name": m.Component.Name, "class": m.Component.Class,
			"score": m.Score, "why": m.Why, "summary": m.Component.Summary,
		})
	}
	return &Result{
		Summary: fmt.Sprintf("%s identified as %s (%s)", orUnnamed(a.Name), best.Component.Name, strings.Join(best.Why, "; ")),
		Data: map[string]any{
			"best":    best.Component.ID,
			"matches": out,
			"doc":     best.Component.Doc,
			"metrics": best.Component.Metrics,
		},
		Freshness: Now("knowledge"),
	}, nil
}

func requireKnowledge(d *Deps) (*knowledge.Catalog, *Result) {
	if d == nil || d.Knowledge == nil {
		return nil, Fail(ErrPlatform, "no_knowledge_catalog", "the knowledge catalog is not loaded", false)
	}
	return d.Knowledge, nil
}

func orUnnamed(s string) string {
	if s == "" {
		return "the workload"
	}
	return s
}
