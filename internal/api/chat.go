package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/devtron-labs/devtron-sre-agent/internal/agents"
	"github.com/devtron-labs/devtron-sre-agent/internal/devtron"
	"github.com/devtron-labs/devtron-sre-agent/internal/runs"
)

// chatRequest is one turn of a conversation the browser is holding.
//
// The history arrives with every request because the server keeps none: a
// chat is a question someone asked once, not a record of an investigation.
// Anything worth keeping becomes a run, which is a different thing with a
// different lifecycle.
type chatRequest struct {
	ClusterID   int    `json:"clusterId"`
	ClusterName string `json:"clusterName"`
	Namespace   string `json:"namespace,omitempty"`
	AppName     string `json:"appName,omitempty"`
	Ask         string `json:"ask"`
	// History is prior turns, oldest first. Only the text is carried; roles
	// are "user" or "agent".
	History []chatTurn `json:"history,omitempty"`
}

type chatTurn struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// chat answers a free-text question as a stream, and records nothing.
//
// This is deliberately not a run. A run is an auditable investigation: it has
// an id, a ledger, a verdict, a place in history and a row someone may read
// six months later. A question typed into a box is none of those, and minting
// a run for one filled the history with rows like "rkwn" that nobody wanted
// and could not delete.
//
// So this endpoint is stateless. The browser holds the thread, the server
// holds nothing, and closing the panel ends the conversation in the only way
// that is actually true: the data was only ever in one place, and it is gone.
func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if strings.TrimSpace(req.Ask) == "" {
		writeError(w, http.StatusBadRequest, "ask_required", "ask something first")
		return
	}
	if req.ClusterID == 0 {
		writeError(w, http.StatusBadRequest, "cluster_required", "a chat needs a cluster to look at")
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

	send := func(kind string, payload any) {
		b, err := json.Marshal(payload)
		if err != nil {
			return
		}
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, b)
		flusher.Flush()
	}

	// Not everything is a cluster question, and sending everything to a
	// cluster-debugging agent made it answer badly about itself and about its
	// own history. Route first; /intelligence is one destination of four.
	switch classify(req.Ask) {
	case intentPlatform:
		send("answer", map[string]any{
			"text":   platformAnswer(req.Ask, s.Cfg.Models.Fast, s.Cfg.Models.Strong, s.Cfg.Run.MaxToolCalls, len(agents.SREToolNames())),
			"source": "platform",
		})
		send("done", map[string]bool{"done": true})
		return

	case intentRuns:
		text, err := s.runsAnswer(r.Context(), req)
		if err != nil {
			send("error", map[string]string{"error": err.Error()})
		} else {
			send("answer", map[string]any{"text": text, "source": "runs"})
		}
		send("done", map[string]bool{"done": true})
		return

	case intentPropose:
		// A prepared run rather than a started one. Spending a Devtron call
		// and two models on someone's behalf is not a thing to do without
		// being asked twice.
		send("answer", map[string]any{
			"text": "That is worth a proper investigation rather than a chat answer — a run gets the fact pack, " +
				"the judge and the deep dive, and leaves a record you can come back to.\n\n" +
				"I have prepared one. Check the parameters and trigger it when you are ready.",
			"source": "propose",
		})
		send("proposal", map[string]any{
			"clusterId":   req.ClusterID,
			"clusterName": req.ClusterName,
			"namespace":   req.Namespace,
			"appName":     req.AppName,
			"ask":         req.Ask,
			"options":     map[string]any{"depth": "auto", "metrics": "auto", "logs": "auto"},
		})
		send("done", map[string]bool{"done": true})
		return
	}

	res, err := s.Devtron.Intelligence(r.Context(), devtron.IntelligenceRequest{
		Ask: askWithHistory(req),
		Context: devtron.IntelligenceContext{
			ClusterID:   req.ClusterID,
			ClusterName: req.ClusterName,
			Namespace:   req.Namespace,
			AppName:     req.AppName,
		},
	}, func(ev devtron.IntelligenceEvent) {
		if ev.Type == devtron.EventThinking && ev.Content != "" {
			send("thinking", map[string]string{"text": ev.Content})
		}
	})

	switch {
	case err != nil:
		send("error", map[string]string{"error": err.Error()})
	case res != nil && res.Failed != "":
		send("error", map[string]string{"error": res.Failed})
	case res != nil:
		send("answer", map[string]any{
			"text":      res.Analysis,
			"requestId": res.RequestID,
			"steps":     len(res.Thinking),
			"ms":        res.Duration.Milliseconds(),
		})
	}
	send("done", map[string]bool{"done": true})
}

