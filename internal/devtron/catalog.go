package devtron

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Cluster is a cluster Devtron manages.
type Cluster struct {
	ID          int    `json:"id"`
	ClusterName string `json:"cluster_name"`
	ServerURL   string `json:"server_url,omitempty"`
	ErrorInCx   string `json:"errorInConnecting,omitempty"`
	IsVirtual   bool   `json:"isVirtualCluster,omitempty"`
}

// Environment is one Devtron environment, flattened out of the grouped
// /env/autocomplete/helm response so every field a tool needs sits together.
// Resolve this once per run and reuse it: every other call takes one of its
// fields.
type Environment struct {
	EnvironmentID   int    `json:"environmentId"`
	EnvironmentName string `json:"environmentName"`
	Namespace       string `json:"namespace"`
	ClusterID       int    `json:"clusterId"`
	ClusterName     string `json:"clusterName"`
	IsVirtual       bool   `json:"isVirtualCluster,omitempty"`
}

// NamespaceKey is the "{clusterId}_{namespace}" form the app list filter wants.
func (e Environment) NamespaceKey() string {
	return strconv.Itoa(e.ClusterID) + "_" + e.Namespace
}

// Clusters lists the clusters this token can actually work with.
//
// Devtron's cluster autocomplete filters on explicit *cluster-level* RBAC by
// default, and a token scoped to environments and applications — which is the
// normal shape of a view-only token — gets an empty list from it while being
// perfectly able to investigate those environments. Observed on a live
// install: cluster/autocomplete returns [] while env/autocomplete/helm
// returns every cluster the token uses.
//
// So the environment mapping is the primary source, because a cluster we can
// reach through an environment is a cluster we can investigate. The
// autocomplete endpoint is used to enrich and to catch clusters that have no
// environments yet.
func (c *Client) Clusters(ctx context.Context) ([]Cluster, error) {
	byID := map[int]Cluster{}

	// 1. Whatever cluster-level RBAC grants outright.
	q := url.Values{}
	q.Set("auth", "true")
	var scoped []Cluster
	authErr := c.get(ctx, "/orchestrator/cluster/autocomplete", q, &scoped)
	for _, cl := range scoped {
		byID[cl.ID] = cl
	}

	// 2. Every cluster reachable through an environment.
	envs, envErr := c.Environments(ctx)
	if envErr == nil {
		for _, e := range envs {
			if e.ClusterID == 0 {
				continue
			}
			if _, seen := byID[e.ClusterID]; !seen {
				byID[e.ClusterID] = Cluster{ID: e.ClusterID, ClusterName: e.ClusterName, IsVirtual: e.IsVirtual}
			}
		}
	}

	// 3. Nothing at all: ask without the RBAC filter before concluding the
	//    token is useless, and let Devtron reject the read if it must.
	if len(byID) == 0 {
		q.Set("auth", "false")
		var all []Cluster
		if err := c.get(ctx, "/orchestrator/cluster/autocomplete", q, &all); err == nil {
			for _, cl := range all {
				byID[cl.ID] = cl
			}
		}
	}

	if len(byID) == 0 {
		if authErr != nil {
			return nil, authErr
		}
		if envErr != nil {
			return nil, envErr
		}
		return []Cluster{}, nil
	}

	out := make([]Cluster, 0, len(byID))
	for _, cl := range byID {
		out = append(out, cl)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ClusterName < out[j].ClusterName })
	return out, nil
}

// ClusterByName resolves a cluster name, matching exactly first and then
// ignoring case, which is what the intelligence endpoint does too.
func (c *Client) ClusterByName(ctx context.Context, name string) (*Cluster, error) {
	cs, err := c.Clusters(ctx)
	if err != nil {
		return nil, err
	}
	for i := range cs {
		if cs[i].ClusterName == name {
			return &cs[i], nil
		}
	}
	for i := range cs {
		if strings.EqualFold(cs[i].ClusterName, name) {
			return &cs[i], nil
		}
	}
	return nil, fmt.Errorf("no Devtron cluster named %q", name)
}

