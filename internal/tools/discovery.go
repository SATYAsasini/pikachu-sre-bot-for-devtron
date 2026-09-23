package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/devtron-labs/devtron-sre-agent/internal/devtron"
)

// ScrapeConfigArgs inspects how a workload's metrics are collected.
type ScrapeConfigArgs struct {
	Namespace string `json:"namespace,omitempty" jsonschema:"the workload's namespace"`
	Name      string `json:"name" jsonschema:"the workload or pod name; a prefix is enough"`
}

// DiscoverMetricsArgs finds the metrics one workload actually publishes.
type DiscoverMetricsArgs struct {
	Namespace string `json:"namespace,omitempty" jsonschema:"the workload's namespace"`
	Name      string `json:"name,omitempty" jsonschema:"workload or pod name prefix; omit to cover the whole namespace"`
	Contains  string `json:"contains,omitempty" jsonschema:"only metric names containing this substring"`
	Limit     int    `json:"limit,omitempty" jsonschema:"maximum metrics, default 60"`
}

// DiscoveryTools answer the question the curated knowledge packs cannot:
// what does THIS deployment of a component actually expose here.
//
// Any third-party component can be configured to emit custom metrics, and two
// installs of the same product rarely expose the same set. Rather than guess,
// these tools read the scrape configuration off the cluster and then ask the
// metrics backend what it is really holding, including each metric's own HELP
// and TYPE as the component declares them.
func DiscoveryTools() []Tool {
	return []Tool{
		Define[ScrapeConfigArgs]("k8s.scrape_config", "k8s",
			"Find out whether and how a workload's metrics are scraped: prometheus.io annotations, named metrics ports, and any ServiceMonitor, PodMonitor or VMServiceScrape that selects it. Use it when you expect metrics for a component and find none — a workload nobody scrapes looks identical to one with no metrics.",
			scrapeConfig),

		Define[DiscoverMetricsArgs]("prom.discover_for", "prom",
			"Discover the metrics a specific workload actually publishes in THIS cluster, with each metric's own HELP text and TYPE. Use it for any component whose metric names you do not already know — custom exporters, third-party charts, anything the knowledge packs do not cover.",
			discoverMetricsFor),
	}
}

