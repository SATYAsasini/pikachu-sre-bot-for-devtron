package devtron

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// find returns a candidate by namespace and name.
func find(t *testing.T, m *MonitoringStack, ns, name string) *Endpoint {
	t.Helper()
	for i := range m.Candidates {
		if m.Candidates[i].Service.Namespace == ns && m.Candidates[i].Service.Name == name {
			return &m.Candidates[i]
		}
	}
	t.Fatalf("no candidate %s/%s in %d candidates", ns, name, len(m.Candidates))
	return nil
}

func hasNote(m *MonitoringStack, substr string) bool {
	for _, n := range m.Notes {
		if strings.Contains(n, substr) {
			return true
		}
	}
	return false
}

func TestClassifySkipsMalformedObjects(t *testing.T) {
	t.Parallel()

	got := classify(fxMalformed())
	// Only the two well-formed, flavour-bearing objects survive; nothing
	// panics on a nil metadata, a string where labels should be, or a
	// boolean port.
	if len(got) != 2 {
		for _, e := range got {
			t.Logf("candidate %s/%s", e.Service.Namespace, e.Service.Name)
		}
		t.Fatalf("want 2 candidates from the malformed list, got %d", len(got))
	}
	for _, e := range got {
		if len(e.Ports) != 0 {
			t.Errorf("%s: ports of the wrong type should be dropped, got %v", e.Service.Name, e.Ports)
		}
	}
}

func TestClassifyKeepsUnicodeServices(t *testing.T) {
	t.Parallel()

	got := classify(fxUnicode())
	if len(got) != 2 {
		t.Fatalf("want 2 candidates, got %d", len(got))
	}
	var flavors []Flavor
	for _, e := range got {
		flavors = append(flavors, e.Flavor)
	}
	if !contains(flavors, FlavorPrometheus) || !contains(flavors, FlavorAlertmanager) {
		t.Errorf("unicode names lost their flavour: %v", flavors)
	}
}

func TestClassifyDemotesScrapeTargets(t *testing.T) {
	t.Parallel()

	got := classify(fxBusyCluster())

	// Every scrape target sorts after every real candidate, so a budget
	// spent top-down is spent on things that can answer.
	seenTarget := false
	for _, e := range got {
		if e.ScrapeTarget {
			seenTarget = true
			continue
		}
		if seenTarget {
			t.Fatalf("%s/%s is a real candidate ranked below a scrape target",
				e.Service.Namespace, e.Service.Name)
		}
	}

	byName := map[string]Endpoint{}
	for _, e := range got {
		byName[e.Service.Name] = e
	}
	for _, n := range []string{
		"victoria-metrics-core-dns", "victoria-metrics-kube-etcd",
		"kube-prom-kube-prometheus-kubelet", "victoria-metrics-stage-mon-grafana",
		"victoria-metrics-stage-mon-kube-state-metrics",
		"victoria-metrics-stage-mon-prometheus-node-exporter",
		"victoria-metrics-stage-mon-victoria-metrics-operator",
		"vmagent-victoria-metrics", "staging-optscale-finops-prometheus-pushgateway",
	} {
		if !byName[n].ScrapeTarget {
			t.Errorf("%s should be marked a scrape target", n)
		}
	}
	for _, n := range []string{
		"vmsingle-victoria-metrics", "vmalert-victoria-metrics",
		"vmalertmanager-victoria-metrics", "prometheus-operated",
	} {
		if byName[n].ScrapeTarget {
			t.Errorf("%s is a real candidate and must not be demoted", n)
		}
	}
}

func TestFlavorOfSeparatesVMAlertFromVMAlertmanager(t *testing.T) {
	t.Parallel()

	cases := map[string]Flavor{
		"vmalert-victoria-metrics":        FlavorVMAlert,
		"vmalertmanager-victoria-metrics": FlavorAlertmanager,
		"alertmanager-operated":           FlavorAlertmanager,
		"vmsingle-victoria-metrics":       FlavorVictoriaMetrics,
		"thanos-query-frontend":           FlavorThanos,
		"mimir-query-frontend":            FlavorMimir,
		"kube-prom-prometheus":            FlavorPrometheus,
		"redis-primary":                   FlavorUnknown,
	}
	for name, want := range cases {
		if got := flavorOf(name, nil); got != want {
			t.Errorf("%s: want %s, got %s", name, want, got)
		}
	}
}

