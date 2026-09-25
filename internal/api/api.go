// Package api serves the product REST contract and the SSE run stream.
//
// This is deliberately a thin projection. The agent runtime is ADK's, but the
// dashboard speaks the product's own vocabulary — clusters, alerts, runs,
// verdicts, remediation — rather than ADK's events and sessions.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/devtron-labs/devtron-sre-agent/internal/capability"
	"github.com/devtron-labs/devtron-sre-agent/internal/config"
	"github.com/devtron-labs/devtron-sre-agent/internal/devtron"
	"github.com/devtron-labs/devtron-sre-agent/internal/incidents"
	"github.com/devtron-labs/devtron-sre-agent/internal/knowledge"
	"github.com/devtron-labs/devtron-sre-agent/internal/runs"
	"github.com/devtron-labs/devtron-sre-agent/internal/webui"
	"github.com/devtron-labs/devtron-sre-agent/internal/worker"
)

// Server holds everything the handlers need.
type Server struct {
	Cfg        *config.Config
	Log        *slog.Logger
	Devtron    *devtron.Client
	Discoverer *devtron.Discoverer
	Knowledge  *knowledge.Catalog
	Runs       *runs.Service
	// Incidents are the alerts we have taken responsibility for, as distinct
	// from the live feed the cluster reports.
	Incidents *incidents.Store
	Worker    *worker.Worker
	// Caps knows which clusters actually answer, as opposed to which ones
	// Devtron lists.
	Caps *capability.Service
	// UI serves everything outside /v1. Nil means the dashboard embedded in
	// the binary.
	UI http.Handler
}

// Handler builds the router.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer, middleware.Compress(5))
	r.Use(cors)

	r.Route("/v1", func(r chi.Router) {
		r.Get("/healthz", s.health)
		r.Get("/config", s.config)
		r.Get("/harness", s.harness)

		// Stateless. A chat is not a run and leaves no record; see chat.go.
		r.Post("/chat", s.chat)

		// One endpoint for "which clusters exist and what can be read from
		// them". /capabilities used to serve the same measurement behind a
		// different shape, so the picker and the setup screen could disagree
		// about how many clusters were usable.
		r.Get("/clusters", s.listClusters)
		r.Post("/clusters/refresh", s.refreshClusters)
		// How far the sweep has got. It runs for a minute or more on a large
		// install, and a caller that cannot see progress cannot tell it apart
		// from a hang.
		r.Get("/clusters/sweep", s.sweepProgress)
		r.Get("/clusters/{clusterId}/environments", s.listClusterEnvironments)
		r.Get("/clusters/{clusterId}/monitoring", s.clusterMonitoring)
		// Discovery picks by heuristic, and on a cluster running both vmalert
		// and an Alertmanager the heuristic is a coin toss. These let the
		// operator settle it; DELETE puts the cluster back on discovery.
		r.Put("/clusters/{clusterId}/monitoring", s.chooseClusterMonitoring)
		r.Delete("/clusters/{clusterId}/monitoring", s.resetClusterMonitoring)
		r.Get("/environments", s.listEnvironments)
		r.Get("/apps", s.listApps)
		r.Get("/helm-apps", s.listHelmApps)
		r.Get("/alerts", s.listAlerts)

		// Tracked alerts: the ones we own, with a lifecycle. /alerts above is
		// the live feed, which is a different thing entirely.
		r.Get("/incidents", s.listIncidents)
		r.Post("/incidents", s.trackIncident)
		r.Get("/incidents/{id}", s.getIncident)
		r.Patch("/incidents/{id}", s.patchIncident)

		// Per-cluster alert rules: what to show, what to mute, what to
		// investigate unprompted, and how urgent each one is.
		r.Get("/rules", s.getRules)
		r.Put("/rules", s.putRules)
		r.Post("/rules/preview", s.previewRules)
		r.Post("/rules/notify/test", s.testNotify)

		r.Post("/runs", s.createRun)
		r.Get("/runs", s.listRuns)
		r.Get("/runs/{id}", s.getRun)
		r.Get("/runs/{id}/events", s.runEvents)
		r.Get("/runs/{id}/stream", s.streamRun)
		r.Post("/runs/{id}/cancel", s.cancelRun)
		// A run is a detail view of an alert. This is the way back.
		r.Get("/runs/{id}/alert", s.incidentForRun)

		r.Get("/settings", s.getSettings)
		r.Put("/settings", s.putSettings)
		r.Post("/settings/test", s.testSettings)

		r.Get("/knowledge", s.listKnowledge)
		r.Get("/knowledge/{id}", s.getKnowledge)
	})

	// The dashboard on the same origin, so a single container and a single
	// Ingress path serve both. Unknown /v1 paths stay 404s from the API
	// router above and never fall through to index.html.
	ui := s.UI
	if ui == nil {
		ui = webui.Handler(webui.Dist())
	}
	r.Handle("/*", ui)
	return r
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) config(w http.ResponseWriter, _ *http.Request) {
	// The live client, not the static config: a host saved through Settings
	// never reaches Cfg, so reading Cfg reported an empty URL on exactly the
	// installations that were configured correctly.
	devtronURL := s.Devtron.BaseURL()
	if devtronURL == "" {
		devtronURL = s.Cfg.Devtron.URL
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"devtronUrl": devtronURL,
		"models": map[string]string{
			"provider": s.Cfg.Models.Provider,
			"fast":     s.Cfg.Models.Fast,
			"strong":   s.Cfg.Models.Strong,
		},
		"features": s.Cfg.Features,
		// What is still missing, so the UI can say so plainly instead of
		// letting the first run fail with a confusing error.
		"ready": map[string]any{
			"devtron":        s.Devtron.BaseURL() != "" && s.Devtron.HasToken(),
			"model":          s.Cfg.ModelCredentialMissing() == "",
			"modelBlockedBy": s.Cfg.ModelCredentialMissing(),
		},
	})
}

