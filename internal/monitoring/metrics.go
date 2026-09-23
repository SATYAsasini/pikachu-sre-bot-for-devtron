package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/devtron-labs/devtron-sre-agent/internal/devtron"
)

// Sample is one instant value.
type Sample struct {
	Labels map[string]string `json:"labels"`
	Value  float64           `json:"value"`
	At     time.Time         `json:"at"`
}

// Point is one value in a range.
type Point struct {
	At    time.Time `json:"at"`
	Value float64   `json:"value"`
}

// Series is one labelled time series.
type Series struct {
	Labels map[string]string `json:"labels"`
	Points []Point           `json:"points"`
}

// Stats returns the lowest, highest and last value, which summarises a
// series without shipping every point to the model — almost always what a
// caller actually needs.
func (s Series) Stats() (lo, hi, last float64) {
	lo, hi = math.Inf(1), math.Inf(-1)
	for _, p := range s.Points {
		if p.Value < lo {
			lo = p.Value
		}
		if p.Value > hi {
			hi = p.Value
		}
		last = p.Value
	}
	if len(s.Points) == 0 {
		return 0, 0, 0
	}
	return lo, hi, last
}

// QueryResult is a decoded Prometheus-compatible answer.
type QueryResult struct {
	ResultType string   `json:"resultType"`
	Samples    []Sample `json:"samples,omitempty"`
	Series     []Series `json:"series,omitempty"`
	Warnings   []string `json:"warnings,omitempty"`
}

// Empty reports that the query matched nothing, which is a real answer and
// not a failure: it usually means the metric does not exist here.
func (r *QueryResult) Empty() bool {
	return r == nil || (len(r.Samples) == 0 && len(r.Series) == 0)
}

// MetricsAvailable reports a reachable query endpoint.
func (c *Client) MetricsAvailable() bool {
	return c.stack != nil && c.stack.Metrics != nil && c.stack.Metrics.Reachable
}

// Flavor names the metrics backend that answered, e.g. prometheus or
// victoriametrics. Worth telling the model: the query dialect is the same but
// the operational advice is not.
func (c *Client) Flavor() string {
	if !c.MetricsAvailable() {
		return "none"
	}
	return string(c.stack.Metrics.Flavor)
}

func (c *Client) metricsEndpoint() (*devtron.Endpoint, error) {
	if !c.MetricsAvailable() {
		return nil, fmt.Errorf("no reachable metrics endpoint in cluster %d: %s", c.clusterID, c.stackNote())
	}
	return c.stack.Metrics, nil
}

// Query runs an instant PromQL query.
func (c *Client) Query(ctx context.Context, expr string, at time.Time) (*QueryResult, error) {
	ep, err := c.metricsEndpoint()
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("query", expr)
	if !at.IsZero() {
		q.Set("time", formatTime(at))
	}
	body, err := c.get(ctx, ep, ep.Path("api/v1/query"), q)
	if err != nil {
		return nil, err
	}
	return decodeQuery(body)
}

// QueryRange runs a range query. Step is chosen for the caller when zero so
// the result stays around 100 points whatever the window.
func (c *Client) QueryRange(ctx context.Context, expr string, start, end time.Time, step time.Duration) (*QueryResult, error) {
	ep, err := c.metricsEndpoint()
	if err != nil {
		return nil, err
	}
	if end.IsZero() {
		end = time.Now()
	}
	if start.IsZero() {
		start = end.Add(-1 * time.Hour)
	}
	if step <= 0 {
		step = end.Sub(start) / 100
		if step < 15*time.Second {
			step = 15 * time.Second
		}
	}
	q := url.Values{}
	q.Set("query", expr)
	q.Set("start", formatTime(start))
	q.Set("end", formatTime(end))
	q.Set("step", strconv.FormatFloat(step.Seconds(), 'f', -1, 64)+"s")
	body, err := c.get(ctx, ep, ep.Path("api/v1/query_range"), q)
	if err != nil {
		return nil, err
	}
	return decodeQuery(body)
}

