package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/devtron-labs/devtron-sre-agent/internal/devtron"
)

// maxRows bounds what any list tool hands back to the model.
const maxRows = 60

// ListArgs selects objects of one kind.
type ListArgs struct {
	Kind          string   `json:"kind" jsonschema:"group/version/kind such as apps/v1/Deployment, or a bare common kind such as Pod"`
	Namespace     string   `json:"namespace,omitempty" jsonschema:"namespace to search; empty searches every namespace the token can read"`
	LabelSelector []string `json:"labelSelector,omitempty" jsonschema:"label selectors such as app=payments"`
	FieldSelector []string `json:"fieldSelector,omitempty" jsonschema:"field selectors such as status.phase=Running"`
	Filter        string   `json:"filter,omitempty" jsonschema:"optional CEL expression over self, such as self.metadata.name.startsWith('pay'); returns whole objects instead of table rows"`
	Limit         int      `json:"limit,omitempty" jsonschema:"maximum rows to return, default 60"`
}

// GetArgs names one object.
type GetArgs struct {
	Kind      string `json:"kind" jsonschema:"group/version/kind, or a bare common kind such as Pod"`
	Namespace string `json:"namespace" jsonschema:"the object's namespace"`
	Name      string `json:"name" jsonschema:"the object's name"`
}

// EventsArgs asks for events about one object.
type EventsArgs struct {
	Namespace string `json:"namespace" jsonschema:"namespace to read events from"`
	Name      string `json:"name,omitempty" jsonschema:"object name to filter on; empty returns the namespace's events"`
	Limit     int    `json:"limit,omitempty" jsonschema:"maximum events, default 40"`
}

// LogsArgs asks for one container's log.
type LogsArgs struct {
	Namespace string `json:"namespace" jsonschema:"the pod's namespace"`
	Pod       string `json:"pod" jsonschema:"the pod name, exactly as k8s.list reported it"`
	Container string `json:"container" jsonschema:"the container name; required, and visible in the pod's spec.containers"`
	TailLines int    `json:"tailLines,omitempty" jsonschema:"how many lines from the end, default 200, maximum 2000"`
	Previous  bool   `json:"previous,omitempty" jsonschema:"read the container instance before the current one; this is where a CrashLoopBackOff or an OOMKill leaves its reason"`
}

// K8sTools are the cluster-read tools, all served through the Devtron
// orchestrator rather than a Kubernetes client.
func K8sTools() []Tool {
	return []Tool{
		Define[ListArgs]("k8s.list", "k8s",
			"List Kubernetes objects of one kind in the run's cluster. Use this to find workloads, services, nodes or PVCs, and to check how many of something exist. Prefer a label selector over a CEL filter.",
			listResources),

		Define[GetArgs]("k8s.get", "k8s",
			"Fetch one Kubernetes object in full, including its status and conditions. Use this after k8s.list has told you the exact name, when you need the reason a workload is unhealthy.",
			getResource),

		Define[EventsArgs]("k8s.events", "k8s",
			"Read recent Kubernetes events for a namespace or one object. Events usually state plainly why scheduling, mounting, pulling or probing failed, so reach for this before reasoning from metrics.",
			listEvents),

		Define[LogsArgs]("k8s.logs", "k8s",
			"Read the tail of one container's log. For a pod that is crashing or was OOMKilled, set previous=true — the current instance is usually too young to have said anything, and the reason is in the instance that died. Events tell you a container restarted; this tells you what it was doing when it did.",
			podLogs),
	}
}