// The bug this whole change exists for: the alert half must be found even
// when a dozen exporters sort ahead of it, and it must be found without
// probing them.
func TestDiscoverFindsBothHalvesPastAPileOfExporters(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{
		Objects: fxBusyCluster(),
		OK: map[string]string{
			probeKey("monitoring", "vmsingle-victoria-metrics"): fxPromOK(),
			probeKey("monitoring", "vmalert-victoria-metrics"):  fxVMAlertAlerts(),
		},
	})
	defer f.Close()

	d := NewDiscoverer(c, time.Minute)
	m, err := d.Get(t.Context(), 1, "default_cluster")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if m.Partial {
		t.Fatalf("a walk that finished reported itself partial: %v", m.Notes)
	}
	if !m.HasMetrics() || m.Metrics.Service.Name != "vmsingle-victoria-metrics" {
		t.Errorf("metrics: %s", m.Summary())
	}
	if !m.HasAlerts() || m.Alerts.Service.Name != "vmalert-victoria-metrics" {
		t.Errorf("alerts: %s", m.Summary())
	}
	if hasNote(m, "no alert source answered") {
		t.Errorf("found an alert source but still reported none: %v", m.Notes)
	}

	// The exporters are listed as candidates and never asked.
	for _, n := range []string{
		"victoria-metrics-core-dns", "victoria-metrics-stage-mon-grafana",
		"vmagent-victoria-metrics",
	} {
		if got := f.probed("kube-system", n) + f.probed("monitoring", n); got != 0 {
			t.Errorf("%s was probed %d times; scrape targets should be skipped", n, got)
		}
	}
	if find(t, m, "monitoring", "vmagent-victoria-metrics").Probed {
		t.Error("an unprobed candidate must not claim it was probed")
	}
}

func TestDiscoverReportsTheHalfItCouldNotFind(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{
		Objects: fxTwoAlertSources(),
		OK: map[string]string{
			probeKey("utils", "shared-monitoring-stack-ku-prometheus"): fxPromOK(),
		},
	})
	defer f.Close()

	m, err := NewDiscoverer(c, time.Minute).Get(t.Context(), 2, "shared")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if !m.HasMetrics() {
		t.Fatalf("metrics should have been found: %s", m.Summary())
	}
	if m.HasAlerts() {
		t.Fatalf("no alert source answered, but one was reported: %s", m.Summary())
	}
	if !hasNote(m, "no alert source answered") {
		t.Errorf("a missing alert source must be stated, not implied: %v", m.Notes)
	}
}

func TestDiscoverNoCandidatesAtAll(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{Objects: []map[string]any{}})
	defer f.Close()

	m, err := NewDiscoverer(c, time.Minute).Get(t.Context(), 1, "empty")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if !hasNote(m, "no Prometheus, VictoriaMetrics, Alertmanager or vmalert Service found") {
		t.Errorf("notes: %v", m.Notes)
	}
	if m.Partial {
		t.Error("an empty cluster is a complete answer, not a partial one")
	}
}

func TestDiscoverExportersOnlyIsNotCoverage(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{Objects: fxExportersOnly()})
	defer f.Close()

	m, err := NewDiscoverer(c, time.Minute).Get(t.Context(), 1, "exporters")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if m.HasMetrics() || m.HasAlerts() {
		t.Fatalf("exporters were reported as a monitoring stack: %s", m.Summary())
	}
	if !hasNote(m, "coverage is unknown, not healthy") {
		t.Errorf("notes: %v", m.Notes)
	}
}

