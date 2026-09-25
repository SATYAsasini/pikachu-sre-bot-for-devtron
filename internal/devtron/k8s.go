package devtron

import (
	"context"
	"fmt"
	"strings"
)

// GVK identifies a Kubernetes kind. Devtron capitalises these field names,
// unlike almost every other JSON API, and gets them wrong silently if you
// use the lowercase spelling.
type GVK struct {
	Group   string `json:"Group"`
	Version string `json:"Version"`
	Kind    string `json:"Kind"`
}

// Common kinds, spelled once so callers cannot mistype the empty core group.
var (
	GVKPod         = GVK{Group: "", Version: "v1", Kind: "Pod"}
	GVKService     = GVK{Group: "", Version: "v1", Kind: "Service"}
	GVKEvent       = GVK{Group: "", Version: "v1", Kind: "Event"}
	GVKConfigMap   = GVK{Group: "", Version: "v1", Kind: "ConfigMap"}
	GVKNode        = GVK{Group: "", Version: "v1", Kind: "Node"}
	GVKNamespace   = GVK{Group: "", Version: "v1", Kind: "Namespace"}
	GVKPVC         = GVK{Group: "", Version: "v1", Kind: "PersistentVolumeClaim"}
	GVKDeployment  = GVK{Group: "apps", Version: "v1", Kind: "Deployment"}
	GVKStatefulSet = GVK{Group: "apps", Version: "v1", Kind: "StatefulSet"}
	GVKDaemonSet   = GVK{Group: "apps", Version: "v1", Kind: "DaemonSet"}
	GVKReplicaSet  = GVK{Group: "apps", Version: "v1", Kind: "ReplicaSet"}
	GVKIngress     = GVK{Group: "networking.k8s.io", Version: "v1", Kind: "Ingress"}
	GVKJob         = GVK{Group: "batch", Version: "v1", Kind: "Job"}
)

// ParseGVK reads "apps/v1/Deployment" or "v1/Service" or plain "Pod".
func ParseGVK(s string) (GVK, error) {
	parts := strings.Split(strings.TrimSpace(s), "/")
	switch len(parts) {
	case 1:
		k := known(parts[0])
		if k == (GVK{}) {
			return GVK{}, fmt.Errorf("kind %q needs a group/version, e.g. apps/v1/%s", parts[0], parts[0])
		}
		return k, nil
	case 2:
		return GVK{Group: "", Version: parts[0], Kind: parts[1]}, nil
	case 3:
		return GVK{Group: parts[0], Version: parts[1], Kind: parts[2]}, nil
	}
	return GVK{}, fmt.Errorf("cannot parse %q as group/version/kind", s)
}

func known(kind string) GVK {
	for _, g := range []GVK{GVKPod, GVKService, GVKEvent, GVKConfigMap, GVKNode, GVKPVC,
		GVKDeployment, GVKStatefulSet, GVKDaemonSet, GVKReplicaSet, GVKIngress, GVKJob} {
		if strings.EqualFold(g.Kind, kind) {
			return g
		}
	}
	return GVK{}
}

// ResourceQuery asks for a list of one kind in one cluster.
type ResourceQuery struct {
	ClusterID int
	// Namespace empty means every namespace the token can read.
	Namespace string
	GVK       GVK
	// LabelSelector entries are "key=value" or "key in (a,b)".
	LabelSelector []string
	// FieldSelector entries are "status.phase=Running" and the like.
	FieldSelector []string
	// Filter is a CEL expression over `self`. Setting it changes the
	// response from table rows to raw objects, which ResourceList reports.
	Filter string
}

// ResourceList is the answer to a ResourceQuery. Devtron normally returns
// table rows (Headers plus Rows); with a CEL Filter it returns whole objects
// in Objects instead.
type ResourceList struct {
	Headers []string         `json:"headers"`
	Rows    []map[string]any `json:"rows"`
	Objects []map[string]any `json:"objects"`
}

// Len is the number of results however they came back.
func (r *ResourceList) Len() int {
	if len(r.Objects) > 0 {
		return len(r.Objects)
	}
	return len(r.Rows)
}