// podLogs reads one container's log tail.
//
// Bounded twice over, because the orchestrator bounds it not at all: it
// buffers the whole log in memory before sending, so tailLines and the byte
// cap in the client are the only limits that exist.
func podLogs(ctx context.Context, d *Deps, a LogsArgs) (*Result, error) {
	if d == nil || d.Devtron == nil {
		return Fail(ErrPlatform, "no_devtron_client", "the Devtron client is not configured", false), nil
	}
	if a.Pod == "" {
		return Fail(ErrInput, "missing_pod", "a pod name is required", false), nil
	}
	if a.Container == "" {
		return Fail(ErrInput, "missing_container", "a container name is required", false,
			"run k8s.get on the pod and read spec.containers[].name"), nil
	}
	ns := a.Namespace
	if ns == "" {
		ns = d.Cluster.Namespace
	}

	key := Key("k8s.logs", a)
	return Cached(ctx, d, key, func() (*Result, error) {
		text, err := d.Devtron.PodLogs(ctx, devtron.PodLogOptions{
			ClusterID: d.Cluster.ID,
			Namespace: ns,
			Pod:       a.Pod,
			Container: a.Container,
			TailLines: a.TailLines,
			Previous:  a.Previous,
		})
		which := "current"
		if a.Previous {
			which = "previous"
		}
		if errors.Is(err, devtron.ErrNoPreviousContainer) {
			// Worth saying rather than reporting as a failure: it means the
			// container has not restarted, which is itself a finding.
			return &Result{
				Summary:   fmt.Sprintf("%s/%s container %s has no previous instance, so it has not restarted", ns, a.Pod, a.Container),
				Freshness: Now("devtron"),
				AgentContext: map[string]any{
					"namespace": ns, "pod": a.Pod, "container": a.Container, "restarted": false,
				},
			}, nil
		}
		if err != nil {
			return devtronFail(err, fmt.Sprintf("read %s logs for %s/%s", which, ns, a.Pod)), nil
		}

		lines := 0
		if text != "" {
			lines = strings.Count(strings.TrimRight(text, "\n"), "\n") + 1
		}
		res := &Result{
			Summary: fmt.Sprintf("%d lines from the %s instance of %s/%s container %s",
				lines, which, ns, a.Pod, a.Container),
			Data:      map[string]any{"log": text},
			Freshness: Now("devtron"),
			AgentContext: map[string]any{
				"namespace": ns, "pod": a.Pod, "container": a.Container, "previous": a.Previous,
			},
			Truncated: len(text) >= devtron.MaxLogBytes,
		}
		if text == "" {
			res.Summary = fmt.Sprintf("the %s instance of %s/%s container %s has written nothing",
				which, ns, a.Pod, a.Container)
		}
		return res, nil
	})
}

func listResources(ctx context.Context, d *Deps, a ListArgs) (*Result, error) {
	if d == nil || d.Devtron == nil {
		return Fail(ErrPlatform, "no_devtron_client", "the Devtron client is not configured", false), nil
	}
	gvk, err := devtron.ParseGVK(a.Kind)
	if err != nil {
		return Fail(ErrInput, "bad_kind", err.Error(), false,
			"pass a full group/version/kind, for example apps/v1/Deployment or v1/Service"), nil
	}
	ns := a.Namespace
	if ns == "" {
		ns = d.Cluster.Namespace
	}
	limit := a.Limit
	if limit <= 0 || limit > maxRows {
		limit = maxRows
	}

	key := Key("k8s.list", a)
	return Cached(ctx, d, key, func() (*Result, error) {
		list, err := d.Devtron.ListResources(ctx, devtron.ResourceQuery{
			ClusterID:     d.Cluster.ID,
			Namespace:     ns,
			GVK:           gvk,
			LabelSelector: a.LabelSelector,
			FieldSelector: a.FieldSelector,
			Filter:        a.Filter,
		})
		if err != nil {
			return devtronFail(err, "list "+gvk.Kind), nil
		}
		total := list.Len()
		res := &Result{
			Summary:   fmt.Sprintf("%d %s in %s", total, gvk.Kind, scopeLabel(d, ns)),
			Freshness: Now("devtron"),
			AgentContext: map[string]any{
				"gvk": gvk, "namespace": ns, "clusterId": d.Cluster.ID, "total": total,
			},
		}
		if total == 0 {
			res.Summary = fmt.Sprintf("no %s found in %s", gvk.Kind, scopeLabel(d, ns))
			return res, nil
		}
		if len(list.Objects) > 0 {
			objs := list.Objects
			if len(objs) > limit {
				objs, res.Truncated = objs[:limit], true
			}
			res.Data = map[string]any{"objects": trimObjects(objs)}
		} else {
			rows := list.Rows
			if len(rows) > limit {
				rows, res.Truncated = rows[:limit], true
			}
			res.Data = map[string]any{"headers": list.Headers, "rows": rows}
		}
		return res, nil
	})
}

