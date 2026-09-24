package worker

import (
	"context"
	"encoding/json"
	"time"

	"github.com/devtron-labs/devtron-sre-agent/internal/incidents"
	"github.com/devtron-labs/devtron-sre-agent/internal/monitoring"
	"github.com/devtron-labs/devtron-sre-agent/internal/rules"
	"github.com/devtron-labs/devtron-sre-agent/internal/runs"
)

// SweepInterval is how often the auto-rules get a chance to act. Alert sources
// evaluate on the order of a minute and an investigation takes minutes, so
// anything faster only produces work that the dedup key then throws away.
const SweepInterval = 2 * time.Minute

// FirstSweepDelay gives cluster discovery time to finish before the first
// pass. Long enough that a restart does not hammer the orchestrator, short
// enough that a restart is not two minutes of nobody watching.
const FirstSweepDelay = 30 * time.Second

// Sweep is what makes an auto-rule mean something.
//
// Until this ran, "investigate these automatically" was a checkbox that
// changed a row in a table and nothing else: rules were evaluated only when a
// person opened the alert list, which is precisely when they do not need an
// agent to notice for them. The sweep asks each cluster with the switch on
// what is firing, applies the same rules the UI applies, and takes on the
// ones marked auto.
//
// Everything expensive is guarded by something cheaper. An alert already
// tracked is a `seen_count` increment, not a run; an alert already being
// investigated is skipped with a log line rather than investigated twice; and
// a cluster whose alert source is unreachable is left alone rather than being
// reported as quiet.
func (w *Worker) StartSweeper(ctx context.Context) {
	if w.Incidents == nil || w.Runs == nil {
		return
	}
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		first := time.NewTimer(FirstSweepDelay)
		defer first.Stop()
		select {
		case <-ctx.Done():
			return
		case <-first.C:
			w.Sweep(ctx)
		}

		t := time.NewTicker(SweepInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				w.Sweep(ctx)
			}
		}
	}()
}

// Sweep runs one pass over every cluster with auto-investigate on.
func (w *Worker) Sweep(ctx context.Context) {
	clusters, err := w.Runs.Store.AutoClusters(ctx)
	if err != nil {
		w.Log.Warn("sweep: cannot list clusters", "err", err)
		return
	}
	for _, c := range clusters {
		select {
		case <-ctx.Done():
			return
		default:
		}
		w.sweepCluster(ctx, c)
	}
}

func (w *Worker) sweepCluster(ctx context.Context, c runs.RuleCluster) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	cfg, err := w.Runs.Store.LoadRules(ctx, c.ID)
	if err != nil || !cfg.AutoEnabled {
		return
	}
	stack, err := w.Discoverer.Get(ctx, c.ID, c.Name)
	if err != nil {
		// Unreachable is not quiet. Say so and try again next tick rather
		// than concluding there is nothing wrong with the cluster.
		w.Log.Warn("sweep: no monitoring", "cluster", c.ID, "err", err)
		return
	}
	alerts, err := monitoring.New(w.Devtron, c.ID, stack).Alerts(ctx, monitoring.AlertFilter{Limit: 200})
	if err != nil {
		w.Log.Warn("sweep: cannot read alerts", "cluster", c.ID, "err", err)
		return
	}

	for _, pick := range autoPicks(cfg, alerts) {
		w.claim(ctx, c, pick.Alert, pick.Priority)
	}
}

// autoPick is one alert the rules claimed, with the urgency they gave it.
type autoPick struct {
	Alert    monitoring.Alert
	Priority rules.Priority
}

// autoPicks is the whole decision the sweep makes, separated from the calls it
// makes afterwards so it can be tested without a cluster.
//
// The batch is deduplicated by the same key the store upserts on. Two alert
// sources reporting one problem — a Prometheus rule and a vmalert copy of it,
// say — arrive as two alerts in one poll, and claiming both would open two
// investigations into the same pod before either had a row to collide with.
func autoPicks(cfg rules.Config, alerts []monitoring.Alert) []autoPick {
	seen := make(map[string]bool, len(alerts))
	out := make([]autoPick, 0, len(alerts))

	for _, a := range alerts {
		d := cfg.Decide(a)
		if !d.Auto {
			continue
		}
		key := incidents.Key(a)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, autoPick{Alert: a, Priority: d.Priority})
	}
	return out
}

// claim tracks one alert and, if nothing is already looking at it, opens an
// investigation.
func (w *Worker) claim(ctx context.Context, c runs.RuleCluster, a monitoring.Alert, p rules.Priority) {
	tracked, created, err := w.Incidents.Track(ctx, c.ID, c.Name, a, p, incidents.OriginRule)
	if err != nil {
		w.Log.Warn("sweep: cannot track", "cluster", c.ID, "alert", a.Name, "err", err)
		return
	}

	// A recurrence of something already answered is not automatically worth
	// another run. The alert is still firing because nobody has fixed it, and
	// re-deriving the same root cause every two minutes is how an agent turns
	// into a bill. Only a newly tracked alert earns one.
	if !created {
		return
	}
	for _, ref := range tracked.Runs {
		if !runs.Terminal(ref.Status) {
			w.Incidents.Log(ctx, tracked.ID, incidents.LogRunSuppressed,
				"already investigating in run "+ref.RunID, "rule")
			return
		}
	}

	alertJSON, _ := json.Marshal(a)
	run, err := w.Runs.Store.Create(ctx, runs.CreateRequest{
		ClusterID:   c.ID,
		ClusterName: c.Name,
		Namespace:   a.Namespace,
		Alert:       alertJSON,
	})
	if err != nil {
		w.Log.Warn("sweep: cannot create run", "cluster", c.ID, "alert", a.Name, "err", err)
		return
	}
	_ = w.Incidents.Attach(ctx, tracked.ID, run.ID)
	w.Incidents.Log(ctx, tracked.ID, incidents.LogRunStarted, "an alert rule opened this investigation", "rule")
	w.Submit(ctx, run.ID)
	w.Log.Info("sweep: investigating", "cluster", c.ID, "alert", a.Name, "run", run.ID, "priority", p)
}
