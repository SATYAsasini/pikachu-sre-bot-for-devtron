// Package monitoring reads alerts and metrics out of whatever monitoring
// stack a cluster happens to run, through the Devtron Kubernetes proxy.
//
// Two backends are supported and normalised to one shape: Prometheus with
// Alertmanager, and VictoriaMetrics with vmalert. Callers never branch on
// which one is installed.
package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/devtron-labs/devtron-sre-agent/internal/devtron"
)

// Alert is one firing or pending alert, normalised across backends.
type Alert struct {
	Name        string            `json:"name"`
	State       string            `json:"state"` // firing | pending | suppressed
	Severity    string            `json:"severity,omitempty"`
	Summary     string            `json:"summary,omitempty"`
	Description string            `json:"description,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	StartsAt    time.Time         `json:"startsAt,omitempty"`
	Fingerprint string            `json:"fingerprint,omitempty"`
	Source      string            `json:"source"` // alertmanager | vmalert
	// Expression is the alert rule, when the backend reports it. vmalert
	// does; Alertmanager does not.
	Expression string `json:"expression,omitempty"`

	// Derived targets, pulled out of labels so callers do not each reimplement
	// the same label archaeology.
	Namespace string `json:"namespace,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Resource  string `json:"resource,omitempty"`
}

// Age is how long the alert has been firing.
func (a Alert) Age() time.Duration {
	if a.StartsAt.IsZero() {
		return 0
	}
	return time.Since(a.StartsAt)
}

// Title is a one-line label for lists and prompts.
func (a Alert) Title() string {
	b := a.Name
	if a.Severity != "" {
		b += " [" + a.Severity + "]"
	}
	if a.Namespace != "" {
		b += " " + a.Namespace
		if a.Resource != "" {
			b += "/" + a.Resource
		}
	}
	return b
}

// conditionLabels are where an alert records the state the resource is
// actually in, most specific first. `reason` carries CrashLoopBackOff,
// ImagePullBackOff, Evicted and the rest; `phase` carries Pending and Failed.
var conditionLabels = []string{"reason", "phase", "condition", "status"}

// Condition is the state the resource is in, as opposed to the state the
// alert is in. "firing" describes Alertmanager; "CrashLoopBackOff" describes
// the thing that is broken, and only the second one is worth asking about.
//
// Falls back to the alert name, which is the next most specific thing
// available and is usually a readable condition in its own right
// (KubePodCrashLooping, TargetDown, KubeSchedulerDown).
func (a Alert) Condition() string {
	for _, k := range conditionLabels {
		if v := strings.TrimSpace(a.Labels[k]); v != "" {
			return v
		}
	}
	return a.Name
}

// Ask renders the alert as the prose question /intelligence expects.
//
// It used to send the whole alert: name, severity, age, summary, description,
// the rule expression and every label. That is the alert's paperwork, not the
// problem, and handing a debugger a page of routing metadata before it has
// been told what is broken buys nothing — the first pass spent its thinking
// budget restating the input.
//
// One question, naming the resource and the state it is in. Everything that
// was in the preamble is already in the structured context, in our own fact
// pack, and on the run page for a human to read.
func (a Alert) Ask() string {
	var b strings.Builder
	b.WriteString("Why is ")
	if a.Kind != "" {
		b.WriteString(strings.ToLower(a.Kind))
		b.WriteString(" ")
	}
	if a.Resource != "" {
		b.WriteString(a.Resource)
	} else {
		// Nothing was resolvable from the labels, so the alert name is the
		// only handle on what is wrong.
		b.WriteString("the resource behind alert ")
		b.WriteString(a.Name)
	}
	if a.Namespace != "" {
		b.WriteString(" in namespace ")
		b.WriteString(a.Namespace)
	}
	b.WriteString(" in ")
	b.WriteString(a.Condition())
	b.WriteString("? Find the root cause and suggest a fix.")
	return b.String()
}

// Client reads alerts and metrics for one cluster.
type Client struct {
	dc        *devtron.Client
	clusterID int
	stack     *devtron.MonitoringStack
}

// New binds a monitoring client to one cluster's discovered stack.
func New(dc *devtron.Client, clusterID int, stack *devtron.MonitoringStack) *Client {
	return &Client{dc: dc, clusterID: clusterID, stack: stack}
}

// Stack exposes what was discovered.
func (c *Client) Stack() *devtron.MonitoringStack { return c.stack }

// AlertFilter narrows Alerts. Filters are applied client-side so the two
// backends behave identically.
type AlertFilter struct {
	Namespace string
	// NameLike matches part of the alert name, case-insensitively.
	NameLike string
	Severity string
	// IncludePending returns pending alerts as well as firing ones.
	IncludePending bool
	Limit          int
}