// listClusters serves the intersection of what Devtron lists and what
// actually answered a probe. Offering a cluster the orchestrator cannot reach
// is offering a run that will hang for its whole timeout and conclude
// nothing, so those are left out unless explicitly asked for with ?all=true.
func (s *Server) listClusters(w http.ResponseWriter, r *http.Request) {
	// A sweep has never run, or the last one has aged out: kick one off now
	// so the next view is narrowed, without making this request wait.
	if s.Caps.Stale() {
		s.Caps.RefreshInBackground(r.Context())
	}
	s.writeClusterRows(w, r)
}

// writeClusterRows serves the intersection of what Devtron lists and what
// actually answered a probe. Offering a cluster the orchestrator cannot read
// is offering a run that will hang for its whole timeout and conclude
// nothing, so those are left out unless explicitly asked for with ?all=true.
func (s *Server) writeClusterRows(w http.ResponseWriter, r *http.Request) {
	all := r.URL.Query().Get("all") == "true"
	cs, err := s.Caps.Clusters(r.Context(), !all)
	if err != nil {
		writeDevtronError(w, err)
		return
	}
	// One query for the whole list rather than one per card, so the cluster
	// picker can say which clusters have a pinned monitoring stack without
	// starting a discovery walk against every one of them.
	pinned, err := s.Runs.Store.PinnedMonitoringClusters(r.Context())
	if err != nil {
		pinned = map[int]bool{}
	}

	out := make([]map[string]any, 0, len(cs))
	for _, c := range cs {
		row := map[string]any{
			"id": c.ID, "clusterName": c.ClusterName,
			"serverUrl": c.ServerURL, "isVirtualCluster": c.IsVirtual,
			"errorInConnecting": c.ErrorInCx,
		}
		if pinned[c.ID] {
			row["monitoringPinned"] = true
		}
		// The measurement rides along, so the setup screen and the picker read
		// the same row rather than each fetching its own idea of the truth.
		if measured := s.Caps.Get(c.ID); measured != nil {
			row["reach"] = measured.Reach
			row["reachWhy"] = measured.Reach.Why()
			row["probedAt"] = measured.ProbedAt
			row["latencyMs"] = measured.LatencyMs
			row["investigable"] = measured.Reach.Investigable()
			if measured.Detail != "" {
				row["detail"] = measured.Detail
			}
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}

// refreshClusters re-measures every cluster and returns the same rows as
// listClusters, so a caller that refreshes can drop the response straight into
// the cache it already had.
// refreshClusters starts a sweep and returns what is known now.
//
// It used to block until every cluster had been measured. That was tolerable
// when a probe gave up after 8 seconds and a dozen ran at once; it is not now
// that probes are given a fair timeout and are deliberately not piled onto
// the orchestrator. Each cluster is published the moment it is measured, so
// the caller polls /clusters/sweep and re-reads the list as it fills.
func (s *Server) refreshClusters(w http.ResponseWriter, r *http.Request) {
	s.Caps.RefreshInBackground(r.Context())
	s.writeClusterRows(w, r)
}

func (s *Server) sweepProgress(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Caps.Progress())
}

func (s *Server) listClusterEnvironments(w http.ResponseWriter, r *http.Request) {
	id, err := intParam(r, "clusterId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_cluster_id", err.Error())
		return
	}
	envs, err := s.Devtron.EnvironmentsInCluster(r.Context(), id)
	if err != nil {
		writeDevtronError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(envs))
}