func TestDiscoverServiceListFailureIsANote(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{ListErr: true})
	defer f.Close()

	m, err := NewDiscoverer(c, time.Minute).Get(t.Context(), 1, "broken")
	if err != nil {
		t.Fatalf("a failed list should be a note, not an error: %v", err)
	}
	if !hasNote(m, "service discovery failed") {
		t.Errorf("notes: %v", m.Notes)
	}
}

// The regression this change is for: a walk that ran out of time used to be
// cached and served for fifteen minutes as "this cluster has no alert
// source".
func TestPartialWalkIsNeverCached(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{
		Objects: fxTwoAlertSources(),
		OK: map[string]string{
			probeKey("utils", "shared-monitoring-stack-ku-prometheus"):   fxPromOK(),
			probeKey("utils", "shared-monitoring-stack-ku-alertmanager"): fxAlertmanagerStatus(),
		},
		Slow: map[string]time.Duration{
			probeKey("utils", "shared-monitoring-stack-ku-alertmanager"): 400 * time.Millisecond,
		},
	})
	defer f.Close()

	d := NewDiscoverer(c, time.Minute)

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	m, err := d.discover(ctx, 2, "shared", false)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if !m.Partial {
		t.Fatalf("a walk cut short must say so: %s / %v", m.Summary(), m.Notes)
	}
	if !hasNote(m, "ran out of time") {
		t.Errorf("notes: %v", m.Notes)
	}

	// And the walk that reported it partial must not have been stored. This
	// is the difference between "no alert source" and "we did not finish
	// looking", which is the single thing this service must never confuse.
	if cached := d.cached(2); cached != nil {
		t.Fatal("a partial walk was cached")
	}
}

func TestGetSurvivesACancelledCaller(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{
		Objects: fxTwoAlertSources(),
		OK: map[string]string{
			probeKey("utils", "shared-monitoring-stack-ku-prometheus"):   fxPromOK(),
			probeKey("utils", "shared-monitoring-stack-ku-alertmanager"): fxAlertmanagerStatus(),
		},
		ListDelay: 150 * time.Millisecond,
	})
	defer f.Close()

	d := NewDiscoverer(c, time.Minute)

	// A browser tab that navigates away. The caller gives up; the walk does
	// not, and nobody inherits a half-finished answer.
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if _, err := d.Get(ctx, 2, "shared"); err == nil {
		t.Fatal("a cancelled caller should get an error, not a partial stack")
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if m := d.cached(2); m != nil {
			if !m.HasAlerts() || !m.HasMetrics() {
				t.Fatalf("the detached walk cached an incomplete answer: %s", m.Summary())
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the detached walk never finished or never cached")
}

func TestConcurrentGetsCostOneWalk(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{
		Objects:   fxTwoAlertSources(),
		OK:        map[string]string{probeKey("utils", "shared-monitoring-stack-ku-prometheus"): fxPromOK()},
		ListDelay: 80 * time.Millisecond,
	})
	defer f.Close()

	d := NewDiscoverer(c, time.Minute)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := d.Get(t.Context(), 2, "shared"); err != nil {
				t.Errorf("get: %v", err)
			}
		}()
	}
	wg.Wait()

	if n := f.lists(); n != 1 {
		t.Errorf("eight readers of one cold cluster cost %d service lists, want 1", n)
	}
}

func TestProbeMeasuresEveryCandidate(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{
		Objects: fxBusyCluster(),
		OK: map[string]string{
			probeKey("monitoring", "vmsingle-victoria-metrics"):       fxPromOK(),
			probeKey("monitoring", "vmalert-victoria-metrics"):        fxVMAlertAlerts(),
			probeKey("monitoring", "vmalertmanager-victoria-metrics"): fxAlertmanagerStatus(),
		},
	})
	defer f.Close()

	m, err := NewDiscoverer(c, time.Minute).Probe(t.Context(), 1, "default_cluster")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	// The picker cannot ask somebody to choose between endpoints it never
	// measured, so a full probe leaves nothing unprobed — exporters included.
	for i := range m.Candidates {
		if !m.Candidates[i].Probed {
			t.Errorf("%s/%s was not probed by a full probe",
				m.Candidates[i].Service.Namespace, m.Candidates[i].Service.Name)
		}
	}
	// Both alert sources answer, which is exactly the case the picker exists
	// for.
	reachableAlerts := 0
	for i := range m.Candidates {
		if m.Candidates[i].IsAlertSource() && m.Candidates[i].Reachable {
			reachableAlerts++
		}
	}
	if reachableAlerts != 2 {
		t.Errorf("want 2 reachable alert sources, got %d", reachableAlerts)
	}
}