// Alerts fetches active alerts from whichever alert source was discovered.
func (c *Client) Alerts(ctx context.Context, f AlertFilter) ([]Alert, error) {
	if c.stack == nil || c.stack.Alerts == nil || !c.stack.Alerts.Reachable {
		return nil, fmt.Errorf("no reachable alert source in cluster %d: %s", c.clusterID, c.stackNote())
	}
	ep := c.stack.Alerts
	var (
		out []Alert
		err error
	)
	switch ep.Flavor {
	case devtron.FlavorVMAlert:
		out, err = c.vmalertAlerts(ctx, ep)
	default:
		out, err = c.alertmanagerAlerts(ctx, ep)
	}
	if err != nil {
		return nil, err
	}
	out = filterAlerts(out, f)
	sort.Slice(out, func(i, j int) bool {
		if severityRank(out[i].Severity) != severityRank(out[j].Severity) {
			return severityRank(out[i].Severity) < severityRank(out[j].Severity)
		}
		return out[i].StartsAt.Before(out[j].StartsAt)
	})
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

func (c *Client) alertmanagerAlerts(ctx context.Context, ep *devtron.Endpoint) ([]Alert, error) {
	q := url.Values{}
	q.Set("active", "true")
	q.Set("silenced", "false")
	q.Set("inhibited", "false")
	body, err := c.get(ctx, ep, ep.Path("api/v2/alerts"), q)
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
		StartsAt    time.Time         `json:"startsAt"`
		Fingerprint string            `json:"fingerprint"`
		Status      struct {
			State string `json:"state"`
		} `json:"status"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode alertmanager alerts: %w", err)
	}
	out := make([]Alert, 0, len(raw))
	for _, r := range raw {
		a := Alert{
			Name:        r.Labels["alertname"],
			State:       orDefault(r.Status.State, "firing"),
			Labels:      r.Labels,
			Annotations: r.Annotations,
			StartsAt:    r.StartsAt,
			Fingerprint: r.Fingerprint,
			Source:      "alertmanager",
		}
		enrich(&a)
		out = append(out, a)
	}
	return out, nil
}

func (c *Client) vmalertAlerts(ctx context.Context, ep *devtron.Endpoint) ([]Alert, error) {
	body, err := c.get(ctx, ep, ep.Path("api/v1/alerts"), nil)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Data struct {
			Alerts []struct {
				Name        string            `json:"name"`
				State       string            `json:"state"`
				Expression  string            `json:"expression"`
				Value       string            `json:"value"`
				Labels      map[string]string `json:"labels"`
				Annotations map[string]string `json:"annotations"`
				ActiveAt    time.Time         `json:"activeAt"`
				ID          string            `json:"id"`
			} `json:"alerts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode vmalert alerts: %w", err)
	}
	out := make([]Alert, 0, len(raw.Data.Alerts))
	for _, r := range raw.Data.Alerts {
		a := Alert{
			Name:        orDefault(r.Name, r.Labels["alertname"]),
			State:       orDefault(r.State, "firing"),
			Labels:      r.Labels,
			Annotations: r.Annotations,
			StartsAt:    r.ActiveAt,
			Fingerprint: r.ID,
			Source:      "vmalert",
			Expression:  r.Expression,
		}
		enrich(&a)
		out = append(out, a)
	}
	return out, nil
}

// enrich pulls the useful fields out of the label soup.
func enrich(a *Alert) {
	if a.Labels == nil {
		a.Labels = map[string]string{}
	}
	if a.Name == "" {
		a.Name = a.Labels["alertname"]
	}
	a.Severity = firstOf(a.Labels, "severity", "level")
	a.Summary = firstOf(a.Annotations, "summary", "message", "description")
	a.Description = firstOf(a.Annotations, "description", "runbook_url")
	a.Namespace = firstOf(a.Labels, "namespace", "exported_namespace", "kubernetes_namespace")

	// Most specific target wins: a pod tells us more than its deployment.
	for _, probe := range []struct{ key, kind string }{
		{"pod", "Pod"}, {"persistentvolumeclaim", "PersistentVolumeClaim"},
		{"deployment", "Deployment"}, {"statefulset", "StatefulSet"},
		{"daemonset", "DaemonSet"}, {"job_name", "Job"}, {"job", "Job"},
		{"service", "Service"}, {"ingress", "Ingress"}, {"node", "Node"},
		{"container", "Container"},
	} {
		if v := a.Labels[probe.key]; v != "" {
			a.Resource, a.Kind = v, probe.kind
			break
		}
	}
	if a.Kind == "Node" || a.Labels["node"] != "" && a.Namespace == "" {
		a.Namespace = ""
	}
}

func filterAlerts(in []Alert, f AlertFilter) []Alert {
	var out []Alert
	for _, a := range in {
		if !f.IncludePending && strings.EqualFold(a.State, "pending") {
			continue
		}
		if f.Namespace != "" && !strings.EqualFold(a.Namespace, f.Namespace) {
			continue
		}
		if f.Severity != "" && !strings.EqualFold(a.Severity, f.Severity) {
			continue
		}
		if f.NameLike != "" && !strings.Contains(strings.ToLower(a.Name), strings.ToLower(f.NameLike)) {
			continue
		}
		out = append(out, a)
	}
	return out
}

func (c *Client) get(ctx context.Context, ep *devtron.Endpoint, path string, q url.Values) ([]byte, error) {
	return c.dc.ProxyGetTimeout(ctx, devtron.ScopeCluster, c.clusterID, ep.Service, path, q, 20*time.Second)
}

func (c *Client) stackNote() string {
	if c.stack == nil {
		return "monitoring was never discovered"
	}
	return strings.Join(c.stack.Notes, "; ")
}

func severityRank(s string) int {
	switch strings.ToLower(s) {
	case "critical", "page":
		return 0
	case "warning", "warn":
		return 1
	case "info":
		return 2
	}
	return 3
}

func firstOf(m map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := m[k]; v != "" {
			return v
		}
	}
	return ""
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