func getResource(ctx context.Context, d *Deps, a GetArgs) (*Result, error) {
	if d == nil || d.Devtron == nil {
		return Fail(ErrPlatform, "no_devtron_client", "the Devtron client is not configured", false), nil
	}
	if a.Name == "" {
		return Fail(ErrInput, "missing_name", "a resource name is required", false), nil
	}
	gvk, err := devtron.ParseGVK(a.Kind)
	if err != nil {
		return Fail(ErrInput, "bad_kind", err.Error(), false), nil
	}
	ns := a.Namespace
	if ns == "" {
		ns = d.Cluster.Namespace
	}

	key := Key("k8s.get", a)
	return Cached(ctx, d, key, func() (*Result, error) {
		// A CEL filter on the name gets the whole object back, which the
		// table-row form does not carry.
		list, err := d.Devtron.ListResources(ctx, devtron.ResourceQuery{
			ClusterID: d.Cluster.ID,
			Namespace: ns,
			GVK:       gvk,
			Filter:    fmt.Sprintf("self.metadata.name == '%s'", escapeCEL(a.Name)),
		})
		if err != nil {
			return devtronFail(err, "get "+gvk.Kind+"/"+a.Name), nil
		}
		if len(list.Objects) == 0 {
			return &Result{
				Summary:   fmt.Sprintf("%s %s/%s not found", gvk.Kind, ns, a.Name),
				Freshness: Now("devtron"),
				Error: &Error{Kind: ErrCluster, Code: "not_found",
					Message:   fmt.Sprintf("no %s named %s in namespace %s", gvk.Kind, a.Name, ns),
					Retryable: false,
					Suggestions: []string{
						"run k8s.list for this kind to see the names that do exist",
						"the object may have been replaced; check for a new generation with a different suffix",
					}},
			}, nil
		}
		obj := list.Objects[0]
		return &Result{
			Summary:      fmt.Sprintf("%s %s/%s: %s", gvk.Kind, ns, a.Name, describeStatus(obj)),
			Data:         obj,
			Freshness:    Now("devtron"),
			AgentContext: map[string]any{"gvk": gvk, "namespace": ns, "name": a.Name},
		}, nil
	})
}

func listEvents(ctx context.Context, d *Deps, a EventsArgs) (*Result, error) {
	if d == nil || d.Devtron == nil {
		return Fail(ErrPlatform, "no_devtron_client", "the Devtron client is not configured", false), nil
	}
	ns := a.Namespace
	if ns == "" {
		ns = d.Cluster.Namespace
	}
	if ns == "" {
		return Fail(ErrInput, "missing_namespace", "a namespace is required to read events", false), nil
	}
	limit := a.Limit
	if limit <= 0 || limit > maxRows {
		limit = 40
	}
	q := devtron.ResourceQuery{ClusterID: d.Cluster.ID, Namespace: ns, GVK: devtron.GVKEvent}
	if a.Name != "" {
		q.FieldSelector = []string{"involvedObject.name=" + a.Name}
	}

	key := Key("k8s.events", a)
	return Cached(ctx, d, key, func() (*Result, error) {
		list, err := d.Devtron.ListResources(ctx, q)
		if err != nil {
			return devtronFail(err, "events in "+ns), nil
		}
		rows := list.Rows
		if len(list.Objects) > 0 {
			rows = nil
			for _, o := range list.Objects {
				rows = append(rows, eventRow(o))
			}
		}
		truncated := false
		if len(rows) > limit {
			rows, truncated = rows[len(rows)-limit:], true
		}
		target := ns
		if a.Name != "" {
			target = ns + "/" + a.Name
		}
		if len(rows) == 0 {
			return &Result{
				Summary:   "no events for " + target + " (events expire, so this does not mean nothing happened)",
				Freshness: Now("devtron"),
			}, nil
		}
		return &Result{
			Summary:   fmt.Sprintf("%d events for %s", len(rows), target),
			Data:      map[string]any{"headers": list.Headers, "events": rows},
			Truncated: truncated,
			Freshness: Now("devtron"),
		}, nil
	})
}

// eventRow flattens an Event object to the fields that carry meaning.
func eventRow(o map[string]any) map[string]any {
	md, _ := o["metadata"].(map[string]any)
	io, _ := o["involvedObject"].(map[string]any)
	row := map[string]any{
		"type":    o["type"],
		"reason":  o["reason"],
		"message": o["message"],
		"count":   o["count"],
	}
	if md != nil {
		row["name"] = md["name"]
	}
	if io != nil {
		row["object"] = fmt.Sprintf("%v/%v", io["kind"], io["name"])
	}
	for _, k := range []string{"lastTimestamp", "eventTime", "firstTimestamp"} {
		if v, ok := o[k]; ok && v != nil {
			row["at"] = v
			break
		}
	}
	return row
}

