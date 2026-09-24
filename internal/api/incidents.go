package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/devtron-labs/devtron-sre-agent/internal/incidents"
	"github.com/devtron-labs/devtron-sre-agent/internal/monitoring"
	"github.com/devtron-labs/devtron-sre-agent/internal/rules"
	"github.com/devtron-labs/devtron-sre-agent/internal/runs"
)

// listIncidents serves the tracked alerts — the dashboard.
//
// These are entities we took responsibility for, not the live feed. The live
// feed is /v1/alerts: a question asked of the cluster and forgotten. The
// difference matters enough that they are separate endpoints rather than a
// flag, because confusing them is how somebody ends up believing an alert is
// being handled when nobody ever claimed it.
func (s *Server) listIncidents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, err := s.Incidents.List(r.Context(), incidents.Filter{
		ClusterID: intQuery(q.Get("clusterId"), 0),
		State:     q.Get("state"),
		Limit:     intQuery(q.Get("limit"), 200),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}

	// The conclusion belongs on the row. The whole point of this screen is
	// seeing what each alert turned out to be without opening anything.
	ids := make([]string, 0, len(list))
	for _, a := range list {
		ids = append(ids, a.ID)
	}
	if found, err := s.Incidents.LatestFindings(r.Context(), ids); err == nil {
		for i := range list {
			if f, ok := found[list[i].ID]; ok {
				f := f
				list[i].Latest = &f
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"alerts": list, "count": len(list)})
}

// getIncident serves one tracked alert with its runs and timeline.
func (s *Server) getIncident(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	a, err := s.Incidents.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no tracked alert with that id")
		return
	}
	if found, err := s.Incidents.LatestFindings(r.Context(), []string{id}); err == nil {
		if f, ok := found[id]; ok {
			a.Latest = &f
		}
	}
	timeline, _ := s.Incidents.Timeline(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{"alert": a, "timeline": timeline})
}

// incidentForRun answers "which alert is this run about?".
//
// The run page is a detail view of an alert's investigation, not a free
// standing object, and without this it has no way back to the thing it was
// answering.
func (s *Server) incidentForRun(w http.ResponseWriter, r *http.Request) {
	id, ok := s.Incidents.ByRun(r.Context(), chi.URLParam(r, "id"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "this run is not attached to a tracked alert")
		return
	}
	a, err := s.Incidents.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "the tracked alert is gone")
		return
	}
	if found, err := s.Incidents.LatestFindings(r.Context(), []string{id}); err == nil {
		if f, ok := found[id]; ok {
			a.Latest = &f
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"alert": a})
}

