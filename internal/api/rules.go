package api

import (
	"encoding/json"
	"net/http"
	"strings"

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
	// The webhook URL is a credential: anyone holding it can post into the
	// channel. It goes out as "set, ending …a1b2", never as the value.
	cfg.Notify = cfg.Notify.Redacted()
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

	// The UI never receives the URL, so it cannot send it back. An empty URL
	// on save means "unchanged", not "cleared" — otherwise editing a priority
	// rule would silently disconnect the channel. Clearing is explicit.
	if strings.TrimSpace(body.Notify.URL) == "" && !body.Notify.ClearURL {
		if prev, err := s.Runs.Store.LoadRules(r.Context(), body.ClusterID); err == nil {
			body.Notify.URL = prev.Notify.URL
		}
	}
	body.Notify.URLSet, body.Notify.URLHint, body.Notify.ClearURL = false, "", false

	if err := s.Runs.Store.SaveRules(r.Context(), body.Config, body.ClusterName, "ui"); err != nil {
		writeError(w, http.StatusInternalServerError, "save_failed", err.Error())
		return
	}
	out := body.Config
	out.Notify = out.Notify.Redacted()
	writeJSON(w, http.StatusOK, out)
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

// testNotify sends one message to the configured channel.
//
// A webhook URL is pasted from somewhere else and is wrong surprisingly
// often — a trailing space, the wrong workspace, a revoked token. Finding
// that out during an incident, when the message that matters does not
// arrive, is the worst possible time.
func (s *Server) testNotify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ClusterID   int          `json:"clusterId"`
		ClusterName string       `json:"clusterName"`
		Notify      rules.Notify `json:"notify"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_body", err.Error())
		return
	}

	// The UI never holds the URL, so a test of the saved channel sends no URL
	// at all. Fall back to what is stored.
	n := body.Notify
	if strings.TrimSpace(n.URL) == "" && body.ClusterID != 0 {
		if prev, err := s.Runs.Store.LoadRules(r.Context(), body.ClusterID); err == nil {
			n.URL = prev.Notify.URL
			if n.Channel == "" {
				n.Channel = prev.Notify.Channel
			}
		}
	}
	// Enabled is irrelevant to a test: someone verifying a URL before
	// switching the channel on is exactly the case this exists for.
	n.Enabled = true

	name := body.ClusterName
	if name == "" {
		name = "this cluster"
	}
	err := rules.Send(r.Context(), n, rules.Message{
		Title:    "Pikachu SRE is connected",
		Body:     "This is a test from " + name + ". Findings for this cluster will arrive here.",
		Priority: rules.P2,
	}, nil)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
