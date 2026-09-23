// Package knowledge identifies what a failing workload actually is and hands
// back what is known about it.
//
// A bare Kubernetes object tells you almost nothing: "StatefulSet
// pg-primary-0 is CrashLooping" is not actionable. The same object, once
// recognised as PostgreSQL from its Helm chart and labels, comes with known
// failure modes, the metrics worth querying and remediation that a senior SRE
// would actually recommend. That recognition is this package's whole job.
//
// Two packs ship embedded:
//
//	packs/devtron  Devtron's own microservices, including the Prometheus
//	               metrics each one exposes, so alerts about the platform
//	               itself can be debugged rather than guessed at.
//	packs/apps     Well-known third-party products deployed by Helm.
package knowledge

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

//go:embed packs
var packFS embed.FS

// Class separates the packs.
type Class string

const (
	// ClassDevtron is a Devtron platform microservice.
	ClassDevtron Class = "devtron"
	// ClassApp is a well-known third-party product.
	ClassApp Class = "app"
)

// Metric is one Prometheus metric a component exposes, read out of its source.
type Metric struct {
	Name   string   `yaml:"name" json:"name"`
	Type   string   `yaml:"type" json:"type"` // counter | gauge | histogram | summary
	Labels []string `yaml:"labels,omitempty" json:"labels,omitempty"`
	// Help is the metric's own help string where the code provides one.
	Help string `yaml:"help,omitempty" json:"help,omitempty"`
	// Means is what a change in this metric tells an SRE, which is the part
	// the help string never says.
	Means string `yaml:"means,omitempty" json:"means,omitempty"`
	// Query is a ready PromQL expression worth running when investigating.
	Query string `yaml:"query,omitempty" json:"query,omitempty"`
}

// Component is one recognisable thing, with its matching rules and its doc.
type Component struct {
	ID      string `yaml:"id" json:"id"`
	Name    string `yaml:"name" json:"name"`
	Class   Class  `yaml:"class" json:"class"`
	Summary string `yaml:"summary,omitempty" json:"summary,omitempty"`

	// Matching rules, cheapest and most specific first.
	Aliases    []string          `yaml:"aliases,omitempty" json:"aliases,omitempty"`
	ChartNames []string          `yaml:"chartNames,omitempty" json:"chartNames,omitempty"`
	Labels     map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
	Images     []string          `yaml:"images,omitempty" json:"images,omitempty"`
	Namespaces []string          `yaml:"namespaces,omitempty" json:"namespaces,omitempty"`

	// Metrics is the component's Prometheus surface.
	Metrics []Metric `yaml:"metrics,omitempty" json:"metrics,omitempty"`
	// Repo is where the facts came from, so a reader can check them.
	Repo string `yaml:"repo,omitempty" json:"repo,omitempty"`

	// Skill is the ADK skill name, which is the directory name. The model
	// loads the full document with load_skill using exactly this value.
	Skill string `yaml:"-" json:"skill,omitempty"`
	// Doc is the markdown body: what it does, how it fails, how to fix it.
	Doc string `yaml:"-" json:"doc,omitempty"`
}

// Signals is what is known about a workload at identification time. Every
// field is optional; more fields make the match more confident.
type Signals struct {
	Name       string
	Namespace  string
	Kind       string
	Labels     map[string]string
	HelmChart  string
	Images     []string
	DevtronApp string
	// AlertName lets a platform alert like DevtronOrchestratorDown match even
	// when no workload was identified.
	AlertName string
}

// Match is one identification with the evidence that produced it.
type Match struct {
	Component *Component `json:"component"`
	// Score is higher for more specific evidence. A chart name is worth more
	// than a substring of a pod name.
	Score int      `json:"score"`
	Why   []string `json:"why"`
}

// Catalog holds the loaded packs.
type Catalog struct {
	byID map[string]*Component
	all  []*Component
}

var (
	loadOnce sync.Once
	loaded   *Catalog
	loadErr  error
)

// Load parses the embedded packs once.
func Load() (*Catalog, error) {
	loadOnce.Do(func() {
		loaded, loadErr = parseFS(packFS, "packs")
	})
	return loaded, loadErr
}

// parseFS walks packs/<class>/<name>/ directories. Each holds two files:
//
//	SKILL.md        ADK-valid frontmatter (name, description) plus the prose.
//	                ADK's parser is strict, so nothing else may go in there.
//	component.yaml  our identification rules and the metric facts.
//
// The split exists so one directory serves both consumers: ADK's skill
// toolset discloses the prose to the model on demand, while this package
// does the deterministic matching ADK has no equivalent for.
func parseFS(fsys fs.FS, root string) (*Catalog, error) {
	cat := &Catalog{byID: map[string]*Component{}}
	err := fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "component.yaml" {
			return err
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		var comp Component
		if err := yaml.Unmarshal(b, &comp); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		if comp.ID == "" {
			return fmt.Errorf("%s: needs an id", p)
		}
		if comp.Name == "" {
			comp.Name = comp.ID
		}
		dir := path.Dir(p)
		comp.Skill = path.Base(dir)
		if md, err := fs.ReadFile(fsys, path.Join(dir, "SKILL.md")); err == nil {
			comp.Doc = skillBody(string(md))
		}
		if _, dup := cat.byID[comp.ID]; dup {
			return fmt.Errorf("%s: duplicate component id %q", p, comp.ID)
		}
		cat.byID[comp.ID] = &comp
		cat.all = append(cat.all, &comp)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(cat.all, func(i, j int) bool { return cat.all[i].ID < cat.all[j].ID })
	return cat, nil
}

// skillBody strips the YAML frontmatter and returns the markdown.
func skillBody(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "---") {
		return s
	}
	rest := s[3:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return s
	}
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest[end:]), "---"))
}