func TestChosenEndpointWinsOverTheHeuristic(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{
		Objects: fxBusyCluster(),
		OK: map[string]string{
			probeKey("monitoring", "vmsingle-victoria-metrics"):       fxPromOK(),
			probeKey("monitoring", "vmalert-victoria-metrics"):        fxVMAlertAlerts(),
			probeKey("monitoring", "vmalertmanager-victoria-metrics"): fxAlertmanagerStatus(),
		},
	})
	defer f.Close()

	d := NewDiscoverer(c, time.Minute)
	d.Overrides = func(context.Context, int) Choice {
		return Choice{Alerts: &Pick{Namespace: "monitoring", Name: "vmalertmanager-victoria-metrics"}}
	}

	m, err := d.Get(t.Context(), 1, "default_cluster")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if m.Alerts == nil || m.Alerts.Service.Name != "vmalertmanager-victoria-metrics" {
		t.Fatalf("the pinned alert source was not used: %s", m.Summary())
	}
	if !m.Alerts.Chosen {
		t.Error("a pinned endpoint should be marked chosen")
	}
	// Pinning an alert source means the others are not worth asking.
	if n := f.probed("monitoring", "vmalert-victoria-metrics"); n != 0 {
		t.Errorf("vmalert was probed %d times despite a pin elsewhere", n)
	}
	// The unpinned half still runs.
	if !m.HasMetrics() {
		t.Errorf("pinning alerts should not disturb metrics: %s", m.Summary())
	}
}

func TestChosenEndpointIsKeptEvenWhenItIsDown(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{
		Objects: fxBusyCluster(),
		OK: map[string]string{
			probeKey("monitoring", "vmsingle-victoria-metrics"): fxPromOK(),
			probeKey("monitoring", "vmalert-victoria-metrics"):  fxVMAlertAlerts(),
			// vmalertmanager answers nothing.
		},
	})
	defer f.Close()

	d := NewDiscoverer(c, time.Minute)
	d.Overrides = func(context.Context, int) Choice {
		return Choice{Alerts: &Pick{Namespace: "monitoring", Name: "vmalertmanager-victoria-metrics"}}
	}

	m, err := d.Get(t.Context(), 1, "default_cluster")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	// Being told your Alertmanager is down is the truth. Being quietly moved
	// onto a different one is how somebody reads another team's alerts and
	// believes they are their own.
	if m.Alerts == nil || m.Alerts.Service.Name != "vmalertmanager-victoria-metrics" {
		t.Fatalf("a down pin was silently replaced: %s", m.Summary())
	}
	if m.HasAlerts() {
		t.Error("an unreachable pin must not report itself usable")
	}
	if !hasNote(m, "did not answer") {
		t.Errorf("notes: %v", m.Notes)
	}
}

func TestChosenEndpointThatIsGoneFallsBack(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{
		Objects: fxBusyCluster(),
		OK: map[string]string{
			probeKey("monitoring", "vmsingle-victoria-metrics"): fxPromOK(),
			probeKey("monitoring", "vmalert-victoria-metrics"):  fxVMAlertAlerts(),
		},
	})
	defer f.Close()

	d := NewDiscoverer(c, time.Minute)
	d.Overrides = func(context.Context, int) Choice {
		return Choice{Alerts: &Pick{Namespace: "gone", Name: "uninstalled-alertmanager"}}
	}

	m, err := d.Get(t.Context(), 1, "default_cluster")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if m.Alerts == nil || m.Alerts.Service.Name != "vmalert-victoria-metrics" {
		t.Fatalf("a vanished pin should fall back to discovery: %s", m.Summary())
	}
	if !hasNote(m, "no longer a Service in this cluster") {
		t.Errorf("a vanished pin must be said out loud: %v", m.Notes)
	}
}