// MetricNames lists metric names, optionally narrowed by series selectors.
// This is the "what can I even measure here" call: the agent uses it before
// inventing a PromQL expression, because guessing a metric name that does not
// exist in this cluster is the most common way a metrics investigation fails.
func (c *Client) MetricNames(ctx context.Context, match []string, limit int) ([]string, error) {
	ep, err := c.metricsEndpoint()
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	for _, m := range match {
		q.Add("match[]", m)
	}
	body, err := c.get(ctx, ep, ep.Path("api/v1/label/__name__/values"), q)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Status string   `json:"status"`
		Data   []string `json:"data"`
		Error  string   `json:"error"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode metric names: %w", err)
	}
	if raw.Status != "success" {
		return nil, fmt.Errorf("metric names: %s", orDefault(raw.Error, raw.Status))
	}
	sort.Strings(raw.Data)
	if limit > 0 && len(raw.Data) > limit {
		raw.Data = raw.Data[:limit]
	}
	return raw.Data, nil
}

// SeriesFor lists the label sets matching selectors, which is how the agent
// learns what labels a metric actually carries in this cluster.
func (c *Client) SeriesFor(ctx context.Context, match []string, start, end time.Time, limit int) ([]map[string]string, error) {
	ep, err := c.metricsEndpoint()
	if err != nil {
		return nil, err
	}
	if len(match) == 0 {
		return nil, fmt.Errorf("at least one series selector is required")
	}
	if end.IsZero() {
		end = time.Now()
	}
	if start.IsZero() {
		start = end.Add(-1 * time.Hour)
	}
	q := url.Values{}
	for _, m := range match {
		q.Add("match[]", m)
	}
	q.Set("start", formatTime(start))
	q.Set("end", formatTime(end))
	body, err := c.get(ctx, ep, ep.Path("api/v1/series"), q)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Status string              `json:"status"`
		Data   []map[string]string `json:"data"`
		Error  string              `json:"error"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode series: %w", err)
	}
	if raw.Status != "success" {
		return nil, fmt.Errorf("series: %s", orDefault(raw.Error, raw.Status))
	}
	if limit > 0 && len(raw.Data) > limit {
		raw.Data = raw.Data[:limit]
	}
	return raw.Data, nil
}