// trackIncident promotes a live alert into a tracked one, and optionally opens
// an investigation on it.
//
// This is the moment an alert stops being something the cluster is shouting
// and starts being something we own. Tracking and investigating are separate
// flags because they are separate decisions: "this one matters, I will look at
// it myself" is a real answer.
func (s *Server) trackIncident(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ClusterID   int              `json:"clusterId"`
		ClusterName string           `json:"clusterName"`
		Alert       monitoring.Alert `json:"alert"`
		Investigate bool             `json:"investigate"`
		Options     runs.RunOptions  `json:"options,omitzero"`

		// Whatever the operator had scoped when they hit Debug. An alert
		// carries its own namespace, but the app and environment are context
		// only the UI knows, and dropping them here would quietly make an
		// alert-triggered run narrower than the same run started from the
		// run endpoint.
		EnvironmentID int    `json:"environmentId,omitempty"`
		Namespace     string `json:"namespace,omitempty"`
		AppName       string `json:"appName,omitempty"`
		AppType       string `json:"appType,omitempty"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_body", err.Error())
		return
	}
	if body.ClusterID == 0 || body.Alert.Name == "" {
		writeError(w, http.StatusBadRequest, "missing_alert", "clusterId and an alert are required")
		return
	}

	// Priority comes from the same rules that decide the alert list, so what
	// lands on the dashboard matches what was on screen.
	priority := rulesPriority(r, s, body.ClusterID, body.Alert)

	tracked, created, err := s.Incidents.Track(r.Context(), body.ClusterID, body.ClusterName,
		body.Alert, priority, incidents.OriginManual)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "track_failed", err.Error())
		return
	}

	out := map[string]any{"alert": tracked, "created": created}

	if body.Investigate {
		// One investigation at a time. A second run against an alert that is
		// already being looked at spends a Devtron call and a model to
		// produce the same answer twice.
		if inflight := openRunFor(r, s, tracked); inflight != "" {
			s.Incidents.Log(r.Context(), tracked.ID, incidents.LogRunSuppressed,
				"already investigating in run "+inflight, "ui")
			out["runId"] = inflight
			out["alreadyRunning"] = true
			writeJSON(w, http.StatusOK, out)
			return
		}

		alertJSON, _ := json.Marshal(body.Alert)
		namespace := body.Alert.Namespace
		if namespace == "" {
			namespace = body.Namespace
		}
		req := runs.CreateRequest{
			ClusterID:     body.ClusterID,
			ClusterName:   body.ClusterName,
			EnvironmentID: body.EnvironmentID,
			Namespace:     namespace,
			AppName:       body.AppName,
			AppType:       body.AppType,
			Alert:         alertJSON,
			Options:       body.Options,
		}
		// The same preflight the run endpoint applies. An investigation that
		// cannot read the cluster is not worth queueing, and finding that out
		// after the fact is what the check exists to prevent.
		if blocked := s.preflightBlock(r, req); blocked != "" {
			writeError(w, http.StatusPreconditionFailed, "not_investigable", blocked)
			return
		}
		run, err := s.Runs.Store.Create(r.Context(), req)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "create_failed", err.Error())
			return
		}
		s.Worker.Submit(r.Context(), run.ID)
		_ = s.Incidents.Attach(r.Context(), tracked.ID, run.ID)
		out["runId"] = run.ID
	}

	writeJSON(w, http.StatusOK, out)
}

// patchIncident changes state or notes.
func (s *Server) patchIncident(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		State *string `json:"state,omitempty"`
		Notes *string `json:"notes,omitempty"`
		Actor string  `json:"actor,omitempty"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_body", err.Error())
		return
	}
	actor := body.Actor
	if actor == "" {
		actor = "ui"
	}

	if body.State != nil {
		st := incidents.State(*body.State)
		switch st {
		case incidents.StateFiring, incidents.StateAcknowledged, incidents.StateResolved:
		default:
			writeError(w, http.StatusBadRequest, "bad_state", "state must be firing, acknowledged or resolved")
			return
		}
		if err := s.Incidents.SetState(r.Context(), id, st, actor); err != nil {
			writeError(w, http.StatusInternalServerError, "update_failed", err.Error())
			return
		}
	}
	if body.Notes != nil {
		if err := s.Incidents.SetNotes(r.Context(), id, *body.Notes, actor); err != nil {
			writeError(w, http.StatusInternalServerError, "update_failed", err.Error())
			return
		}
	}

	a, err := s.Incidents.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no tracked alert with that id")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"alert": a})
}

// rulesPriority asks the cluster's rules how urgent this alert is, so the
// dashboard agrees with the list it was promoted from.
func rulesPriority(r *http.Request, s *Server, clusterID int, a monitoring.Alert) rules.Priority {
	cfg, err := s.Runs.Store.LoadRules(r.Context(), clusterID)
	if err != nil {
		return rules.DefaultPriority
	}
	return cfg.Decide(a).Priority
}

// openRunFor returns the id of an investigation already in flight for this
// alert, if there is one.
//
// Without this, hitting debug twice — or an auto-rule firing while somebody
// clicks — spends two Devtron calls and two models to produce the same answer
// twice. The second request is answered with the first run rather than
// refused, because the caller wanted to see an investigation and there is one.
func openRunFor(r *http.Request, s *Server, a *incidents.Alert) string {
	for _, ref := range a.Runs {
		if !runs.Terminal(ref.Status) {
			return ref.RunID
		}
	}
	return ""
}