func TestPortlessFirstThenPortFallback(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{
		Objects:   fxNoPorts(),
		OK:        map[string]string{probeKey("monitoring", "prometheus-server"): fxPromOK()},
		NeedsPort: map[string]bool{},
	})
	defer f.Close()

	m, err := NewDiscoverer(c, time.Minute).Get(t.Context(), 1, "c")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if !m.HasMetrics() || m.Metrics.Service.Port != "" {
		t.Fatalf("a portless target should be preferred: %s", m.Summary())
	}
}

func TestPortRetriesAreCapped(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{Objects: fxManyPorts()})
	defer f.Close()

	if _, err := NewDiscoverer(c, time.Minute).Get(t.Context(), 1, "c"); err != nil {
		t.Fatalf("discover: %v", err)
	}
	// One portless attempt plus at most maxPortRetries, not one per port.
	if n := f.probed("monitoring", "prometheus-server"); n > 1+maxPortRetries {
		t.Errorf("%d attempts against one dead Service; the cap is %d", n, 1+maxPortRetries)
	}
}

func TestMetricsProbeRejectsANonPrometheusAnswer(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{
		Objects: fxNoPorts(),
		OK:      map[string]string{probeKey("monitoring", "prometheus-server"): fxPromNotJSON()},
	})
	defer f.Close()

	m, err := NewDiscoverer(c, time.Minute).Get(t.Context(), 1, "c")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	// A login page is a 200. Treating it as a metrics backend is how a run
	// concludes "no data" against a cluster that has plenty.
	if m.HasMetrics() {
		t.Fatalf("an HTML login page was accepted as Prometheus: %s", m.Summary())
	}
	if e := find(t, m, "monitoring", "prometheus-server"); !strings.Contains(e.Detail, "not with a Prometheus query result") {
		t.Errorf("detail: %q", e.Detail)
	}
}

func TestUnicodeServiceNamesSurviveTheProxy(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{
		Objects: fxUnicode(),
		OK: map[string]string{
			probeKey("監視", "プロメテウス-prometheus-server"): fxPromOK(),
			probeKey("监控", "alertmanager-告警"):          fxAlertmanagerStatus(),
		},
	})
	defer f.Close()

	m, err := NewDiscoverer(c, time.Minute).Get(t.Context(), 1, "c")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if !m.HasMetrics() || !m.HasAlerts() {
		t.Fatalf("unicode service names did not survive: %s", m.Summary())
	}
}

func TestInvalidateDropsTheCachedStack(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{
		Objects: fxNoPorts(),
		OK:      map[string]string{probeKey("monitoring", "prometheus-server"): fxPromOK()},
	})
	defer f.Close()

	d := NewDiscoverer(c, time.Minute)
	if _, err := d.Get(t.Context(), 1, "c"); err != nil {
		t.Fatalf("first get: %v", err)
	}
	if _, err := d.Get(t.Context(), 1, "c"); err != nil {
		t.Fatalf("second get: %v", err)
	}
	if n := f.lists(); n != 1 {
		t.Fatalf("the second get should have been cached; %d lists", n)
	}
	d.Invalidate(1)
	if _, err := d.Get(t.Context(), 1, "c"); err != nil {
		t.Fatalf("third get: %v", err)
	}
	if n := f.lists(); n != 2 {
		t.Errorf("invalidate did not force a new walk; %d lists", n)
	}
}

func TestPickAndChoiceZeroValues(t *testing.T) {
	t.Parallel()

	if (Pick{}).Valid() {
		t.Error("an empty pick is not valid")
	}
	if (Pick{Namespace: "monitoring"}).Valid() {
		t.Error("a pick needs a name")
	}
	if !(Choice{}).Empty() {
		t.Error("a zero choice overrides nothing")
	}
	if (Choice{Alerts: &Pick{Namespace: "a", Name: "b"}}).Empty() {
		t.Error("a choice with an alert pick is not empty")
	}
	if got := (Pick{Namespace: "監視", Name: "am"}).String(); got != "監視/am" {
		t.Errorf("String: %q", got)
	}
}