// Rules fetches the configured alerting and recording rules, which is how the
// agent explains why an alert fired rather than guessing its threshold.
func (c *Client) Rules(ctx context.Context, nameLike string) ([]Rule, error) {
	ep, err := c.metricsEndpoint()
	if err != nil {
		return nil, err
	}
	body, err := c.get(ctx, ep, ep.Path("api/v1/rules"), nil)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Status string `json:"status"`
		Data   struct {
			Groups []struct {
				Name  string `json:"name"`
				File  string `json:"file"`
				Rules []struct {
					Name        string            `json:"name"`
					Query       string            `json:"query"`
					Duration    float64           `json:"duration"`
					Labels      map[string]string `json:"labels"`
					Annotations map[string]string `json:"annotations"`
					Health      string            `json:"health"`
					Type        string            `json:"type"`
					LastError   string            `json:"lastError"`
				} `json:"rules"`
			} `json:"groups"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode rules: %w", err)
	}
	var out []Rule
	for _, g := range raw.Data.Groups {
		for _, r := range g.Rules {
			if nameLike != "" && !strings.Contains(strings.ToLower(r.Name), strings.ToLower(nameLike)) {
				continue
			}
			out = append(out, Rule{
				Name: r.Name, Group: g.Name, File: g.File, Type: r.Type,
				Query: r.Query, For: time.Duration(r.Duration) * time.Second,
				Labels: r.Labels, Annotations: r.Annotations,
				Health: r.Health, LastError: r.LastError,
			})
		}
	}
	return out, nil
}

// Rule is one configured alerting or recording rule.
type Rule struct {
	Name        string            `json:"name"`
	Group       string            `json:"group"`
	File        string            `json:"file,omitempty"`
	Type        string            `json:"type"` // alerting | recording
	Query       string            `json:"query"`
	For         time.Duration     `json:"for,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Health      string            `json:"health,omitempty"`
	LastError   string            `json:"lastError,omitempty"`
}

func decodeQuery(body []byte) (*QueryResult, error) {
	var raw struct {
		Status string `json:"status"`
		Error  string `json:"error"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Metric map[string]string `json:"metric"`
				Value  []any             `json:"value"`
				Values [][]any           `json:"values"`
			} `json:"result"`
		} `json:"data"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode query response: %w", err)
	}
	if raw.Status != "success" {
		return nil, fmt.Errorf("query failed: %s", orDefault(raw.Error, raw.Status))
	}
	out := &QueryResult{ResultType: raw.Data.ResultType, Warnings: raw.Warnings}
	for _, r := range raw.Data.Result {
		if len(r.Value) == 2 {
			at, v := decodePair(r.Value)
			out.Samples = append(out.Samples, Sample{Labels: r.Metric, Value: v, At: at})
		}
		if len(r.Values) > 0 {
			s := Series{Labels: r.Metric}
			for _, pv := range r.Values {
				at, v := decodePair(pv)
				s.Points = append(s.Points, Point{At: at, Value: v})
			}
			out.Series = append(out.Series, s)
		}
	}
	return out, nil
}

// decodePair reads Prometheus' [<unix seconds>, "<value>"] pair. The value is
// a string on the wire, including "NaN" and "+Inf".
func decodePair(p []any) (time.Time, float64) {
	var at time.Time
	var val float64
	if len(p) != 2 {
		return at, val
	}
	if ts, ok := p[0].(float64); ok {
		sec, frac := math.Modf(ts)
		at = time.Unix(int64(sec), int64(frac*1e9)).UTC()
	}
	if s, ok := p[1].(string); ok {
		val, _ = strconv.ParseFloat(s, 64)
	}
	return at, val
}

func formatTime(t time.Time) string {
	return strconv.FormatFloat(float64(t.UnixNano())/1e9, 'f', 3, 64)
}

// MetricMeta is a metric's self-description, as the exporting component
// declares it. This is why a custom metric from any third-party component is
// still explainable: the component ships its own HELP and TYPE, and
// Prometheus keeps them.
type MetricMeta struct {
	Metric string `json:"metric"`
	Type   string `json:"type,omitempty"`
	Help   string `json:"help,omitempty"`
	Unit   string `json:"unit,omitempty"`
}

// Metadata returns HELP and TYPE for scraped metrics. With metric == "" it
// returns everything the backend knows, which is large, so callers should
// narrow it or bound limit.
//
// VictoriaMetrics implements this endpoint too, but some builds return an
// empty set; an empty answer therefore means "not published", never "the
// metric does not exist".
func (c *Client) Metadata(ctx context.Context, metric string, limit int) ([]MetricMeta, error) {
	ep, err := c.metricsEndpoint()
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	if metric != "" {
		q.Set("metric", metric)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	body, err := c.get(ctx, ep, ep.Path("api/v1/metadata"), q)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Status string `json:"status"`
		Data   map[string][]struct {
			Type string `json:"type"`
			Help string `json:"help"`
			Unit string `json:"unit"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode metadata: %w", err)
	}
	out := make([]MetricMeta, 0, len(raw.Data))
	for name, entries := range raw.Data {
		m := MetricMeta{Metric: name}
		if len(entries) > 0 {
			m.Type, m.Help, m.Unit = entries[0].Type, entries[0].Help, entries[0].Unit
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Metric < out[j].Metric })
	return out, nil
}

// Target is one scrape target as the metrics backend sees it.
type Target struct {
	Labels     map[string]string `json:"labels"`
	ScrapeURL  string            `json:"scrapeUrl"`
	Health     string            `json:"health"`
	LastError  string            `json:"lastError,omitempty"`
	LastScrape string            `json:"lastScrape,omitempty"`
}

// Targets lists active scrape targets, optionally filtered to a namespace.
// This answers a question no amount of PromQL can: whether the component is
// being scraped at all. A workload that exposes metrics nobody collects looks
// exactly like a workload with no metrics.
func (c *Client) Targets(ctx context.Context, namespace string) ([]Target, error) {
	ep, err := c.metricsEndpoint()
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("state", "active")
	body, err := c.get(ctx, ep, ep.Path("api/v1/targets"), q)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Status string `json:"status"`
		Data   struct {
			ActiveTargets []struct {
				Labels     map[string]string `json:"labels"`
				ScrapeURL  string            `json:"scrapeUrl"`
				Health     string            `json:"health"`
				LastError  string            `json:"lastError"`
				LastScrape string            `json:"lastScrape"`
			} `json:"activeTargets"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode targets: %w", err)
	}
	var out []Target
	for _, t := range raw.Data.ActiveTargets {
		if namespace != "" && t.Labels["namespace"] != namespace {
			continue
		}
		out = append(out, Target{
			Labels: t.Labels, ScrapeURL: t.ScrapeURL, Health: t.Health,
			LastError: t.LastError, LastScrape: t.LastScrape,
		})
	}
	return out, nil
}