func scrapeConfig(ctx context.Context, d *Deps, a ScrapeConfigArgs) (*Result, error) {
	if d == nil || d.Devtron == nil {
		return Fail(ErrPlatform, "no_devtron_client", "the Devtron client is not configured", false), nil
	}
	ns := a.Namespace
	if ns == "" {
		ns = d.Cluster.Namespace
	}
	if ns == "" || a.Name == "" {
		return Fail(ErrInput, "missing_target", "a namespace and a name are required", false), nil
	}

	key := Key("k8s.scrape_config", a)
	return Cached(ctx, d, key, func() (*Result, error) {
		found := map[string]any{}
		var how []string

		// 1. Pod-level annotations and container ports named for metrics.
		pods, err := d.Devtron.ListResources(ctx, devtron.ResourceQuery{
			ClusterID: d.Cluster.ID, Namespace: ns, GVK: devtron.GVKPod,
			Filter: fmt.Sprintf("self.metadata.name.startsWith('%s')", escapeCEL(a.Name)),
		})
		if err == nil && len(pods.Objects) > 0 {
			p := pods.Objects[0]
			ann := podAnnotations(p)
			if scrape := ann["prometheus.io/scrape"]; scrape != "" {
				found["annotations"] = map[string]string{
					"prometheus.io/scrape": scrape,
					"prometheus.io/port":   ann["prometheus.io/port"],
					"prometheus.io/path":   ann["prometheus.io/path"],
					"prometheus.io/scheme": ann["prometheus.io/scheme"],
				}
				if scrape == "true" {
					how = append(how, "prometheus.io annotations on the pod")
				} else {
					how = append(how, "prometheus.io/scrape is set to "+scrape+", which disables annotation-based scraping")
				}
			}
			if ports := metricsPorts(p); len(ports) > 0 {
				found["metricsPorts"] = ports
				how = append(how, "a container port named for metrics")
			}
		}

		// 2. Operator CRs that select it. Both the Prometheus Operator and the
		//    VictoriaMetrics Operator spellings, since either stack may be here.
		for _, probe := range []struct{ gvk, label string }{
			{"monitoring.coreos.com/v1/ServiceMonitor", "ServiceMonitor"},
			{"monitoring.coreos.com/v1/PodMonitor", "PodMonitor"},
			{"operator.victoriametrics.com/v1beta1/VMServiceScrape", "VMServiceScrape"},
			{"operator.victoriametrics.com/v1beta1/VMPodScrape", "VMPodScrape"},
		} {
			gvk, err := devtron.ParseGVK(probe.gvk)
			if err != nil {
				continue
			}
			list, err := d.Devtron.ListResources(ctx, devtron.ResourceQuery{
				ClusterID: d.Cluster.ID, Namespace: ns, GVK: gvk,
				Filter: "true",
			})
			if err != nil || len(list.Objects) == 0 {
				continue // the CRD is absent, which is normal, not an error
			}
			var names []string
			for _, o := range list.Objects {
				if md, ok := o["metadata"].(map[string]any); ok {
					if n, ok := md["name"].(string); ok {
						names = append(names, n)
					}
				}
			}
			if len(names) > 0 {
				found[probe.label] = names
				how = append(how, fmt.Sprintf("%d %s in this namespace", len(names), probe.label))
			}
		}

		// 3. Does the metrics backend actually hold a target for it?
		if d.Monitoring != nil && d.Monitoring.MetricsAvailable() {
			if targets, err := d.Monitoring.Targets(ctx, ns); err == nil {
				var mine []map[string]any
				for _, t := range targets {
					if matchesTarget(t.Labels, a.Name) {
						mine = append(mine, map[string]any{
							"scrapeUrl": t.ScrapeURL, "health": t.Health,
							"lastError": t.LastError, "job": t.Labels["job"],
						})
					}
				}
				if len(mine) > 0 {
					found["activeTargets"] = mine
					how = append(how, fmt.Sprintf("%d active scrape target(s) confirmed in the metrics backend", len(mine)))
				}
			}
		}

		if len(how) == 0 {
			return &Result{
				Summary:   fmt.Sprintf("no metrics scrape configuration found for %s/%s", ns, a.Name),
				Data:      map[string]any{"namespace": ns, "name": a.Name},
				Freshness: Now("devtron"),
				AgentContext: map[string]any{
					"hint": "this workload is probably not scraped at all. Absence of metrics for it therefore says nothing about its health, and you must not read it as healthy.",
				},
			}, nil
		}
		return &Result{
			Summary:   fmt.Sprintf("%s/%s is scraped via: %s", ns, a.Name, strings.Join(how, "; ")),
			Data:      found,
			Freshness: Now("devtron"),
		}, nil
	})
}

