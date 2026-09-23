package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/devtron-labs/devtron-sre-agent/internal/monitoring"
	"github.com/devtron-labs/devtron-sre-agent/internal/runs"
)

func (s *Server) listAlerts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	clusterID := intQuery(q.Get("clusterId"), 0)
	if clusterID == 0 {
		writeError(w, http.StatusBadRequest, "missing_cluster_id", "clusterId is required")
		return
	}
	stack, err := s.Discoverer.Get(r.Context(), clusterID, q.Get("clusterName"))
	if err != nil {
		writeDevtronError(w, err)
		return
	}
	mon := monitoring.New(s.Devtron, clusterID, stack)
	alerts, err := mon.Alerts(r.Context(), monitoring.AlertFilter{
		Namespace:      q.Get("namespace"),
		NameLike:       q.Get("nameLike"),
		Severity:       q.Get("severity"),
		IncludePending: q.Get("includePending") == "true",
		Limit:          intQuery(q.Get("limit"), 100),
	})
	if err != nil {
		// No alert source is a legitimate state of the world, not a server
		// fault. The UI must be able to say "coverage unknown" rather than
		// render a generic failure.
		writeJSONStatus(w, http.StatusOK, map[string]any{
			"alerts":      []monitoring.Alert{},
			"unavailable": err.Error(),
			"notes":       stack.Notes,
		})
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(alerts))
}

func (s *Server) createRun(w http.ResponseWriter, r *http.Request) {
	var req runs.CreateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_body", err.Error())
		return
	}
	if req.ClusterID <= 0 {
		writeError(w, http.StatusBadRequest, "missing_cluster", "clusterId is required")
		return
	}
	if len(req.Alert) == 0 && strings.TrimSpace(req.Ask) == "" {
		writeError(w, http.StatusBadRequest, "missing_trigger",
			"either an alert or a free-text ask is required")
		return
	}
	if req.ClusterName == "" {
		if c, err := s.clusterName(r, req.ClusterID); err == nil {
			req.ClusterName = c
		}
	}

	// Check what this run will actually be able to read BEFORE queueing it.
	// Discovering mid-run that there is no metrics backend means the first
	// pass, the judge and the deep dive have all already been paid for by the
	// time anyone learns the answer was never obtainable.
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
	writeJSON(w, http.StatusCreated, run)
}

// preflightBlock returns why a run cannot usefully proceed, or "".
//
// It deliberately does not require metrics: plenty of workload questions are
// answerable from Kubernetes state alone. What it refuses is the case where
// nothing at all can be read, because that run can only ever conclude that it
// could not look.
func (s *Server) preflightBlock(r *http.Request, req runs.CreateRequest) string {
	if req.Force {
		return ""
	}
	ctx := r.Context()

	if measured := s.Caps.Get(req.ClusterID); measured != nil && !measured.Reach.Investigable() {
		return "Cluster " + req.ClusterName + " cannot be read. " + measured.Reach.Why() +
			" Nothing useful can be concluded from it, so this run was not started."
	}

	stack, err := s.Discoverer.Get(ctx, req.ClusterID, req.ClusterName)
	if err != nil {
		return "Could not work out what " + req.ClusterName + " exposes: " + err.Error()
	}

	// An alert-triggered run already has its alert, so a missing alert source
	// only matters for the wider-incident check, not for the investigation.
	metrics, alerts := stack.HasMetrics(), stack.HasAlerts()
	if !metrics && !alerts {
		why := "No metrics backend and no alert source were found in " + req.ClusterName + "."
		if len(stack.Notes) > 0 {
			why += " " + strings.Join(stack.Notes, " ")
		}
		return why + " The agent would have nothing to measure against, so this run was not started. " +
			"Deploy or expose Prometheus, VictoriaMetrics, Alertmanager or vmalert, or re-send with force to " +
			"investigate from Kubernetes state alone."
	}
	return ""
}

func (s *Server) clusterName(r *http.Request, id int) (string, error) {
	cs, err := s.Devtron.Clusters(r.Context())
	if err != nil {
		return "", err
	}
	for _, c := range cs {
		if c.ID == id {
			return c.ClusterName, nil
		}
	}
	return "", errors.New("cluster not found")
}

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, err := s.Runs.Store.List(r.Context(), runs.ListFilter{
		Status:    q.Get("status"),
		ClusterID: intQuery(q.Get("clusterId"), 0),
		Limit:     intQuery(q.Get("limit"), 50),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(list))
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.Runs.Store.Get(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, runs.ErrNotFound) {
		writeError(w, http.StatusNotFound, "run_not_found", "no run with that id")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) runEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	evs, err := s.Runs.Store.Events(r.Context(), chi.URLParam(r, "id"),
		intQuery(q.Get("after"), 0), intQuery(q.Get("limit"), 500))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "events_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(evs))
}

func (s *Server) cancelRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s.Worker.Cancel(id)
	if err := s.Runs.Store.Cancel(r.Context(), id); err != nil {
		writeError(w, http.StatusConflict, "not_cancelable", err.Error())
		return
	}
	run, err := s.Runs.Store.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// streamRun serves the live ledger over SSE.
//
// Ordering matters here: the broker subscription is taken BEFORE the database
// replay, so an event written between the two is delivered rather than lost.
// The seen-sequence check then drops the duplicate that ordering creates.
func (s *Server) streamRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	run, err := s.Runs.Store.Get(r.Context(), id)
	if errors.Is(err, runs.ErrNotFound) {
		writeError(w, http.StatusNotFound, "run_not_found", "no run with that id")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get_failed", err.Error())
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "no_streaming", "this server cannot stream")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // defeat proxy buffering
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	live, unsubscribe := s.Runs.Broker.Subscribe(id)
	defer unsubscribe()

	after := intQuery(r.URL.Query().Get("after"), 0)
	highest := after
	replay, err := s.Runs.Store.Events(r.Context(), id, after, 1000)
	if err != nil {
		s.Log.Warn("sse replay failed", "run", id, "err", err)
	}
	for _, e := range replay {
		writeSSE(w, flusher, e.Type, e)
		highest = e.Seq
	}

	// A run that finished before the client connected gets its history and a
	// clean close rather than an open socket that never speaks again.
	if runs.Terminal(run.Status) {
		writeSSEDone(w, flusher, run.Status)
		return
	}

	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()
	ctx := r.Context()

	for {
		select {
		case <-ctx.Done():
			return
		case <-keepalive.C:
			_, _ = fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case e, open := <-live:
			if !open {
				writeSSEDone(w, flusher, "")
				return
			}
			if e.Seq <= highest {
				continue // already sent during replay
			}
			highest = e.Seq
			writeSSE(w, flusher, e.Type, e)
			if e.Type == runs.EvStatus && terminalStatusIn(e.Payload) != "" {
				writeSSEDone(w, flusher, terminalStatusIn(e.Payload))
				return
			}
		}
	}
}

func terminalStatusIn(payload json.RawMessage) string {
	var p struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(payload, &p) != nil {
		return ""
	}
	if runs.Terminal(p.Status) {
		return p.Status
	}
	return ""
}

func writeSSE(w http.ResponseWriter, f http.Flusher, event string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
	f.Flush()
}

func writeSSEDone(w http.ResponseWriter, f http.Flusher, status string) {
	payload := map[string]string{}
	if status != "" {
		payload["status"] = status
	}
	writeSSE(w, f, "done", payload)
}

func writeJSONStatus(w http.ResponseWriter, status int, v any) { writeJSON(w, status, v) }