func (s *Server) clusterMonitoring(w http.ResponseWriter, r *http.Request) {
	id, err := intParam(r, "clusterId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_cluster_id", err.Error())
		return
	}
	q := r.URL.Query()
	name := q.Get("clusterName")

	// ?probe=all measures every candidate rather than stopping at the first
	// that answers. It is what the cluster's monitoring screen asks for:
	// choosing between endpoints means seeing all of them, and "never asked"
	// is not a useful thing to show somebody who is being asked to pick.
	var stack *devtron.MonitoringStack
	if q.Get("probe") == "all" {
		stack, err = s.Discoverer.Probe(r.Context(), id, name)
	} else {
		if q.Get("refresh") == "true" {
			s.Discoverer.Invalidate(id)
		}
		stack, err = s.Discoverer.Get(r.Context(), id, name)
	}
	if err != nil {
		writeDevtronError(w, err)
		return
	}
	// Candidates are already on the stack; they matter to the UI because
	// discovery picks one of them by name heuristics and can pick wrong.
	writeJSON(w, http.StatusOK, stack)
}

// chooseClusterMonitoring pins which discovered endpoints a cluster uses.
//
// Only a Service discovery has already reported can be pinned. That is the
// line this keeps: the operator settles a choice between measured options,
// they do not get to type in an address, and a pin that stops resolving falls
// back to discovery with a note rather than silently.
func (s *Server) chooseClusterMonitoring(w http.ResponseWriter, r *http.Request) {
	id, err := intParam(r, "clusterId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_cluster_id", err.Error())
		return
	}
	var body struct {
		ClusterName string        `json:"clusterName"`
		Metrics     *devtron.Pick `json:"metrics"`
		Alerts      *devtron.Pick `json:"alerts"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_body", err.Error())
		return
	}
	choice := devtron.Choice{Metrics: body.Metrics, Alerts: body.Alerts}
	if choice.Metrics != nil && !choice.Metrics.Valid() {
		writeError(w, http.StatusBadRequest, "bad_pick", "a metrics pick needs a namespace and a name")
		return
	}
	if choice.Alerts != nil && !choice.Alerts.Valid() {
		writeError(w, http.StatusBadRequest, "bad_pick", "an alert source pick needs a namespace and a name")
		return
	}

	if err := s.Runs.Store.SaveMonitoringChoice(r.Context(), id, body.ClusterName, choice, "ui"); err != nil {
		writeError(w, http.StatusInternalServerError, "save_failed", err.Error())
		return
	}
	// The cached stack was measured under the old choice, so it is now a
	// statement about a decision nobody is making any more.
	s.Discoverer.Invalidate(id)

	stack, err := s.Discoverer.Probe(r.Context(), id, body.ClusterName)
	if err != nil {
		writeDevtronError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stack)
}

// resetClusterMonitoring drops the pins and goes back to discovery.
func (s *Server) resetClusterMonitoring(w http.ResponseWriter, r *http.Request) {
	id, err := intParam(r, "clusterId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_cluster_id", err.Error())
		return
	}
	if err := s.Runs.Store.ClearMonitoringChoice(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "save_failed", err.Error())
		return
	}
	s.Discoverer.Invalidate(id)

	stack, err := s.Discoverer.Probe(r.Context(), id, r.URL.Query().Get("clusterName"))
	if err != nil {
		writeDevtronError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stack)
}

func (s *Server) listEnvironments(w http.ResponseWriter, r *http.Request) {
	envs, err := s.Devtron.Environments(r.Context())
	if err != nil {
		writeDevtronError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(envs))
}

func (s *Server) listApps(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := devtron.AppListFilter{
		AppNameSearch: q.Get("search"),
		Size:          intQuery(q.Get("size"), 50),
	}
	if envID := intQuery(q.Get("environmentId"), 0); envID > 0 {
		f.Environments = []int{envID}
	}
	if st := q.Get("status"); st != "" {
		f.AppStatuses = []string{st}
	}
	apps, _, err := s.Devtron.DevtronApps(r.Context(), f)
	if err != nil {
		writeDevtronError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(apps))
}

func (s *Server) listHelmApps(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	clusterID := intQuery(q.Get("clusterId"), 0)
	if clusterID == 0 {
		writeError(w, http.StatusBadRequest, "missing_cluster_id", "clusterId is required")
		return
	}
	apps, err := s.Devtron.HelmApps(r.Context(), clusterID)
	if err != nil {
		writeDevtronError(w, err)
		return
	}
	envID := intQuery(q.Get("environmentId"), 0)
	search := strings.ToLower(q.Get("search"))
	out := make([]devtron.HelmApp, 0, len(apps))
	for _, a := range apps {
		if envID > 0 && a.EnvironmentDetail.EnvironmentID != envID {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(a.AppName), search) &&
			!strings.Contains(strings.ToLower(a.ChartName), search) {
			continue
		}
		out = append(out, a)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listKnowledge(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var comps []*knowledge.Component
	switch {
	case q.Get("q") != "":
		comps = s.Knowledge.Search(q.Get("q"), 50)
	case q.Get("class") != "":
		comps = s.Knowledge.OfClass(knowledge.Class(q.Get("class")))
	default:
		comps = s.Knowledge.All()
	}
	// The listing omits the document body; the detail endpoint carries it.
	out := make([]map[string]any, 0, len(comps))
	for _, c := range comps {
		out = append(out, map[string]any{
			"id": c.ID, "name": c.Name, "class": c.Class, "skill": c.Skill,
			"summary": c.Summary, "repo": c.Repo, "metricCount": len(c.Metrics),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getKnowledge(w http.ResponseWriter, r *http.Request) {
	c, ok := s.Knowledge.Get(chi.URLParam(r, "id"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown_component", "no component with that id")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		return
	}
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": msg},
	})
}

// writeDevtronError preserves the distinction the UI needs: a token problem
// is actionable by the operator, a 5xx is not.
func writeDevtronError(w http.ResponseWriter, err error) {
	var de *devtron.Error
	if errors.As(err, &de) {
		switch {
		case de.Unauthorized():
			writeError(w, http.StatusBadGateway, "devtron_unauthorized",
				"Devtron refused the API token: "+de.Body)
			return
		case de.Status == http.StatusNotFound:
			writeError(w, http.StatusNotFound, "devtron_not_found", de.Body)
			return
		}
		writeError(w, http.StatusBadGateway, "devtron_error", de.Error())
		return
	}
	writeError(w, http.StatusBadGateway, "devtron_unreachable", err.Error())
}

// asDevtron is errors.As specialised for the Devtron transport error.
func asDevtron(err error, target **devtron.Error) bool { return errors.As(err, target) }

func itoa(n int) string { return strconv.Itoa(n) }

func intParam(r *http.Request, name string) (int, error) {
	v := chi.URLParam(r, name)
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, errors.New(name + " must be a positive integer")
	}
	return n, nil
}

func intQuery(v string, def int) int {
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// orEmpty makes a nil slice serialise as [] rather than null, which the UI
// contract promises on every list endpoint.
func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
