package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/devtron-labs/devtron-sre-agent/internal/devtron"
)

// settingsView is what the settings screen sees. The token is deliberately
// absent: an operator needs to know whether one is configured and which one,
// never to read it back out.
type settingsView struct {
	DevtronURL string     `json:"devtronUrl"`
	TokenSet   bool       `json:"tokenSet"`
	TokenHint  string     `json:"tokenHint,omitempty"`
	Source     string     `json:"source"` // database | environment
	UpdatedAt  *time.Time `json:"updatedAt,omitempty"`
	UpdatedBy  string     `json:"updatedBy,omitempty"`
	// IntelligencePath is fixed at deploy time; shown so the screen can say
	// where the first-pass debugger is expected to live.
	IntelligencePath string `json:"intelligencePath"`
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	view := settingsView{
		DevtronURL:       s.Devtron.BaseURL(),
		TokenSet:         s.Devtron.HasToken(),
		TokenHint:        s.Devtron.TokenHint(),
		Source:           "environment",
		IntelligencePath: s.Cfg.Devtron.IntelligencePath,
	}
	if stored, ok, err := s.Runs.Store.LoadSettings(r.Context()); err == nil && ok {
		view.Source = "database"
		view.UpdatedAt = &stored.UpdatedAt
		view.UpdatedBy = stored.UpdatedBy
	}
	writeJSON(w, http.StatusOK, view)
}

type settingsBody struct {
	DevtronURL string `json:"devtronUrl"`
	// Empty means "keep the stored token", so the host can be changed without
	// re-pasting the credential.
	DevtronToken string `json:"devtronToken"`
}

// testSettings checks a candidate host and token without saving anything.
// Listing clusters is the right probe: it is read-only, it is the first call
// every run makes, and it fails distinctly on a bad token versus a bad host.
func (s *Server) testSettings(w http.ResponseWriter, r *http.Request) {
	var body settingsBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_body", err.Error())
		return
	}
	url, token := strings.TrimSpace(body.DevtronURL), strings.TrimSpace(body.DevtronToken)
	if url == "" {
		url = s.Devtron.BaseURL()
	}
	if token == "" {
		if stored, ok, err := s.Runs.Store.LoadSettings(r.Context()); err == nil && ok {
			token = stored.Token
		} else {
			token = s.Cfg.Devtron.Token
		}
	}
	writeJSON(w, http.StatusOK, probeDevtron(r.Context(), s.Cfg.Devtron.IntelligencePath, url, token, s.Cfg.Devtron.InsecureTLS))
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var body settingsBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_body", err.Error())
		return
	}
	saved, err := s.Runs.Store.SaveSettings(r.Context(), body.DevtronURL, body.DevtronToken, actorOf(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_settings", err.Error())
		return
	}

	// Point the live client at the new installation and drop everything that
	// was derived from the old one. A cached monitoring stack from a previous
	// Devtron would otherwise be served as though it were current.
	s.Devtron.Reconfigure(saved.DevtronURL, saved.Token)
	s.Discoverer.InvalidateAll()
	// Capabilities measured against the previous installation say nothing
	// about this one.
	s.Caps.Reset(r.Context())
	s.Caps.RefreshInBackground(r.Context())
	s.Log.Info("devtron settings changed", "url", saved.DevtronURL, "by", saved.UpdatedBy)

	s.getSettings(w, r)
}

// ProbeResult is the outcome of a connection test.
type ProbeResult struct {
	OK       bool   `json:"ok"`
	Message  string `json:"message"`
	Clusters int    `json:"clusters"`
	// Names are the first few clusters found, which is the clearest possible
	// proof the credential works and points where the operator expects.
	Names []string `json:"names,omitempty"`
}

func probeDevtron(ctx context.Context, intelligencePath, url, token string, insecure bool) ProbeResult {
	if url == "" {
		return ProbeResult{Message: "No Devtron URL is set."}
	}
	if token == "" {
		return ProbeResult{Message: "No API token is set."}
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	probe := devtron.New(devtron.Options{
		BaseURL: url, Token: token, IntelligencePath: intelligencePath,
		Timeout: 12 * time.Second, InsecureTLS: insecure,
	})
	clusters, err := probe.Clusters(ctx)
	if err != nil {
		var de *devtron.Error
		switch {
		case asDevtron(err, &de) && de.Unauthorized():
			return ProbeResult{Message: "Devtron rejected the token. Check that it is valid and has View access."}
		case asDevtron(err, &de):
			return ProbeResult{Message: "Devtron answered with HTTP " + itoa(de.Status) + ". The host is reachable but the API did not respond as expected."}
		default:
			return ProbeResult{Message: "Could not reach " + url + ": " + err.Error()}
		}
	}
	names := make([]string, 0, 5)
	for i, c := range clusters {
		if i >= 5 {
			break
		}
		names = append(names, c.ClusterName)
	}
	if len(clusters) == 0 {
		return ProbeResult{
			OK: true, Message: "Connected, but this token can see no clusters. Check its Devtron RBAC.",
		}
	}
	return ProbeResult{
		OK: true, Clusters: len(clusters), Names: names,
		Message: "Connected. " + itoa(len(clusters)) + " cluster(s) visible to this token.",
	}
}

func actorOf(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-User"); v != "" {
		return v
	}
	return "ui"
}
