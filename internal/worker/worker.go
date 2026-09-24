package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/devtron-labs/devtron-sre-agent/internal/agents"
	"github.com/devtron-labs/devtron-sre-agent/internal/capability"
	"github.com/devtron-labs/devtron-sre-agent/internal/devtron"
	"github.com/devtron-labs/devtron-sre-agent/internal/incidents"
	"github.com/devtron-labs/devtron-sre-agent/internal/knowledge"
	"github.com/devtron-labs/devtron-sre-agent/internal/monitoring"
	"github.com/devtron-labs/devtron-sre-agent/internal/rules"
	"github.com/devtron-labs/devtron-sre-agent/internal/runs"
	"github.com/devtron-labs/devtron-sre-agent/internal/tools"
)

// Worker runs investigations. It lives in the same process as the API, so
// there is no queue table and no lease: a submitted run goes onto a channel
// and a pool goroutine picks it up. A restart loses in-flight runs, which
// Store.Reclaim turns into an honest terminal state rather than a run that
// appears to hang forever.
type Worker struct {
	Runs       *runs.Service
	Devtron    *devtron.Client
	Discoverer *devtron.Discoverer
	Knowledge  *knowledge.Catalog
	Caps       *capability.Service
	// Incidents links a finished investigation back to the alert it was about.
	Incidents *incidents.Store
	Registry  *tools.Registry
	Pipeline  *agents.Pipeline
	Redact    func(string) string
	Log       *slog.Logger

	// PublicURL is where this deployment is reachable, for links in
	// notifications. Empty simply omits the link.
	PublicURL string

	Concurrency         int
	IntelligenceTimeout time.Duration
	RunTimeout          time.Duration

	queue   chan string
	cancels sync.Map // runID -> context.CancelFunc
	started sync.Once
	wg      sync.WaitGroup
}

// Start launches the pool. It returns immediately; Stop waits for drain.
func (w *Worker) Start(ctx context.Context) {
	w.started.Do(func() {
		if w.Concurrency < 1 {
			w.Concurrency = 1
		}
		w.queue = make(chan string, 256)
		for i := 0; i < w.Concurrency; i++ {
			w.wg.Add(1)
			go func() {
				defer w.wg.Done()
				for {
					select {
					case <-ctx.Done():
						return
					case id, ok := <-w.queue:
						if !ok {
							return
						}
						w.execute(ctx, id)
					}
				}
			}()
		}
	})
}

// Stop drains the pool.
func (w *Worker) Stop() {
	if w.queue != nil {
		close(w.queue)
	}
	w.wg.Wait()
}

// Submit queues a run. It never blocks the caller: a full queue fails the run
// immediately rather than stalling the HTTP handler that created it.
func (w *Worker) Submit(ctx context.Context, runID string) {
	select {
	case w.queue <- runID:
	default:
		w.Log.Error("run queue is full", "run", runID)
		_ = w.Runs.Finish(ctx, runID, runs.StatusFailed,
			"the agent is at capacity; try again shortly", runs.Usage{})
	}
}

// Cancel stops an in-flight run.
func (w *Worker) Cancel(runID string) bool {
	if v, ok := w.cancels.Load(runID); ok {
		v.(context.CancelFunc)()
		return true
	}
	return false
}