func TestSummaryNamesThePinnedEndpoint(t *testing.T) {
	t.Parallel()

	var nilStack *MonitoringStack
	if got := nilStack.Summary(); got != "no monitoring discovered" {
		t.Errorf("nil stack: %q", got)
	}

	m := &MonitoringStack{
		Metrics: &Endpoint{Flavor: FlavorVictoriaMetrics, Reachable: true,
			Service: ServiceRef{Namespace: "monitoring", Name: "vmsingle"}},
		Alerts: &Endpoint{Flavor: FlavorAlertmanager, Reachable: true, Chosen: true,
			Service: ServiceRef{Namespace: "monitoring", Name: "vmalertmanager"}},
	}
	got := m.Summary()
	if !strings.Contains(got, "[chosen]") || strings.Count(got, "[chosen]") != 1 {
		t.Errorf("only the pinned half should be marked: %q", got)
	}
}

func contains[T comparable](s []T, v T) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// A pin settles which endpoint is used. It must not settle what the picker
// is allowed to know about the alternatives, or switching back becomes a
// guess.
func TestFullProbeMeasuresAlternativesEvenWhenPinned(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{
		Objects: fxBusyCluster(),
		OK: map[string]string{
			probeKey("monitoring", "vmsingle-victoria-metrics"):       fxPromOK(),
			probeKey("monitoring", "vmalert-victoria-metrics"):        fxVMAlertAlerts(),
			probeKey("monitoring", "vmalertmanager-victoria-metrics"): fxAlertmanagerStatus(),
		},
	})
	defer f.Close()

	d := NewDiscoverer(c, time.Minute)
	d.Overrides = func(context.Context, int) Choice {
		return Choice{Alerts: &Pick{Namespace: "monitoring", Name: "vmalertmanager-victoria-metrics"}}
	}

	m, err := d.Probe(t.Context(), 1, "default_cluster")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if m.Alerts == nil || !m.Alerts.Chosen {
		t.Fatalf("the pin should still win: %s", m.Summary())
	}
	alt := find(t, m, "monitoring", "vmalert-victoria-metrics")
	if !alt.Probed || !alt.Reachable {
		t.Errorf("the alternative was not measured: probed=%v reachable=%v", alt.Probed, alt.Reachable)
	}
	// And the pin is measured once, not once per pass.
	if n := f.probed("monitoring", "vmalertmanager-victoria-metrics"); n != 1 {
		t.Errorf("the pinned endpoint was probed %d times, want 1", n)
	}
}

// Cutting the per-attempt timeout to 4s to make walks finish sooner broke
// discovery on installs where the proxy hop genuinely takes longer than
// that. The default has to clear a slow orchestrator, and be raisable.
func TestProbeTimeoutIsGenerousAndOverridable(t *testing.T) {
	t.Parallel()

	d := NewDiscoverer(nil, time.Minute)
	if d.probeTimeout() != DefaultDiscoveryProbeTimeout {
		t.Errorf("unset should use the default, got %v", d.probeTimeout())
	}
	if DefaultDiscoveryProbeTimeout < 10*time.Second {
		t.Errorf("a service proxy hop needs room; default is %v", DefaultDiscoveryProbeTimeout)
	}
	d.ProbeTimeout = 30 * time.Second
	if d.probeTimeout() != 30*time.Second {
		t.Errorf("override ignored, got %v", d.probeTimeout())
	}
	d.ProbeTimeout = -1
	if d.probeTimeout() != DefaultDiscoveryProbeTimeout {
		t.Errorf("a nonsense override should fall back, got %v", d.probeTimeout())
	}
}

