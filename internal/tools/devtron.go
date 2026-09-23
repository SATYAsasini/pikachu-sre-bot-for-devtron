package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/devtron-labs/devtron-sre-agent/internal/devtron"
)

// AppContextArgs asks what Devtron knows about a namespace or workload.
type AppContextArgs struct {
	Namespace string `json:"namespace,omitempty" jsonschema:"the namespace to look up; defaults to the run's namespace"`
	AppName   string `json:"appName,omitempty" jsonschema:"narrow to one application by name fragment"`
}

// DevtronTools answer questions Kubernetes cannot: who owns this namespace,
// what chart is installed here, and when it last changed.
func DevtronTools() []Tool {
	return []Tool{
		Define[AppContextArgs]("devtron.app_context", "devtron",
			"Ask Devtron who owns a namespace: which applications and Helm releases live there, their health, their chart names and when they last deployed. The chart name identifies anonymous workloads, and a recent deployment time is the first thing to correlate an incident against.",
			appContext),
	}
}

func appContext(ctx context.Context, d *Deps, a AppContextArgs) (*Result, error) {
	if d == nil || d.Devtron == nil {
		return Fail(ErrPlatform, "no_devtron_client", "the Devtron client is not configured", false), nil
	}
	ns := a.Namespace
	if ns == "" {
		ns = d.Cluster.Namespace
	}

	key := Key("devtron.app_context", a)
	return Cached(ctx, d, key, func() (*Result, error) {
		envs, err := d.Devtron.EnvironmentsInCluster(ctx, d.Cluster.ID)
		if err != nil {
			return devtronFail(err, "list environments"), nil
		}
		var env *devtron.Environment
		for i := range envs {
			if ns != "" && envs[i].Namespace == ns {
				env = &envs[i]
				break
			}
			if d.Cluster.EnvID != 0 && envs[i].EnvironmentID == d.Cluster.EnvID {
				env = &envs[i]
				break
			}
		}

		data := map[string]any{"cluster": d.Cluster.Name, "namespace": ns}
		var notes []string

		if env == nil {
			notes = append(notes, fmt.Sprintf("namespace %s is not mapped to a Devtron environment; it is probably not managed by Devtron", ns))
			return &Result{
				Summary:      fmt.Sprintf("no Devtron environment owns %s/%s", d.Cluster.Name, ns),
				Data:         data,
				Freshness:    Now("devtron"),
				AgentContext: map[string]any{"notes": notes},
			}, nil
		}
		data["environment"] = env

		apps, _, err := d.Devtron.DevtronApps(ctx, devtron.AppListFilter{
			Environments:  []int{env.EnvironmentID},
			AppNameSearch: a.AppName,
			Size:          25,
		})
		if err != nil {
			notes = append(notes, "could not list Devtron applications: "+err.Error())
		} else {
			data["devtronApps"] = summariseApps(apps, env.EnvironmentID)
		}

		helm, err := d.Devtron.HelmAppsInEnvironment(ctx, *env)
		if err != nil {
			notes = append(notes, "could not list Helm releases: "+err.Error())
		} else {
			data["helmApps"] = summariseHelm(helm, a.AppName)
		}

		return &Result{
			Summary:   summariseContext(env, data),
			Data:      data,
			Freshness: Now("devtron"),
			AgentContext: map[string]any{
				"notes": notes,
				"hint":  "chartName identifies what a workload actually is. Pass it to knowledge.identify before reasoning about an anonymous StatefulSet or Deployment.",
			},
		}, nil
	})
}

func summariseApps(apps []devtron.DevtronApp, envID int) []map[string]any {
	out := make([]map[string]any, 0, len(apps))
	for _, a := range apps {
		row := map[string]any{"appId": a.AppID, "appName": a.AppName}
		for _, e := range a.Environments {
			if e.EnvironmentID == envID {
				row["appStatus"] = e.AppStatus
				row["deploymentStatus"] = e.Status
				row["lastDeployedTime"] = e.LastDeployedTime
				break
			}
		}
		out = append(out, row)
	}
	return out
}

func summariseHelm(apps []devtron.HelmApp, nameLike string) []map[string]any {
	out := make([]map[string]any, 0, len(apps))
	for _, a := range apps {
		if nameLike != "" && !strings.Contains(strings.ToLower(a.AppName), strings.ToLower(nameLike)) {
			continue
		}
		out = append(out, map[string]any{
			"appName": a.AppName, "chartName": a.ChartName, "chartVersion": a.ChartVersion,
			"appStatus": a.AppStatus, "lastDeployedAt": a.LastDeployedAt,
		})
	}
	return out
}

func summariseContext(env *devtron.Environment, data map[string]any) string {
	nApps := len(asMapSlice(data["devtronApps"]))
	nHelm := len(asMapSlice(data["helmApps"]))
	var unhealthy []string
	for _, a := range asMapSlice(data["devtronApps"]) {
		if s, _ := a["appStatus"].(string); s != "" && s != "Healthy" {
			unhealthy = append(unhealthy, fmt.Sprintf("%v=%s", a["appName"], s))
		}
	}
	s := fmt.Sprintf("environment %s (%s): %d Devtron apps, %d Helm releases",
		env.EnvironmentName, env.Namespace, nApps, nHelm)
	if len(unhealthy) > 0 {
		if len(unhealthy) > 5 {
			unhealthy = append(unhealthy[:5], "…")
		}
		s += "; unhealthy: " + strings.Join(unhealthy, ", ")
	}
	return s
}

func asMapSlice(v any) []map[string]any {
	s, _ := v.([]map[string]any)
	return s
}