func (w *Worker) execute(parent context.Context, runID string) {
	timeout := w.RunTimeout
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	w.cancels.Store(runID, context.CancelFunc(cancel))
	defer w.cancels.Delete(runID)

	log := w.Log.With("run", runID)
	ledger := w.Runs.Ledger(runID)

	run, err := w.Runs.Store.Get(ctx, runID)
	if err != nil {
		log.Error("cannot load run", "err", err)
		return
	}
	if err := w.Runs.Store.Start(ctx, runID); err != nil {
		log.Error("cannot start run", "err", err)
		return
	}
	_, _ = ledger(ctx, runs.EvStatus, "", map[string]any{"status": runs.StatusRunning})

	usage := runs.Usage{}
	fail := func(msg string, err error) {
		log.Error(msg, "err", err)
		_, _ = ledger(context.WithoutCancel(ctx), runs.EvError, "", map[string]any{"error": msg + ": " + errText(err)})
		_ = w.Runs.Finish(context.WithoutCancel(ctx), runID, runs.StatusFailed, msg+": "+errText(err), usage)
	}

	// 0. Refuse early if the cluster cannot answer. Without this the run
	//    spends its Devtron call and both agents discovering the same thing
	//    slowly, and concludes nothing anyone can act on.
	if w.Caps != nil {
		if c := w.Caps.Get(run.Scope.ClusterID); c != nil && !c.Reach.Investigable() {
			msg := "cluster " + run.Scope.ClusterName + " cannot be read: " + c.Reach.Why()
			_, _ = ledger(ctx, runs.EvError, "preflight", map[string]any{
				"error": msg, "reach": c.Reach, "detail": c.Detail,
			})
			_ = w.Runs.Finish(ctx, runID, runs.StatusFailed, msg, usage)
			log.Warn("refused run on unreachable cluster", "cluster", run.Scope.ClusterName, "reach", c.Reach)
			return
		}
	}

	// 1. Discover what this cluster actually runs for metrics and alerts.
	stack, err := w.Discoverer.Get(ctx, run.Scope.ClusterID, run.Scope.ClusterName)
	if err != nil {
		fail("monitoring discovery failed", err)
		return
	}
	_, _ = ledger(ctx, runs.EvStatus, "preflight", map[string]any{"monitoring": stack.Summary(), "notes": stack.Notes})

	mon := monitoring.New(w.Devtron, run.Scope.ClusterID, stack)
	deps := &tools.Deps{
		Devtron:    w.Devtron,
		Monitoring: mon,
		Knowledge:  w.Knowledge,
		Cluster: tools.ClusterInfo{
			ID: run.Scope.ClusterID, Name: run.Scope.ClusterName,
			EnvID: run.Scope.EnvironmentID, Namespace: run.Scope.Namespace,
			AppName: run.Scope.AppName,
		},
		Log:    log,
		Cache:  tools.NewCache(10 * time.Minute),
		Redact: w.Redact,
	}

	var alert *monitoring.Alert
	if len(run.Trigger.Alert) > 0 {
		var a monitoring.Alert
		if err := json.Unmarshal(run.Trigger.Alert, &a); err == nil && a.Name != "" {
			alert = &a
		}
	}

	// 2. Deterministic facts, before any model call.
	facts := w.collectFacts(ctx, run, deps, alert, ledger)

	// 3. Devtron's own first pass. A failure here is survivable: the agents
	//    still have the fact pack, and the report says the first pass errored.
	intel := w.runIntelligence(ctx, run, alert, ledger, log)
	if err := w.Runs.Store.SetIntelligence(ctx, runID, intel); err != nil {
		log.Warn("could not store intelligence", "err", err)
	}

	// 4. The two agents.
	out, err := w.Pipeline.Run(ctx, agents.Input{
		RunID:        runID,
		UserID:       run.Scope.ClusterName,
		Alert:        rawOrNil(run.Trigger.Alert),
		Scope:        run.Scope,
		Facts:        facts,
		Intelligence: intelligenceText(intel),
	}, deps, agents.LedgerFunc(ledger))
	usage = out.Usage

	if out.Verdict != nil {
		if err := w.Runs.Store.SetVerdict(ctx, runID, out.Verdict); err != nil {
			log.Warn("could not store verdict", "err", err)
		}
	}
	if out.Report != nil {
		if err := w.Runs.Store.SetReport(ctx, runID, out.Report); err != nil {
			log.Warn("could not store report", "err", err)
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			_ = w.Runs.Finish(context.WithoutCancel(ctx), runID, runs.StatusCanceled, "the run was canceled", usage)
			return
		}
		fail("the investigation failed", err)
		return
	}

	msg := ""
	if out.Status == runs.StatusBudgetExceeded {
		msg = "the run stopped at its budget; the report may be incomplete"
	}
	// Devtron answered and we did not. That is not a failed run — a complete
	// 42-second first pass is real work, and burying it under a red banner
	// that says nothing was learned throws it away. It is not a success
	// either: nobody verified it.
	if out.Status == runs.StatusFailed && out.Report == nil && intel != nil && intel.Analysis != "" {
		out.Status = runs.StatusPartial
		out.Report = unverifiedReport(intel.Analysis)
		msg = "Devtron's analysis is below, unverified — our agent could not run: " + errText(err)
		if err := w.Runs.Store.SetReport(ctx, runID, out.Report); err != nil {
			log.Warn("could not store the unverified report", "err", err)
		}
		err = nil
	}
	if out.Status == runs.StatusFailed && out.Report == nil {
		msg = "the agents produced no usable report"
	}
	// Record the conclusion against the alert, so the dashboard shows what
	// this turned out to be without anyone opening the run.
	w.closeTheLoop(ctx, runID, out, log)

	// Tell the channel what we found. After Finish rather than before, so a
	// notification never describes a run that then failed to record.
	w.notifyFinding(ctx, run, alert, out, log)

	if err := w.Runs.Finish(ctx, runID, out.Status, msg, usage); err != nil {
		log.Error("could not finish run", "err", err)
	}
	log.Info("run finished", "status", out.Status, "toolCalls", usage.ToolCalls, "tokens", usage.ModelTokens)
}