func discoverMetricsFor(ctx context.Context, d *Deps, a DiscoverMetricsArgs) (*Result, error) {
	m, bad := requireMetrics(d)
	if bad != nil {
		return bad, nil
	}
	ns := a.Namespace
	if ns == "" {
		ns = d.Cluster.Namespace
	}
	limit := a.Limit
	if limit <= 0 || limit > 200 {
		limit = 60
	}

	// Scope by whatever labels the backend is likely to carry. Pod and job
	// are the two that reliably identify a workload's own series.
	var selectors []string
	switch {
	case ns != "" && a.Name != "":
		selectors = []string{
			fmt.Sprintf(`{namespace=%q,pod=~%q}`, ns, a.Name+".*"),
			fmt.Sprintf(`{namespace=%q,job=~%q}`, ns, ".*"+a.Name+".*"),
			fmt.Sprintf(`{namespace=%q,service=~%q}`, ns, a.Name+".*"),
		}
	case ns != "":
		selectors = []string{fmt.Sprintf(`{namespace=%q}`, ns)}
	case a.Name != "":
		selectors = []string{fmt.Sprintf(`{job=~%q}`, ".*"+a.Name+".*")}
	default:
		return Fail(ErrInput, "missing_target", "a namespace or a name is required", false), nil
	}

	key := Key("prom.discover_for", a)
	return Cached(ctx, d, key, func() (*Result, error) {
		seen := map[string]bool{}
		var names []string
		var usedSelector string
		for _, sel := range selectors {
			got, err := m.MetricNames(ctx, []string{sel}, 0)
			if err != nil {
				continue
			}
			for _, n := range got {
				if !seen[n] {
					seen[n] = true
					names = append(names, n)
				}
			}
			if usedSelector == "" && len(got) > 0 {
				usedSelector = sel
			}
		}
		if a.Contains != "" {
			needle := strings.ToLower(a.Contains)
			var f []string
			for _, n := range names {
				if strings.Contains(strings.ToLower(n), needle) {
					f = append(f, n)
				}
			}
			names = f
		}
		sort.Strings(names)
		total := len(names)
		if total == 0 {
			return &Result{
				Summary:   fmt.Sprintf("no metrics found for %s/%s", ns, orUnnamed(a.Name)),
				Freshness: Now(m.Flavor()),
				AgentContext: map[string]any{
					"selectorsTried": selectors,
					"hint":           "either this workload is not scraped or its series carry different labels. Call k8s.scrape_config next; do not conclude the component is healthy.",
				},
			}, nil
		}
		truncated := false
		if len(names) > limit {
			names, truncated = names[:limit], true
		}

		// Attach each metric's own HELP and TYPE. This is what makes a custom
		// metric from an unfamiliar component usable: the component documents
		// itself, and Prometheus kept the documentation.
		described := make([]map[string]any, 0, len(names))
		meta, _ := m.Metadata(ctx, "", 0)
		byName := map[string]string{}
		byType := map[string]string{}
		for _, md := range meta {
			byName[md.Metric] = md.Help
			byType[md.Metric] = md.Type
		}
		documented := 0
		for _, n := range names {
			row := map[string]any{"name": n}
			if h := byName[n]; h != "" {
				row["help"] = h
				documented++
			}
			if t := byType[n]; t != "" {
				row["type"] = t
			}
			described = append(described, row)
		}

		return &Result{
			Summary: fmt.Sprintf("%d metrics for %s/%s (%d self-documented)",
				total, ns, orUnnamed(a.Name), documented),
			Data: map[string]any{
				"metrics": described, "total": total, "selector": usedSelector,
			},
			Truncated: truncated,
			Freshness: Now(m.Flavor()),
			AgentContext: map[string]any{
				"hint": "these names exist in this cluster right now. Build PromQL from them rather than from remembered metric names, and use the selector shown to scope your query to this workload.",
			},
		}, nil
	})
}

func podAnnotations(p map[string]any) map[string]string {
	md, _ := p["metadata"].(map[string]any)
	if md == nil {
		return nil
	}
	out := map[string]string{}
	if ann, ok := md["annotations"].(map[string]any); ok {
		for k, v := range ann {
			if s, ok := v.(string); ok {
				out[k] = s
			}
		}
	}
	return out
}

// metricsPorts finds container ports whose name suggests a metrics endpoint,
// which is the convention operators rely on.
func metricsPorts(p map[string]any) []map[string]any {
	spec, _ := p["spec"].(map[string]any)
	if spec == nil {
		return nil
	}
	var out []map[string]any
	containers, _ := spec["containers"].([]any)
	for _, c := range containers {
		cm, _ := c.(map[string]any)
		if cm == nil {
			continue
		}
		ports, _ := cm["ports"].([]any)
		for _, pt := range ports {
			pm, _ := pt.(map[string]any)
			if pm == nil {
				continue
			}
			name, _ := pm["name"].(string)
			if strings.Contains(strings.ToLower(name), "metric") || strings.Contains(strings.ToLower(name), "telemetry") {
				out = append(out, map[string]any{
					"container": cm["name"], "port": pm["containerPort"], "name": name,
				})
			}
		}
	}
	return out
}

func matchesTarget(labels map[string]string, name string) bool {
	n := strings.ToLower(name)
	for _, k := range []string{"pod", "service", "job", "instance", "container"} {
		if v := strings.ToLower(labels[k]); v != "" && strings.Contains(v, n) {
			return true
		}
	}
	return false
}
