package devtron

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ProxyScope selects which prefix the Kubernetes proxy uses.
type ProxyScope string

const (
	// ScopeCluster addresses a cluster directly: /proxy/cluster/{id}/...
	ScopeCluster ProxyScope = "cluster"
	// ScopeEnv addresses it through an environment: /proxy/env/{id}/...
	// The namespace still has to appear in the path; the environment only
	// decides which cluster and which RBAC check apply.
	ScopeEnv ProxyScope = "env"
)

// ServiceRef names an in-cluster Service to talk to through the proxy.
type ServiceRef struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	// Scheme is "http" or "https"; empty means http. Devtron's service proxy
	// encodes it as "https:<name>:<port>" when it is not plain http.
	Scheme string `json:"scheme,omitempty"`
	// Port is left empty on purpose unless the token has "*" on resource
	// names: naming a port turns the proxy target into "<name>:<port>", which
	// a narrowly scoped token is not allowed to reach.
	Port string `json:"port,omitempty"`
}

func (s ServiceRef) target() string {
	switch {
	case s.Scheme == "https" && s.Port != "":
		return "https:" + s.Name + ":" + s.Port
	case s.Scheme == "https":
		return "https:" + s.Name
	case s.Port != "":
		return s.Name + ":" + s.Port
	default:
		return s.Name
	}
}

// ProxyGet performs a GET through the orchestrator's Kubernetes proxy and
// returns the raw body. Devtron does not wrap proxy responses in its usual
// {code, result} envelope, so what comes back is exactly what the target
// served.
//
// GET only: every other method on this path requires Admin in Devtron, and a
// view-only token must not have it.
func (c *Client) ProxyGet(ctx context.Context, scope ProxyScope, id int, k8sPath string, query url.Values) ([]byte, error) {
	if id == 0 {
		return nil, fmt.Errorf("a %s id is required for the proxy", scope)
	}
	base, token := c.creds()
	u := fmt.Sprintf("%s/orchestrator/k8s/proxy/%s/%d/%s",
		base, scope, id, strings.TrimLeft(k8sPath, "/"))
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	// The proxy is the one path that wants a bearer token.
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &Error{
			Status: resp.StatusCode,
			Path:   "/orchestrator/k8s/proxy/" + string(scope) + "/" + strconv.Itoa(id) + "/" + k8sPath,
			Body:   Truncate(string(body), 400),
		}
	}
	return body, nil
}

// ServiceProxyGet reaches an in-cluster Service's own HTTP API through the
// Kubernetes service proxy. This is how the agent queries Prometheus,
// VictoriaMetrics, Alertmanager and vmalert without any network path of its
// own into the cluster.
//
// The resulting k8s path is
//
//	api/v1/namespaces/<ns>/services/<target>/proxy/<path>
func (c *Client) ServiceProxyGet(ctx context.Context, scope ProxyScope, id int, svc ServiceRef, path string, query url.Values) ([]byte, error) {
	if svc.Namespace == "" || svc.Name == "" {
		return nil, fmt.Errorf("a service namespace and name are required")
	}
	k8sPath := fmt.Sprintf("api/v1/namespaces/%s/services/%s/proxy/%s",
		svc.Namespace, svc.target(), strings.TrimLeft(path, "/"))
	return c.ProxyGet(ctx, scope, id, k8sPath, query)
}

// ProxyGetTimeout is ServiceProxyGet with its own deadline, for metric range
// queries that legitimately take longer than a catalog lookup.
func (c *Client) ProxyGetTimeout(ctx context.Context, scope ProxyScope, id int, svc ServiceRef, path string, query url.Values, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return c.ServiceProxyGet(ctx, scope, id, svc, path, query)
}