// runIntelligence streams Devtron's one-shot debugger into the ledger so the
// UI shows progress while it thinks, which can take a minute or more.
func (w *Worker) runIntelligence(ctx context.Context, run *runs.Run, alert *monitoring.Alert, ledger runs.LedgerFunc, log *slog.Logger) *runs.Intelligence {
	timeout := w.IntelligenceTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ictx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ask := askFor(run, alert)
	_, _ = ledger(ctx, runs.EvIntelligenceStart, "intelligence", map[string]any{"ask": ask})

	res, err := w.Devtron.Intelligence(ictx, devtron.IntelligenceRequest{
		Ask:     ask,
		Context: intelligenceContext(run, alert),
	}, func(ev devtron.IntelligenceEvent) {
		if ev.Type == devtron.EventThinking && ev.Content != "" {
			_, _ = ledger(ctx, runs.EvIntelligenceThinking, "intelligence", map[string]any{"content": ev.Content})
		}
	})

	out := &runs.Intelligence{}
	if res != nil {
		out.RequestID = res.RequestID
		out.Analysis = res.Analysis
		out.ThinkingCount = len(res.Thinking)
		out.Thinking = keepThinking(res.Thinking)
		out.DurationMs = res.Duration.Milliseconds()
		out.Failed = res.Failed
	}
	if err != nil {
		if out.Failed == "" {
			out.Failed = errText(err)
		}
		log.Warn("devtron intelligence did not answer", "err", err, "requestId", out.RequestID)
	}
	_, _ = ledger(ctx, runs.EvIntelligenceAnalysis, "intelligence", map[string]any{
		"requestId": out.RequestID, "analysis": out.Analysis,
		"failed": out.Failed, "durationMs": out.DurationMs, "thinkingCount": out.ThinkingCount,
	})
	return out
}

// keepThinking trims the narrated steps to what is worth carrying.
//
// The first steps are where Devtron says what it is about to inspect, which
// is the part our agents can act on; the tail is mostly narration of a
// conclusion already reached. Each line is clipped too, because a step that
// has quoted a whole manifest into itself is not a step any more.
func keepThinking(steps []string) []string {
	const maxLine = 240
	out := make([]string, 0, runs.ThinkingKept)
	seen := make(map[string]bool, runs.ThinkingKept)
	for _, s := range steps {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		// Clipped by rune, not by byte: a step in Japanese or one carrying an
		// emoji would otherwise be cut mid-character and reach the model as
		// invalid UTF-8.
		if r := []rune(s); len(r) > maxLine {
			s = string(r[:maxLine]) + "…"
		}
		out = append(out, s)
		if len(out) == runs.ThinkingKept {
			break
		}
	}
	return out
}

// intelligenceText is what the agents actually read. When the first pass
// failed, say so in the text: a blank section would read as "it found
// nothing", which is a different and much weaker claim.
//
// The narrated steps are appended under the analysis. They are the cheapest
// evidence in the run — Devtron already paid to gather them — and handing
// them over is what stops our agents re-reading the same objects.
func intelligenceText(in *runs.Intelligence) string {
	if in == nil {
		return "(Devtron Intelligence was not called for this run.)"
	}
	body := in.Analysis
	if body == "" {
		if in.Failed != "" {
			body = "(Devtron Intelligence returned no analysis. It failed with: " + in.Failed +
				"\nThere is no first-pass analysis to verify. Work from the deterministic facts alone and say so in your output.)"
		} else {
			body = "(Devtron Intelligence returned an empty analysis.)"
		}
	}
	if len(in.Thinking) == 0 {
		return body
	}
	var b strings.Builder
	b.WriteString(body)
	b.WriteString("\n\n### Steps Devtron took (")
	b.WriteString(strconv.Itoa(in.ThinkingCount))
	b.WriteString(" in total, first ")
	b.WriteString(strconv.Itoa(len(in.Thinking)))
	b.WriteString(" shown)\n\n")
	b.WriteString("These are what the first pass actually inspected, in order. Treat them as evidence " +
		"of what has already been looked at — cite them as `devtron_step` — and do not spend a tool " +
		"call repeating one unless you have reason to think it got the wrong answer.\n")
	for i, s := range in.Thinking {
		b.WriteString("\n")
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(". ")
		b.WriteString(s)
	}
	return b.String()
}

