// Package tools defines the contract every investigation tool follows: a
// typed, read-only function that returns a bounded Result envelope. Tool
// packages (k8s, logs, prom, ...) register Tools; the agent runtime binds them
// to a run's sandbox and exposes them to ADK, and the pre-flight collector
// calls them directly.
//
// Envelope fields follow the design in docs/architecture.md §7.3 and the
// prior-art notes (kstack response schema, kagent tool errors).
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"google.golang.org/adk/agent"
	adktool "google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"

	"github.com/devtron-labs/devtron-sre-agent/internal/devtron"
	"github.com/devtron-labs/devtron-sre-agent/internal/knowledge"
	"github.com/devtron-labs/devtron-sre-agent/internal/monitoring"
)

// ErrorKind classifies a tool failure so the planner reacts correctly.
type ErrorKind string

const (
	// ErrInput: the caller's arguments were wrong. Never retry as-is.
	ErrInput ErrorKind = "input"
	// ErrCluster: the target cluster refused or could not serve (RBAC, not
	// found, timeout). Retry only if Retryable.
	ErrCluster ErrorKind = "cluster"
	// ErrPlatform: our own side failed (backend missing, bug). Do not retry
	// against the cluster in a loop.
	ErrPlatform ErrorKind = "platform"
)

// Error is a structured tool failure with recovery hints.
type Error struct {
	Kind        ErrorKind `json:"kind"`
	Code        string    `json:"code"`
	Message     string    `json:"message"`
	Retryable   bool      `json:"retryable"`
	Suggestions []string  `json:"suggestions,omitempty"`
}

func (e *Error) Error() string { return fmt.Sprintf("%s/%s: %s", e.Kind, e.Code, e.Message) }

// Freshness says when and from where data was obtained.
type Freshness struct {
	FetchedAt time.Time `json:"fetchedAt"`
	Source    string    `json:"source"`             // devtron | prometheus | alertmanager | knowledge | cache
	CacheAge  string    `json:"cacheAge,omitempty"` // human form, only when served from cache
}

// Result is the envelope every tool returns. Data is bounded; anything larger
// is stored as an artifact and referenced.
type Result struct {
	// Summary is a one-line header for the UI timeline and the model.
	Summary string `json:"summary"`
	// Data is the structured, bounded payload.
	Data any `json:"data,omitempty"`
	// Truncated says Data was cut; ArtifactRef points at the full content.
	Truncated   bool       `json:"truncated,omitempty"`
	ArtifactRef string     `json:"artifactRef,omitempty"`
	Freshness   *Freshness `json:"freshness,omitempty"`
	// AgentContext is machine-readable side information (resolved GVK, owner
	// UIDs, ids) the model may use but must never quote to the user.
	AgentContext map[string]any `json:"agentContext,omitempty"`
	Error        *Error         `json:"error,omitempty"`
}

// Fail builds an error Result.
func Fail(kind ErrorKind, code, msg string, retryable bool, suggestions ...string) *Result {
	return &Result{Summary: "error: " + msg, Error: &Error{Kind: kind, Code: code, Message: msg, Retryable: retryable, Suggestions: suggestions}}
}

// Now is the freshness stamp for live data.
func Now(source string) *Freshness { return &Freshness{FetchedAt: time.Now(), Source: source} }

// Deps is what a tool gets at call time: the run's sandbox and shared
// per-run facilities. Tools must not hold on to it beyond the call.
type Deps struct {
	// Devtron is the only path to a cluster. There is no kubeconfig.
	Devtron *devtron.Client
	// Monitoring is bound to this run's cluster. It may report that no
	// metrics or alert backend exists, which is a fact tools must pass on
	// rather than hide behind an empty result.
	Monitoring *monitoring.Client
	// Knowledge is the embedded component catalog.
	Knowledge *knowledge.Catalog
	Cluster   ClusterInfo
	Log       *slog.Logger
	Cache     *Cache
	// Redact is applied to every free-text field that leaves the tool.
	Redact func(string) string
	// Artifacts stores oversized content and returns a reference.
	Artifacts ArtifactSink
}

// RedactString applies the run's redactor when one is configured.
func (d *Deps) RedactString(s string) string {
	if d == nil || d.Redact == nil {
		return s
	}
	return d.Redact(s)
}

// ClusterInfo is the run's target, resolved before any tool runs.
type ClusterInfo struct {
	// ID is the Devtron cluster id, which every orchestrator call needs.
	ID   int
	Name string
	// EnvID, Namespace and AppName scope the run when the user narrowed it.
	// A zero or empty value means "not narrowed", never "unknown".
	EnvID     int
	Namespace string
	AppName   string
}

// ArtifactSink stores a blob and returns an opaque reference.
type ArtifactSink interface {
	Put(ctx context.Context, name, contentType string, data []byte) (string, error)
}

// Tool is a read-only capability exposed to the agent.
type Tool interface {
	Name() string        // fully qualified, e.g. "k8s.events"
	Package() string     // "k8s"
	Description() string // says when to use it, not only what it does
	// Invoke runs the tool with JSON-encoded arguments.
	Invoke(ctx context.Context, d *Deps, args json.RawMessage) (*Result, error)
	// Bind returns an ADK tool wired to the given deps.
	Bind(d *Deps) (adktool.Tool, error)
}

// Func is a tool body. It returns an envelope; a Go error means a platform
// bug, not a cluster or input condition (put those in Result.Error).
type Func[TArgs any] func(ctx context.Context, d *Deps, args TArgs) (*Result, error)

type typed[TArgs any] struct {
	name, pkg, desc string
	fn              Func[TArgs]
}

