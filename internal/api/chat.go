package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/devtron-labs/devtron-sre-agent/internal/devtron"
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
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, b)
		flusher.Flush()
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