// Get returns a component by id.
func (c *Catalog) Get(id string) (*Component, bool) {
	if c == nil {
		return nil, false
	}
	comp, ok := c.byID[id]
	return comp, ok
}

// All returns every component.
func (c *Catalog) All() []*Component {
	if c == nil {
		return nil
	}
	return c.all
}

// OfClass returns every component in one pack.
func (c *Catalog) OfClass(class Class) []*Component {
	var out []*Component
	for _, comp := range c.All() {
		if comp.Class == class {
			out = append(out, comp)
		}
	}
	return out
}

// Identify ranks components against the signals, best first. An empty result
// means the workload is not something we recognise, which the agent must say
// plainly rather than inventing product-specific advice.
func (c *Catalog) Identify(s Signals) []Match {
	if c == nil {
		return nil
	}
	var out []Match
	for _, comp := range c.all {
		if m := score(comp, s); m.Score > 0 {
			m.Component = comp
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// Best returns the single most likely component, or nil.
func (c *Catalog) Best(s Signals) *Match {
	m := c.Identify(s)
	if len(m) == 0 {
		return nil
	}
	return &m[0]
}

// Scoring weights. A Helm chart name states what something is; a pod name
// only hints at it, and "redis" appears in plenty of pod names that are not
// Redis.
const (
	scoreChart     = 100
	scoreImage     = 80
	scoreLabel     = 60
	scoreAlertName = 50
	scoreAlias     = 30
	scoreNamespace = 10
)

func score(comp *Component, s Signals) Match {
	var m Match
	add := func(n int, why string) {
		m.Score += n
		m.Why = append(m.Why, why)
	}

	if s.HelmChart != "" {
		chart := normalizeChart(s.HelmChart)
		for _, want := range comp.ChartNames {
			if chart == normalizeChart(want) || strings.HasPrefix(chart, normalizeChart(want)+"-") {
				add(scoreChart, "helm chart "+s.HelmChart)
				break
			}
		}
	}
	for _, img := range s.Images {
		for _, want := range comp.Images {
			if strings.Contains(strings.ToLower(img), strings.ToLower(want)) {
				add(scoreImage, "image "+img)
				break
			}
		}
	}
	for k, want := range comp.Labels {
		if got, ok := s.Labels[k]; ok && strings.EqualFold(got, want) {
			add(scoreLabel, "label "+k+"="+got)
		}
	}
	// Chart and product names also arrive as standard labels.
	for _, key := range []string{"app.kubernetes.io/name", "app.kubernetes.io/part-of", "app", "helm.sh/chart"} {
		v := strings.ToLower(s.Labels[key])
		if v == "" {
			continue
		}
		for _, a := range comp.matchNames() {
			if v == a || strings.HasPrefix(v, a+"-") {
				add(scoreLabel, "label "+key+"="+s.Labels[key])
				break
			}
		}
	}
	if s.AlertName != "" {
		an := strings.ToLower(s.AlertName)
		for _, a := range comp.matchNames() {
			if len(a) >= 4 && strings.Contains(an, a) {
				add(scoreAlertName, "alert name mentions "+a)
				break
			}
		}
	}
	for _, field := range []string{s.Name, s.DevtronApp} {
		if field == "" {
			continue
		}
		f := strings.ToLower(field)
		for _, a := range comp.matchNames() {
			if len(a) >= 4 && strings.Contains(f, a) {
				add(scoreAlias, "name "+field+" contains "+a)
				break
			}
		}
	}
	if s.Namespace != "" {
		for _, ns := range comp.Namespaces {
			if strings.EqualFold(ns, s.Namespace) {
				add(scoreNamespace, "namespace "+s.Namespace)
				break
			}
		}
	}
	return m
}

func (c *Component) matchNames() []string {
	out := make([]string, 0, len(c.Aliases)+1)
	out = append(out, strings.ToLower(c.Name))
	for _, a := range c.Aliases {
		out = append(out, strings.ToLower(a))
	}
	return out
}

// normalizeChart strips the version Helm appends, turning
// "postgresql-12.5.6" into "postgresql".
func normalizeChart(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	parts := strings.Split(s, "-")
	for i := len(parts) - 1; i > 0; i-- {
		if p := parts[i]; p != "" && (p[0] >= '0' && p[0] <= '9') {
			parts = parts[:i]
			continue
		}
		break
	}
	return strings.Join(parts, "-")
}

// Search does a substring lookup over names, aliases and summaries, for the
// agent's own knowledge tool.
func (c *Catalog) Search(q string, limit int) []*Component {
	if c == nil || strings.TrimSpace(q) == "" {
		return nil
	}
	ql := strings.ToLower(q)
	var out []*Component
	for _, comp := range c.all {
		hay := strings.ToLower(comp.ID + " " + comp.Name + " " + comp.Summary + " " + strings.Join(comp.Aliases, " "))
		if strings.Contains(hay, ql) {
			out = append(out, comp)
			continue
		}
		for _, m := range comp.Metrics {
			if strings.Contains(strings.ToLower(m.Name), ql) {
				out = append(out, comp)
				break
			}
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}