// Define declares a tool with typed arguments. TArgs must be a struct with
// json tags and `jsonschema:"description"` style comments are encouraged.
func Define[TArgs any](name, pkg, description string, fn Func[TArgs]) Tool {
	if !strings.HasPrefix(name, pkg+".") {
		panic(fmt.Sprintf("tool %q must be prefixed with its package %q", name, pkg))
	}
	return &typed[TArgs]{name: name, pkg: pkg, desc: description, fn: fn}
}

func (t *typed[TArgs]) Name() string        { return t.name }
func (t *typed[TArgs]) Package() string     { return t.pkg }
func (t *typed[TArgs]) Description() string { return t.desc }

func (t *typed[TArgs]) Invoke(ctx context.Context, d *Deps, raw json.RawMessage) (*Result, error) {
	var args TArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return Fail(ErrInput, "bad_arguments", err.Error(), false), nil
		}
	}
	return t.call(ctx, d, args)
}

// CallTimeout bounds one tool invocation. Tools that legitimately need
// longer (log-store range scans) still finish well inside it.
var CallTimeout = 30 * time.Second

func (t *typed[TArgs]) call(ctx context.Context, d *Deps, args TArgs) (*Result, error) {
	ctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	res, err := t.fn(ctx, d, args)
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return Fail(ErrCluster, "timeout", fmt.Sprintf("%s did not finish within %s", t.name, CallTimeout), true,
			"narrow the request (namespace, selector, shorter window) and retry once"), nil
	}
	if err != nil {
		if d != nil && d.Log != nil {
			d.Log.Error("tool platform error", "tool", t.name, "err", err)
		}
		return Fail(ErrPlatform, "tool_failure", err.Error(), false), nil
	}
	if res == nil {
		res = &Result{Summary: "no data"}
	}
	return res, nil
}

// Bind adapts the tool to ADK. ADK tool names cannot contain dots, so the
// exposed name is "k8s_events" while our ledger keeps "k8s.events".
func (t *typed[TArgs]) Bind(d *Deps) (adktool.Tool, error) {
	return functiontool.New[TArgs, *Result](functiontool.Config{
		Name:        ADKName(t.name),
		Description: t.desc,
	}, func(tc agent.ToolContext, args TArgs) (*Result, error) {
		return t.call(tc, d, args)
	})
}

// ADKName maps "k8s.events" to "k8s_events".
func ADKName(name string) string { return strings.ReplaceAll(name, ".", "_") }

// Registry holds all tools compiled into the binary. There are, by
// construction, no write tools to register.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry builds an empty registry.
func NewRegistry() *Registry { return &Registry{tools: map[string]Tool{}} }

// Register adds tools; duplicate names panic at init.
func (r *Registry) Register(ts ...Tool) *Registry {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range ts {
		if _, dup := r.tools[t.Name()]; dup {
			panic("duplicate tool " + t.Name())
		}
		r.tools[t.Name()] = t
	}
	return r
}

// Get returns a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Select resolves a workflow step's tool patterns to concrete tool names. An
// empty pattern list means NO tools, which is what a step that lists none
// must get. Names() treats an empty list as "every tool" because it is the
// inventory call, and that difference has bitten twice: use Select wherever a
// step's allowlist is being resolved.
func (r *Registry) Select(patterns []string) []string {
	if len(patterns) == 0 {
		return nil
	}
	return r.Names(patterns...)
}

// Names returns sorted tool names, optionally filtered by glob-lite patterns
// ("k8s.*", "prom.query"). With no patterns it returns every registered tool,
// so it is the inventory call, not the allowlist call; see Select.
func (r *Registry) Names(patterns ...string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []string
	for name := range r.tools {
		if len(patterns) == 0 || matchAny(name, patterns) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func matchAny(name string, patterns []string) bool {
	for _, p := range patterns {
		if p == name {
			return true
		}
		if strings.HasSuffix(p, ".*") && strings.HasPrefix(name, strings.TrimSuffix(p, "*")) {
			return true
		}
	}
	return false
}

// Cache dedupes identical reads within one run. Keys are tool name plus a
// canonical argument encoding; values are Results with Freshness.CacheAge
// filled on hit.
type Cache struct {
	mu   sync.Mutex
	ttl  time.Duration
	data map[string]cacheEntry
}

type cacheEntry struct {
	at  time.Time
	res *Result
}

// NewCache builds a cache with the given TTL.
func NewCache(ttl time.Duration) *Cache { return &Cache{ttl: ttl, data: map[string]cacheEntry{}} }

// Key builds a cache key.
func Key(tool string, args any) string {
	b, _ := json.Marshal(args)
	return tool + ":" + string(b)
}

// Get returns a cached result, marking its age.
func (c *Cache) Get(key string) (*Result, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.data[key]
	if !ok || time.Since(e.at) > c.ttl {
		return nil, false
	}
	cp := *e.res
	if cp.Freshness != nil {
		f := *cp.Freshness
		f.CacheAge = time.Since(e.at).Round(time.Second).String()
		f.Source = "cache(" + f.Source + ")"
		cp.Freshness = &f
	}
	return &cp, true
}

// Put stores a result unless it is an error.
func (c *Cache) Put(key string, res *Result) {
	if c == nil || res == nil || res.Error != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[key] = cacheEntry{at: time.Now(), res: res}
}

// Cached wraps a fetch with the per-run cache.
func Cached(ctx context.Context, d *Deps, key string, fetch func() (*Result, error)) (*Result, error) {
	if d != nil && d.Cache != nil {
		if r, ok := d.Cache.Get(key); ok {
			return r, nil
		}
	}
	r, err := fetch()
	if err == nil && d != nil && d.Cache != nil {
		d.Cache.Put(key, r)
	}
	return r, err
}

// IsCanceled reports context cancellation so tools can classify it.
func IsCanceled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