// Names pulls the name column out of either shape.
func (r *ResourceList) Names() []string {
	var out []string
	for _, o := range r.Objects {
		if md, ok := o["metadata"].(map[string]any); ok {
			if n, ok := md["name"].(string); ok {
				out = append(out, n)
			}
		}
	}
	for _, row := range r.Rows {
		if n, ok := row["name"].(string); ok {
			out = append(out, n)
		}
	}
	return out
}

// ListResources runs a read-only list through the orchestrator. This is the
// workhorse: it is how the agent finds a monitoring Service, reads events,
// checks pods and inspects any other kind, all without a kubeconfig.
func (c *Client) ListResources(ctx context.Context, q ResourceQuery) (*ResourceList, error) {
	if q.ClusterID == 0 {
		return nil, fmt.Errorf("clusterId is required")
	}
	if q.GVK.Version == "" || q.GVK.Kind == "" {
		return nil, fmt.Errorf("a group/version/kind is required")
	}
	body := map[string]any{
		"clusterId": q.ClusterID,
		"k8sRequest": map[string]any{
			"resourceIdentifier": map[string]any{
				"namespace":        q.Namespace,
				"groupVersionKind": q.GVK,
			},
		},
	}
	if len(q.LabelSelector) > 0 {
		body["labelSelector"] = q.LabelSelector
	}
	if len(q.FieldSelector) > 0 {
		body["fieldSelector"] = q.FieldSelector
	}
	if q.Filter != "" {
		body["filter"] = q.Filter
	}

	// The shape depends on whether a CEL filter was set, so decode loosely.
	var raw any
	if err := c.post(ctx, "/orchestrator/k8s/resource/list", body, &raw); err != nil {
		return nil, err
	}
	return decodeResourceList(raw), nil
}

func decodeResourceList(raw any) *ResourceList {
	out := &ResourceList{}
	switch v := raw.(type) {
	case []any:
		for _, o := range v {
			if m, ok := o.(map[string]any); ok {
				out.Objects = append(out.Objects, normalizeObject(m))
			}
		}
	case map[string]any:
		for _, h := range asSlice(v["headers"]) {
			if s, ok := h.(string); ok {
				out.Headers = append(out.Headers, s)
			}
		}
		for _, d := range asSlice(v["data"]) {
			m, ok := d.(map[string]any)
			if !ok {
				continue
			}
			out.Rows = append(out.Rows, m)
			// A filtered request is documented as returning bare objects, but
			// what actually comes back is a table row that happens to carry
			// apiVersion, kind and spec alongside the display columns. Callers
			// that want an object were silently getting nothing, so any row
			// rich enough to be one is promoted here rather than at each of
			// the three call sites that made that mistake independently.
			if looksLikeObject(m) {
				out.Objects = append(out.Objects, normalizeObject(m))
			}
		}
		for _, key := range []string{"objects", "items"} {
			for _, d := range asSlice(v[key]) {
				if m, ok := d.(map[string]any); ok {
					out.Objects = append(out.Objects, normalizeObject(m))
				}
			}
		}
	}
	return out
}

// looksLikeObject reports that a table row carries enough of the real object
// to be treated as one.
func looksLikeObject(m map[string]any) bool {
	if _, ok := m["metadata"]; ok {
		return true
	}
	_, hasKind := m["kind"]
	_, hasSpec := m["spec"]
	_, hasAPI := m["apiVersion"]
	return hasKind || hasSpec || hasAPI
}

// normalizeObject gives every object a metadata block, whether it arrived as
// a real Kubernetes object or as a flattened table row with name and
// namespace at the top level.
func normalizeObject(m map[string]any) map[string]any {
	md, _ := m["metadata"].(map[string]any)
	if md == nil {
		md = map[string]any{}
	}
	for _, k := range []string{"name", "namespace"} {
		if _, have := md[k]; have {
			continue
		}
		if v, ok := m[k].(string); ok && v != "" {
			md[k] = v
		}
	}
	if _, have := md["labels"]; !have {
		if l, ok := m["labels"].(map[string]any); ok {
			md["labels"] = l
		}
	}
	m["metadata"] = md
	return m
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}