// Environments lists every environment, flattened. This is the step-0 call:
// it is the only place that maps an environment name to the environmentId,
// clusterId and namespace the other endpoints need.
func (c *Client) Environments(ctx context.Context) ([]Environment, error) {
	// The response is grouped by cluster.
	var grouped []struct {
		ClusterID    int    `json:"clusterId"`
		ClusterName  string `json:"clusterName"`
		IsVirtual    bool   `json:"isVirtualCluster"`
		Environments []struct {
			EnvironmentID   int    `json:"environmentId"`
			EnvironmentName string `json:"environmentName"`
			Namespace       string `json:"namespace"`
		} `json:"environments"`
	}
	if err := c.get(ctx, "/orchestrator/env/autocomplete/helm", nil, &grouped); err != nil {
		return nil, err
	}
	var out []Environment
	for _, g := range grouped {
		for _, e := range g.Environments {
			out = append(out, Environment{
				EnvironmentID:   e.EnvironmentID,
				EnvironmentName: e.EnvironmentName,
				Namespace:       e.Namespace,
				ClusterID:       g.ClusterID,
				ClusterName:     g.ClusterName,
				IsVirtual:       g.IsVirtual,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ClusterName != out[j].ClusterName {
			return out[i].ClusterName < out[j].ClusterName
		}
		return out[i].EnvironmentName < out[j].EnvironmentName
	})
	return out, nil
}

// EnvironmentsInCluster filters Environments to one cluster.
func (c *Client) EnvironmentsInCluster(ctx context.Context, clusterID int) ([]Environment, error) {
	all, err := c.Environments(ctx)
	if err != nil {
		return nil, err
	}
	var out []Environment
	for _, e := range all {
		if e.ClusterID == clusterID {
			out = append(out, e)
		}
	}
	return out, nil
}

// AppEnvironment is one environment row of a Devtron application.
type AppEnvironment struct {
	EnvironmentID    int    `json:"environmentId"`
	EnvironmentName  string `json:"environmentName"`
	Namespace        string `json:"namespace"`
	ClusterName      string `json:"clusterName"`
	Status           string `json:"status"`
	AppStatus        string `json:"appStatus"`
	LastDeployedTime string `json:"lastDeployedTime"`
}

// DevtronApp is a Devtron (CI/CD) application with its environment rows.
type DevtronApp struct { //nolint:revive // Devtron's own name, distinct from Helm apps
	AppID        int              `json:"appId"`
	AppName      string           `json:"appName"`
	ProjectID    int              `json:"projectId"`
	Environments []AppEnvironment `json:"environments"`
}

// AppListFilter narrows POST /app/list/v2. Set Environments or Namespaces,
// not both: they are alternative ways to say the same thing.
type AppListFilter struct {
	AppNameSearch string   `json:"appNameSearch,omitempty"`
	Environments  []int    `json:"environments,omitempty"`
	Namespaces    []string `json:"namespaces,omitempty"` // "{clusterId}_{namespace}"
	Teams         []int    `json:"teams,omitempty"`
	AppStatuses   []string `json:"appStatuses,omitempty"`
	Statuses      []string `json:"statuses,omitempty"`
	SortBy        string   `json:"sortBy,omitempty"`    // appNameSort | lastDeployedSort
	SortOrder     string   `json:"sortOrder,omitempty"` // ASC | DESC
	Offset        int      `json:"offset"`
	Size          int      `json:"size,omitempty"`
}

// DevtronApps lists Devtron applications matching a filter.
func (c *Client) DevtronApps(ctx context.Context, f AppListFilter) ([]DevtronApp, int, error) {
	if f.Size == 0 {
		f.Size = 50
	}
	if f.SortBy == "" {
		f.SortBy = "appNameSort"
		f.SortOrder = "ASC"
	}
	var res struct {
		AppContainers []DevtronApp `json:"appContainers"`
		AppCount      int          `json:"appCount"`
	}
	if err := c.post(ctx, "/orchestrator/app/list/v2", f, &res); err != nil {
		return nil, 0, err
	}
	return res.AppContainers, res.AppCount, nil
}

// SearchApps is the cheap name-only autocomplete, with no environment filter.
func (c *Client) SearchApps(ctx context.Context, nameLike string, size int) ([]struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}, error) {
	q := url.Values{}
	if nameLike != "" {
		q.Set("appName", nameLike)
	}
	if size > 0 {
		q.Set("size", strconv.Itoa(size))
	}
	var out []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	err := c.get(ctx, "/orchestrator/app/autocomplete", q, &out)
	return out, err
}

// HelmEnvDetail is the environment an installed Helm release sits in.
type HelmEnvDetail struct {
	EnvironmentID   int    `json:"environmentId"`
	EnvironmentName string `json:"environmentName"`
	Namespace       string `json:"namespace"`
	ClusterID       int    `json:"clusterId"`
	ClusterName     string `json:"clusterName"`
}

// HelmApp is an installed Helm release Devtron knows about. ChartName is the
// most useful field in this whole package for identification: it names the
// product, which is how the agent knows a failing workload is Postgres or
// Kafka rather than an anonymous StatefulSet.
type HelmApp struct {
	AppID             string        `json:"appId"`
	AppName           string        `json:"appName"`
	ChartName         string        `json:"chartName"`
	ChartVersion      string        `json:"chartVersion"`
	ChartAvatar       string        `json:"chartAvatar,omitempty"`
	AppStatus         string        `json:"appStatus"`
	ProjectID         int           `json:"projectId"`
	LastDeployedAt    string        `json:"lastDeployedAt"`
	EnvironmentDetail HelmEnvDetail `json:"environmentDetail"`
}

// HelmApps lists installed Helm releases in a cluster. The server offers no
// environment or name filter, so narrowing happens here.
func (c *Client) HelmApps(ctx context.Context, clusterID int) ([]HelmApp, error) {
	q := url.Values{}
	q.Set("clusterIds", strconv.Itoa(clusterID))
	var res struct {
		HelmApps []HelmApp `json:"helmApps"`
	}
	if err := c.get(ctx, "/orchestrator/application", q, &res); err != nil {
		return nil, err
	}
	return res.HelmApps, nil
}

// HelmAppsInEnvironment filters HelmApps to one environment. External
// releases can come back without an environmentId, so a cluster+namespace
// match is accepted as well.
func (c *Client) HelmAppsInEnvironment(ctx context.Context, env Environment) ([]HelmApp, error) {
	all, err := c.HelmApps(ctx, env.ClusterID)
	if err != nil {
		return nil, err
	}
	var out []HelmApp
	for _, a := range all {
		d := a.EnvironmentDetail
		if (env.EnvironmentID != 0 && d.EnvironmentID == env.EnvironmentID) ||
			(d.ClusterID == env.ClusterID && d.Namespace == env.Namespace) {
			out = append(out, a)
		}
	}
	return out, nil
}
