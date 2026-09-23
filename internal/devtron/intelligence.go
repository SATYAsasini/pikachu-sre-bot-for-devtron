package devtron

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// IntelligenceContext is the only structured information the Athena agent
// reads. Devtron drops every other key before the prompt is built, so alert
// name, severity, labels and summary must go into Ask as prose — putting
// them here means the model never sees them.
type IntelligenceContext struct {
	ClusterID       int    `json:"clusterId,omitempty"`
	ClusterName     string `json:"clusterName,omitempty"`
	Namespace       string `json:"namespace,omitempty"`
	AppID           int    `json:"appId,omitempty"`
	AppName         string `json:"appName,omitempty"`
	AppType         string `json:"appType,omitempty"`
	EnvironmentID   int    `json:"environmentId,omitempty"`
	EnvironmentName string `json:"environmentName,omitempty"`
	ResourceKind    string `json:"resourceKind,omitempty"`
	ResourceName    string `json:"resourceName,omitempty"`
	ResourceStatus  string `json:"resourceStatus,omitempty"`
}

// IntelligenceRequest is one stateless debugging question.
type IntelligenceRequest struct {
	Ask     string              `json:"ask"`
	Context IntelligenceContext `json:"context"`
}

// Event types on the intelligence stream.
const (
	EventThinking = "thinking"
	EventAnalysis = "analysis"
	EventError    = "error"
)

// IntelligenceEvent is one decoded server-sent event.
type IntelligenceEvent struct {
	Type      string `json:"type"`
	Content   string `json:"content,omitempty"`
	Analysis  string `json:"analysis,omitempty"`
	Error     string `json:"error,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// IntelligenceResult is the outcome of one call.
type IntelligenceResult struct {
	// Analysis is the markdown answer. Empty when the run ended in error.
	Analysis string
	// Thinking is the trail of progress lines, useful in the UI and as a
	// record of which tools Devtron's own agent reached for.
	Thinking []string
	// RequestID is Devtron's oneshot-<hex> id. Always log it: it is the only
	// way to trace the call on the Devtron side.
	RequestID string
	// Failed is the error text when the final event was an error.
	Failed string
	// Duration is how long the stream took.
	Duration time.Duration
}

// OK reports that an analysis came back.
func (r *IntelligenceResult) OK() bool { return r != nil && r.Analysis != "" }

// ErrIntelligenceNoAnswer means the stream closed without a final event.
var ErrIntelligenceNoAnswer = errors.New("intelligence stream ended without an analysis or error event")

// Intelligence asks Devtron's one-shot debugger to investigate, streaming
// progress to onEvent as it arrives. It is the first pass of every run: the
// agent's own job starts from this answer rather than from a blank cluster.
//
// Auth here is a cookie, not a bearer header. Devtron reads argocd.token.
func (c *Client) Intelligence(ctx context.Context, req IntelligenceRequest, onEvent func(IntelligenceEvent)) (*IntelligenceResult, error) {
	if strings.TrimSpace(req.Ask) == "" {
		return nil, fmt.Errorf("ask is required")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	base, token := c.creds()
	url := base + c.intelligencePath
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	// Athena authenticates from the cookie, not the Authorization header.
	httpReq.AddCookie(&http.Cookie{Name: "argocd.token", Value: token})

	started := time.Now()
	resp, err := c.stream.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("intelligence: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		return nil, &Error{Status: resp.StatusCode, Path: c.intelligencePath, Body: Truncate(buf.String(), 400)}
	}

	out := &IntelligenceResult{}
	sc := bufio.NewScanner(resp.Body)
	// Analysis markdown can be large; the default 64K token limit is not enough.
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var ev IntelligenceEvent
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			continue // a keep-alive or a shape we do not model
		}
		if ev.RequestID != "" {
			out.RequestID = ev.RequestID
		}
		if onEvent != nil {
			onEvent(ev)
		}
		switch ev.Type {
		case EventThinking:
			if ev.Content != "" {
				out.Thinking = append(out.Thinking, ev.Content)
			}
		case EventAnalysis:
			out.Analysis = ev.Analysis
			out.Duration = time.Since(started)
			return out, nil
		case EventError:
			out.Failed = ev.Error
			out.Duration = time.Since(started)
			return out, nil
		}
	}
	out.Duration = time.Since(started)
	if err := sc.Err(); err != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		return out, fmt.Errorf("intelligence stream: %w", err)
	}
	return out, ErrIntelligenceNoAnswer
}