// trimObjects drops managedFields and the last-applied annotation, which are
// large, never diagnostic, and would eat the model's context.
func trimObjects(objs []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(objs))
	for _, o := range objs {
		if md, ok := o["metadata"].(map[string]any); ok {
			delete(md, "managedFields")
			if ann, ok := md["annotations"].(map[string]any); ok {
				delete(ann, "kubectl.kubernetes.io/last-applied-configuration")
			}
		}
		out = append(out, o)
	}
	return out
}

// describeStatus produces a one-line status for the summary line.
func describeStatus(o map[string]any) string {
	st, _ := o["status"].(map[string]any)
	if st == nil {
		return "fetched"
	}
	if phase, ok := st["phase"].(string); ok && phase != "" {
		if reason := containerTrouble(st); reason != "" {
			return phase + ", " + reason
		}
		return phase
	}
	var parts []string
	for _, key := range []string{"readyReplicas", "replicas", "availableReplicas", "unavailableReplicas"} {
		if v, ok := st[key]; ok {
			parts = append(parts, fmt.Sprintf("%s=%v", key, v))
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, " ")
	}
	if conds, ok := st["conditions"].([]any); ok {
		for _, c := range conds {
			cm, _ := c.(map[string]any)
			if cm == nil {
				continue
			}
			if cm["status"] == "False" || cm["status"] == "Unknown" {
				return fmt.Sprintf("%v=%v (%v)", cm["type"], cm["status"], cm["reason"])
			}
		}
	}
	return "fetched"
}

// containerTrouble surfaces the waiting or terminated reason, which is the
// single most useful field on a failing pod.
func containerTrouble(st map[string]any) string {
	statuses, _ := st["containerStatuses"].([]any)
	for _, cs := range statuses {
		m, _ := cs.(map[string]any)
		if m == nil {
			continue
		}
		state, _ := m["state"].(map[string]any)
		if w, ok := state["waiting"].(map[string]any); ok && w["reason"] != nil {
			return fmt.Sprintf("%v waiting: %v", m["name"], w["reason"])
		}
		if t, ok := state["terminated"].(map[string]any); ok && t["reason"] != nil {
			return fmt.Sprintf("%v terminated: %v (exit %v)", m["name"], t["reason"], t["exitCode"])
		}
		if last, ok := m["lastState"].(map[string]any); ok {
			if t, ok := last["terminated"].(map[string]any); ok && t["reason"] != nil {
				return fmt.Sprintf("%v last terminated: %v (exit %v)", m["name"], t["reason"], t["exitCode"])
			}
		}
	}
	return ""
}

func scopeLabel(d *Deps, ns string) string {
	if ns == "" {
		return "cluster " + d.Cluster.Name
	}
	return "namespace " + ns
}

// escapeCEL makes a name safe inside a single-quoted CEL literal.
func escapeCEL(s string) string {
	return strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s)
}

// devtronFail turns a Devtron transport error into a structured tool error
// with the recovery hint that actually applies.
func devtronFail(err error, what string) *Result {
	var de *devtron.Error
	if ok := asDevtronError(err, &de); ok {
		switch {
		case de.Unauthorized():
			return Fail(ErrCluster, "forbidden",
				fmt.Sprintf("Devtron refused %s: %s", what, de.Body), false,
				"the API token lacks View permission on this cluster, namespace or kind",
				"do not retry; report the missing permission instead of guessing at the answer")
		case de.Status == 404:
			return Fail(ErrCluster, "not_found", fmt.Sprintf("%s: not found", what), false)
		case de.Status >= 500:
			return Fail(ErrCluster, "devtron_error",
				fmt.Sprintf("Devtron failed %s: HTTP %d", what, de.Status), true,
				"retry once; if it persists the orchestrator or the target cluster is unhealthy")
		}
	}
	if IsCanceled(err) {
		return Fail(ErrCluster, "timeout", what+" timed out", true, "narrow the request and retry once")
	}
	return Fail(ErrCluster, "devtron_request_failed", fmt.Sprintf("%s: %v", what, err), true)
}
