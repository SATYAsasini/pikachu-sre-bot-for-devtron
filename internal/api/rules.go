package api

import (
	"encoding/json"
	"net/http"

	"github.com/devtron-labs/devtron-sre-agent/internal/monitoring"
	"github.com/devtron-labs/devtron-sre-agent/internal/rules"
)

// getRules serves one cluster's rules.
func (s *Server) getRules(w http.ResponseWriter, r *http.Request) {
	id := intQuery(r.URL.Query().Get("clusterId"), 0)
	if id == 0 {
		writeError(w, http.StatusBadRequest, "missing_cluster_id", "clusterId is required")
		return
	}
	cfg, err := s.Runs.Store.LoadRules(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// putRules replaces one cluster's rules.
func (s *Server) putRules(w http.ResponseWriter, r *http.Request) {
	var body struct {
		rules.Config
		ClusterName string `json:"clusterName"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_body", err.Error())
		return
	}
	if body.ClusterID == 0 {
		writeError(w, http.StatusBadRequest, "missing_cluster_id", "clusterId is required")
		return
	}
	if err := s.Runs.Store.SaveRules(r.Context(), body.Config, body.ClusterName, "ui"); err != nil {
		writeError(w, http.StatusInternalServerError, "save_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, body.Config)
}

// previewRules runs a proposed rule set against the alerts firing right now.
//
// Writing a match against a payload you cannot see is guesswork, and a rule
// that silently hides the one alert that mattered is the worst outcome this
// feature has. So the editor asks the server what the rules *would* do, using
// the same evaluator that will run in production — not a re-implementation in
// the browser, which is how the two drift.
func (s *Server) previewRules(w http.ResponseWriter, r *http.Request) {
	var body struct {
		rules.Config
		ClusterName string `json:"clusterName"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_body", err.Error())
		return
	}
	if body.ClusterID == 0 {
		writeError(w, http.StatusBadRequest, "missing_cluster_id", "clusterId is required")
		return
	}

	stack, err := s.Discoverer.Get(r.Context(), body.ClusterID, body.ClusterName)
	if err != nil {
		writeDevtronError(w, err)
		return
	}
	all, alertErr := monitoring.New(s.Devtron, body.ClusterID, stack).
		Alerts(r.Context(), monitoring.AlertFilter{Limit: 200})
	if alertErr != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"rows":        []any{},
			"unavailable": alertErr.Error(),
		})
		return
	}

	// Every alert, shown or not, with the decision attached. The hidden ones
	// are the point: an operator needs to see what a rule is about to
	// suppress, not just what survives it.
	rows := make([]map[string]any, 0, len(all))
	var shown, muted, auto int
	counts := map[rules.Priority]int{}

	for _, a := range all {
		d := body.Config.Decide(a)
		if d.Show {
			shown++
			counts[d.Priority]++
		} else {
			muted++
		}
		if d.Auto {
			auto++
		}
		rows = append(rows, map[string]any{"alert": a, "decision": d})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"rows":  rows,
		"total": len(all),
		"shown": shown,
		"muted": muted,
		"auto":  auto,
		"byPriority": map[string]int{
			string(rules.P0): counts[rules.P0],
			string(rules.P1): counts[rules.P1],
			string(rules.P2): counts[rules.P2],
		},
	})
}