// askWithHistory folds prior turns into the prose question.
//
// /intelligence is stateless and takes one string, so a conversation has to be
// re-stated on every turn. Only the last few turns are carried: the endpoint
// has its own budget, and a transcript of twenty exchanges crowds out the
// question actually being asked.
func askWithHistory(req chatRequest) string {
	const keep = 6

	hist := req.History
	if len(hist) > keep {
		hist = hist[len(hist)-keep:]
	}
	if len(hist) == 0 {
		return req.Ask
	}

	var b strings.Builder
	b.WriteString("Earlier in this conversation:\n")
	for _, t := range hist {
		who := "They asked"
		if t.Role != "user" {
			who = "You answered"
		}
		b.WriteString("- ")
		b.WriteString(who)
		b.WriteString(": ")
		b.WriteString(clip(t.Text, 400))
		b.WriteString("\n")
	}
	b.WriteString("\nNow: ")
	b.WriteString(req.Ask)
	return b.String()
}

func clip(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// runsAnswer reports on what this agent has already done.
//
// Composed here rather than asked of a model: the runs are in the database two
// function calls away, and a model summarising rows it was handed is a slower,
// less reliable way to read a table.
func (s *Server) runsAnswer(ctx context.Context, req chatRequest) (string, error) {
	list, err := s.Runs.Store.List(ctx, runs.ListFilter{ClusterID: req.ClusterID, Limit: 20})
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "No runs yet on " + orDash(req.ClusterName) + ". Trigger one from an alert and it will show up here.", nil
	}

	var done, failed int
	for _, r := range list {
		switch r.Status {
		case runs.StatusSucceeded:
			done++
		case runs.StatusFailed, runs.StatusBudgetExceeded:
			failed++
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "**%d recent run(s)** on %s — %d succeeded, %d failed.\n\n",
		len(list), orDash(req.ClusterName), done, failed)

	for i, r := range list {
		if i == 5 {
			fmt.Fprintf(&b, "\n…and %d more in History.", len(list)-5)
			break
		}
		fmt.Fprintf(&b, "- **%s** — %s", runTitle(r), r.Status)
		if r.DurationMs > 0 {
			fmt.Fprintf(&b, ", %ds", r.DurationMs/1000)
		}
		if line := conclusionLine(r); line != "" {
			fmt.Fprintf(&b, "\n  %s", line)
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

// runTitle is what the run was about, in a few words.
func runTitle(r runs.Run) string {
	var a struct {
		Name     string `json:"name"`
		Resource string `json:"resource"`
	}
	if len(r.Trigger.Alert) > 0 {
		_ = json.Unmarshal(r.Trigger.Alert, &a)
	}
	if a.Name != "" {
		if a.Resource != "" {
			return a.Name + " on " + a.Resource
		}
		return a.Name
	}
	if r.Trigger.Ask != "" {
		return clip(r.Trigger.Ask, 60)
	}
	return "run " + clip(r.ID, 8)
}

// conclusionLine is the one sentence a finished run is worth quoting for.
func conclusionLine(r runs.Run) string {
	if len(r.Report) == 0 {
		return ""
	}
	var rep struct {
		CorrectedRootCause string `json:"correctedRootCause"`
		SreNotes           string `json:"sreNotes"`
	}
	if json.Unmarshal(r.Report, &rep) != nil {
		return ""
	}
	if c := strings.TrimSpace(rep.CorrectedRootCause); c != "" {
		return clip(c, 180)
	}
	return clip(strings.TrimSpace(rep.SreNotes), 180)
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "this cluster"
	}
	return s
}