func rawOrNil(b json.RawMessage) any {
	if len(b) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil
	}
	return v
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// notifyFinding posts the conclusion of a run to the cluster's channel.
//
// This is the only notification the product sends. Every monitoring tool can
// already say something broke; what none of them say is "we looked into it,
// here is the cause and the first thing to do" — arriving while somebody is
// still reading the alert.
//
// It is best-effort by design. A webhook that is down must never fail a run
// or block the next one: the finding is already saved, and the channel is a
// convenience on top of it.
func (w *Worker) notifyFinding(ctx context.Context, run *runs.Run, alert *monitoring.Alert, out agents.Output, log *slog.Logger) {
	if out.Report == nil {
		return
	}
	cfg, err := w.Runs.Store.LoadRules(ctx, run.Scope.ClusterID)
	if err != nil {
		return
	}

	// Priority comes from the same rules that decide the alert list, so what
	// arrives in Slack matches what is on screen.
	priority := rules.DefaultPriority
	if alert != nil {
		priority = cfg.Decide(*alert).Priority
	}
	if !cfg.Notify.Wants(priority) {
		return
	}

	var rep struct {
		Agrees             bool   `json:"agrees"`
		CorrectedRootCause string `json:"correctedRootCause"`
		SreNotes           string `json:"sreNotes"`
		Remediation        []struct {
			Action string `json:"action"`
			Risk   string `json:"risk"`
		} `json:"remediation"`
	}
	if json.Unmarshal(out.Report, &rep) != nil {
		return
	}

	subject := "a question"
	if alert != nil {
		subject = alert.Name
		if alert.Resource != "" {
			subject += " on " + alert.Resource
		}
	}

	cause := strings.TrimSpace(rep.CorrectedRootCause)
	if cause == "" {
		cause = strings.TrimSpace(rep.SreNotes)
	}
	if cause == "" {
		// Nothing worth saying. Posting "investigated, no conclusion" into a
		// channel is how a channel gets muted.
		return
	}

	body := cause
	if len(rep.Remediation) > 0 {
		body += "\n\nDo first: " + rep.Remediation[0].Action
		if r := rep.Remediation[0].Risk; r != "" {
			body += " (risk: " + r + ")"
		}
	}

	title := "[" + string(priority) + "] " + subject
	if !rep.Agrees {
		title += " — the first pass was wrong"
	}

	if err := rules.Send(ctx, cfg.Notify, rules.Message{
		Title:    title,
		Body:     body,
		Priority: priority,
		Link:     runLink(w.PublicURL, run.ID),
	}, nil); err != nil {
		log.Warn("could not notify", "channel", cfg.Notify.Channel, "err", err)
	}
}

// runLink points at the run, when the deployment knows its own address.
func runLink(base, id string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return ""
	}
	return base + "/runs/" + id
}

// unverifiedReport carries Devtron's analysis through when our agent could not
// run at all.
//
// It is explicitly marked unverified and carries no remediation and no
// confidence. Presenting somebody else's unchecked conclusion as our finding
// would be exactly the failure this product was built to catch, so it says
// what it is and stops there.
func unverifiedReport(analysis string) json.RawMessage {
	body, err := json.Marshal(map[string]any{
		"agrees":             true,
		"correctedRootCause": "",
		"confidence":         0,
		"evidence":           []any{},
		"remediation":        []any{},
		"unknowns": []string{
			"Nothing here has been checked. Our agent did not run, so no claim in this analysis was graded against the cluster.",
		},
		"sreNotes":   "Devtron Intelligence answered, but our verification never ran. Treat everything below as a first pass, not a finding.\n\n" + analysis,
		"unverified": true,
	})
	if err != nil {
		return nil
	}
	return body
}

// closeTheLoop records a finished investigation against the alert it was about.
//
// An alert is the entity here; a run is something that happened to one. The
// dashboard reads the finding off the alert, so a run that concludes without
// writing back leaves the alert looking uninvestigated — which is how somebody
// ends up debugging the same thing twice.
func (w *Worker) closeTheLoop(ctx context.Context, runID string, out agents.Output, log *slog.Logger) {
	if w.Incidents == nil {
		return
	}
	alertID, ok := w.Incidents.ByRun(ctx, runID)
	if !ok {
		// A free-text run, or one started before alerts were tracked. Nothing
		// to attach it to, and that is fine.
		return
	}

	detail := "no usable report"
	if out.Report != nil {
		detail = "report written"
	}
	if out.Status == runs.StatusPartial {
		detail = "Devtron answered; unverified"
	}
	w.Incidents.Log(ctx, alertID, incidents.LogRunFinished, detail, "")
	log.Debug("attached finding to alert", "alert", alertID, "run", runID)
}