// A candidate is tried portless and then on its ports. Reporting only the
// first failure meant a fast rejection of the portless target hid a timeout
// on the port that mattered.
func TestProbeFailureReportsEveryAttempt(t *testing.T) {
	t.Parallel()

	mixed := &probeFailure{attempts: []attemptFailure{
		{port: "", err: &Error{Status: 503, Path: "/x"}},
		{port: "8429", err: context.DeadlineExceeded},
	}}
	got := mixed.Error()
	if !strings.Contains(got, "no port") || !strings.Contains(got, ":8429") {
		t.Errorf("want both targets named, got %q", got)
	}
	if !strings.Contains(got, "503") || !strings.Contains(got, "timed out") {
		t.Errorf("want both reasons, got %q", got)
	}

	// All failing the same way is said once, not three times.
	same := &probeFailure{attempts: []attemptFailure{
		{port: "", err: &Error{Status: 403, Path: "/x"}},
		{port: "9093", err: &Error{Status: 403, Path: "/x"}},
	}}
	if n := strings.Count(same.Error(), "HTTP 403"); n != 1 {
		t.Errorf("one shared reason should be stated once, got %d in %q", n, same.Error())
	}

	if got := (&probeFailure{}).Error(); got == "" {
		t.Error("an empty failure still has to say something")
	}
	// It still unwraps to a *Error so callers can classify it.
	var de *Error
	if !errors.As(mixed, &de) || de.Status != 503 {
		t.Errorf("want the first HTTP error reachable through Unwrap, got %v", de)
	}
}

// A Service whose object arrived without a spec still has to be reachable.
//
// Some Devtron versions answer the filtered resource list with table rows
// rather than whole objects, so every candidate comes back with no ports.
// Without a port the Kubernetes service proxy only resolves for a
// single-port Service, so a two-port vmsingle answers 503 and — before this
// — there was nothing left to try. On a 52-cluster install that was every
// monitoring service on every cluster.
func TestPortlessCandidatesFallBackToWellKnownPorts(t *testing.T) {
	t.Parallel()

	f, c := newFakeDevtron(fakeOpts{
		// No ports on the Service, exactly as that response shape delivers.
		Objects: []map[string]any{svc("monitoring", "vmsingle-victoria-metrics")},
		OK:      map[string]string{probeKey("monitoring", "vmsingle-victoria-metrics"): fxPromOK()},
		// And the portless target does not resolve, as it does not for a
		// multi-port Service.
		NeedsPort: map[string]bool{probeKey("monitoring", "vmsingle-victoria-metrics"): true},
	})
	defer f.Close()

	m, err := NewDiscoverer(c, time.Minute).Get(t.Context(), 1, "no-spec")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if !m.HasMetrics() {
		t.Fatalf("a portless candidate must still be reachable: %s / %v", m.Summary(), m.Notes)
	}
	if got := m.Metrics.Service.Port; got != "8429" {
		t.Errorf("want the flavor's published port, got %q", got)
	}
}

func TestPortsForPrefersWhatTheObjectDeclared(t *testing.T) {
	t.Parallel()

	declared := &Endpoint{Flavor: FlavorVictoriaMetrics, Ports: []string{"9999"}}
	if got := portsFor(declared); len(got) != 1 || got[0] != "9999" {
		t.Errorf("a declared port wins over the default, got %v", got)
	}

	// Every flavor the discovery filter can produce needs a default, or a
	// portless install of it is unreachable and nothing says why.
	for _, f := range []Flavor{
		FlavorVictoriaMetrics, FlavorPrometheus, FlavorAlertmanager,
		FlavorVMAlert, FlavorThanos, FlavorMimir,
	} {
		if len(portsFor(&Endpoint{Flavor: f})) == 0 {
			t.Errorf("%s has no default port", f)
		}
	}
	// The cap has to clear the longest list, or the last one is never tried.
	for f, ports := range wellKnownPorts {
		if len(ports) > maxPortRetries {
			t.Errorf("%s declares %d ports but only %d are tried", f, len(ports), maxPortRetries)
		}
	}
	if got := portsFor(&Endpoint{Flavor: FlavorUnknown}); len(got) != 0 {
		t.Errorf("an unrecognised flavor has no defaults to offer, got %v", got)
	}
}
