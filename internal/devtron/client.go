// Package devtron is the single access path to every cluster the agent can
// see. Nothing here uses a kubeconfig: cluster reads go through the Devtron
// orchestrator's Kubernetes proxy, and Devtron's own RBAC decides what the
// token may touch.
//
// One token, three placements — getting this wrong is the usual cause of a
// 401, so it is encoded in the client rather than left to callers:
//
//	token: <t>                  every /orchestrator/* REST call
//	Authorization: Bearer <t>   /orchestrator/k8s/proxy/* only
//	Cookie: argocd.token=<t>    /proxy/athena/intelligence only
//
// Every request is a read. The proxy is GET-only by Devtron's own rule: any
// other method there requires Admin, which a view-only token must not have.
package devtron

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Options configures a Client.
type Options struct {
	// BaseURL is the Devtron host, e.g. https://devtron.example.com.
	BaseURL string
	// Token is a view-only Devtron API token.
	Token string
	// IntelligencePath defaults to /proxy/athena/intelligence.
	IntelligencePath string
	// Timeout bounds a normal REST call. Streaming calls set their own.
	Timeout time.Duration
	// InsecureTLS skips certificate verification. Test installs only.
	InsecureTLS bool
}

// Client talks to one Devtron installation.
//
// The host and token are guarded because they are reconfigurable from the
// settings screen while investigations are in flight. Everything else is
// fixed at construction.
type Client struct {
	mu               sync.RWMutex
	base             string
	token            string
	intelligencePath string
	http             *http.Client
	// stream has no timeout; /intelligence runs for minutes.
	stream *http.Client
}

// creds reads the host and token under the lock.
func (c *Client) creds() (base, token string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.base, c.token
}

// Reconfigure points the client at a different Devtron installation. Calls
// already in flight keep the credentials they started with.
func (c *Client) Reconfigure(baseURL, token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if baseURL != "" {
		c.base = strings.TrimRight(baseURL, "/")
	}
	if token != "" {
		c.token = token
	}
}

// TokenHint is the last four characters of the token, for showing an
// operator which credential is loaded without revealing it.
func (c *Client) TokenHint() string {
	_, token := c.creds()
	if len(token) < 4 {
		return ""
	}
	return "…" + token[len(token)-4:]
}

// HasToken reports whether any credential is configured.
func (c *Client) HasToken() bool {
	_, token := c.creds()
	return token != ""
}

// New builds a client.
func New(o Options) *Client {
	if o.Timeout <= 0 {
		o.Timeout = 30 * time.Second
	}
	if o.IntelligencePath == "" {
		o.IntelligencePath = "/proxy/athena/intelligence"
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if o.InsecureTLS {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in for test installs
	}
	return &Client{
		base:             strings.TrimRight(o.BaseURL, "/"),
		token:            o.Token,
		intelligencePath: o.IntelligencePath,
		http:             &http.Client{Timeout: o.Timeout, Transport: tr},
		stream:           &http.Client{Transport: tr},
	}
}

// BaseURL returns the configured Devtron host.
func (c *Client) BaseURL() string {
	base, _ := c.creds()
	return base
}

// Error is a Devtron API failure.
type Error struct {
	Status int
	Path   string
	Body   string
}

func (e *Error) Error() string {
	return fmt.Sprintf("devtron %s: HTTP %d: %s", e.Path, e.Status, e.Body)
}

// Reason is the short human form: what went wrong, not where.
//
// Error() leads with the request path, and for a Kubernetes service proxy
// call that is ninety characters of namespace and service name before the
// first word that explains anything. Anywhere the text is clamped to a line —
// which is every list row that shows it — the reader gets the path and none
// of the reason. A probe records this instead, and keeps the full error for
// the tooltip.
func (e *Error) Reason() string {
	switch {
	case e.Unauthorized():
		// The proxy builds its RBAC object from the URL segment verbatim, so
		// "prom", "prom:9090" and "https:prom:9090" are three different
		// resource names. Only a grant of "*" matches the ones with a port.
		return fmt.Sprintf("HTTP %d — Devtron refused this read. The token needs Kubernetes Resources → View on this cluster, and a target naming a port (name:port, https:name:port) matches only a grant of \"All resources\".", e.Status)
	case e.Status == http.StatusNotFound:
		return "HTTP 404 — no such Service in that namespace, or the proxy rejected the path."
	case e.Status == http.StatusServiceUnavailable:
		// The orchestrator's proxy handler only ever writes JSON Kubernetes
		// Status objects, and an RBAC refusal from it is a 403, never a 503.
		// So an HTML body did not come from Devtron at all — something at the
		// edge replaced it — and a JSON one came from the cluster's own API
		// server, which returns 503 when its service proxy cannot reach the
		// pods behind a Service.
		if looksLikeHTML(e.Body) {
			return "HTTP 503 with an HTML body — this did not come from Devtron, whose proxy only ever answers JSON. Something in front of it replaced the response, so the real cause is hidden. Ask whoever runs the ingress to stop rewriting 503s on the Kubernetes proxy path."
		}
		return "HTTP 503 from the cluster's API server — its service proxy could not reach the pods behind this Service. Either the Service has no ready endpoints, or the control plane has no route to them: " + Truncate(oneLine(e.Body), 120)
	case e.Status >= 500:
		return fmt.Sprintf("HTTP %d — the orchestrator failed while serving this.", e.Status)
	}
	return fmt.Sprintf("HTTP %d: %s", e.Status, Truncate(e.Body, 160))
}

// looksLikeHTML reports whether a body is a rendered error page rather than
// anything a Kubernetes API answered.
//
// Both Devtron's proxy handler and a cluster's API server answer with JSON
// Status objects. An HTML body therefore proves the response was replaced in
// transit, which is worth saying plainly: the status code is real and the
// reason behind it has been thrown away.
func looksLikeHTML(body string) bool {
	b := strings.ToLower(body)
	return strings.Contains(b, "<!doctype html") || strings.Contains(b, "<html")
}

// oneLine flattens a body so it can sit on a single row.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Unauthorized reports a token problem rather than a missing object.
func (e *Error) Unauthorized() bool {
	return e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden
}

// envelope is the {code, status, result, errors} wrapper Devtron puts around
// every /orchestrator response. The proxy is the one exception.
type envelope struct {
	Code   int             `json:"code"`
	Status string          `json:"status"`
	Result json.RawMessage `json:"result"`
	Errors []struct {
		Code            string `json:"code"`
		InternalMessage string `json:"internalMessage"`
		UserMessage     string `json:"userMessage"`
	} `json:"errors"`
}

// get issues a GET against /orchestrator and unwraps the envelope into out.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	return c.do(ctx, http.MethodGet, path, query, nil, out)
}

// post issues a POST against /orchestrator and unwraps the envelope into out.
// These are read APIs that take a filter body, not writes.
func (c *Client) post(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPost, path, nil, body, out)
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	base, token := c.creds()
	u := base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode body for %s: %w", path, err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("token", token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return &Error{Status: resp.StatusCode, Path: path, Body: Truncate(string(raw), 400)}
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		// A few endpoints answer with a bare document.
		if out == nil {
			return nil
		}
		return json.Unmarshal(raw, out)
	}
	if len(env.Errors) > 0 {
		msg := strings.TrimSpace(env.Errors[0].UserMessage + " " + env.Errors[0].InternalMessage)
		return &Error{Status: resp.StatusCode, Path: path, Body: msg}
	}
	if out == nil || len(env.Result) == 0 {
		return nil
	}
	return json.Unmarshal(env.Result, out)
}

// Truncate shortens s for error messages and summaries.
func Truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
