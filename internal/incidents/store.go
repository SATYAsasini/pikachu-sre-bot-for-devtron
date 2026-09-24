package incidents

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devtron-labs/devtron-sre-agent/internal/monitoring"
	"github.com/devtron-labs/devtron-sre-agent/internal/rules"
)

// Store persists tracked alerts, their runs and their log.
type Store struct{ pool *pgxpool.Pool }

// NewStore binds a store to a pool.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

const cols = `id, seq, cluster_id, cluster_name, dedup_key, name, severity, namespace, kind, resource,
	summary, labels, payload, priority, state, origin, first_seen, last_seen, seen_count,
	acked_by, acked_at, resolved_at, notes, updated_at`

// Track takes responsibility for a live alert, or records another sighting of
// one already tracked.
//
// The upsert on `dedup_key` is the whole feature. Without it, an alert firing
// for 23 days produces a row per poll and the dashboard is unusable within an
// hour; with it, that is one entity with `seenCount: 6048`. A resolved alert
// that fires again reopens rather than duplicating, because it is the same
// problem coming back and the previous investigation is worth keeping beside
// the new one.
func (s *Store) Track(ctx context.Context, clusterID int, clusterName string, a monitoring.Alert, p rules.Priority, origin Origin) (*Alert, bool, error) {
	key := Key(a)
	labels, _ := json.Marshal(orEmptyLabels(a.Labels))
	payload, _ := json.Marshal(a)

	// Returns whether this insert created the row, so the caller can tell a
	// new problem from a recurrence without a second query.
	var created bool
	var id, prior string
	// The prior state comes from a CTE rather than from RETURNING, because
	// RETURNING hands back the row after the upsert has already flipped a
	// resolved alert back to firing. Without it, an alert that was closed and
	// came back — the single most interesting thing an alert can do — logged
	// as an ordinary sighting.
	err := s.pool.QueryRow(ctx, `
		with prior as (select state, seq from alerts where cluster_id = $2 and dedup_key = $4)
		insert into alerts (id, seq, cluster_id, cluster_name, dedup_key, name, severity, namespace, kind,
			resource, summary, labels, payload, priority, state, origin)
		-- coalesce short-circuits, so nextval is only called when there is no
		-- prior row. An alert that has been firing for 23 days keeps the
		-- number it was given on day one.
		values ($1, coalesce((select seq from prior), nextval('alerts_seq')),
			$2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, 'firing', $14)
		on conflict (cluster_id, dedup_key) do update set
			last_seen  = now(),
			seen_count = alerts.seen_count + 1,
			severity   = excluded.severity,
			summary    = excluded.summary,
			labels     = excluded.labels,
			payload    = excluded.payload,
			priority   = excluded.priority,
			-- A resolved alert firing again is the same problem returning.
			state      = case when alerts.state = 'resolved' then 'firing' else alerts.state end,
			resolved_at = case when alerts.state = 'resolved' then null else alerts.resolved_at end,
			updated_at = now()
		returning id, (xmax = 0), coalesce((select state from prior), '')`,
		uuid.NewString(), clusterID, clusterName, key, a.Name, a.Severity, a.Namespace, a.Kind,
		a.Resource, a.Summary, labels, payload, string(p), string(origin)).
		Scan(&id, &created, &prior)
	if err != nil {
		return nil, false, err
	}

	switch {
	case created:
		s.Log(ctx, id, LogTracked, "now tracked as an incident", string(origin))
	case prior == string(StateResolved):
		s.Log(ctx, id, LogReopened, "resolved, and firing again", string(origin))
	default:
		s.Log(ctx, id, LogSeen, "fired again", "")
	}
	got, err := s.Get(ctx, id)
	return got, created, err
}

// Get reads one alert with its runs and log-derived finding.
func (s *Store) Get(ctx context.Context, id string) (*Alert, error) {
	row := s.pool.QueryRow(ctx, `select `+cols+` from alerts where id = $1`, id)
	a, err := scanAlert(row)
	if err != nil {
		return nil, err
	}
	a.Runs, _ = s.runsFor(ctx, id)
	return a, nil
}

// Filter narrows a listing.
type Filter struct {
	ClusterID int
	// State empty means every open state; "all" includes resolved.
	State string
	Limit int
}

// List returns tracked alerts, most urgent and most recent first.
func (s *Store) List(ctx context.Context, f Filter) ([]Alert, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 200
	}
	where := []string{"($1 = 0 or cluster_id = $1)"}
	switch f.State {
	case "", "open":
		where = append(where, "state <> 'resolved'")
	case "all":
	default:
		where = append(where, "state = '"+strings.ReplaceAll(f.State, "'", "")+"'")
	}

	rows, err := s.pool.Query(ctx, `
		select `+cols+` from alerts
		where `+strings.Join(where, " and ")+`
		order by
			case priority when 'P0' then 0 when 'P1' then 1 else 2 end,
			case state when 'firing' then 0 when 'acknowledged' then 1 else 2 end,
			last_seen desc
		limit $2`, f.ClusterID, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Alert{}
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// SetState moves an alert through its life and records who did it.
func (s *Store) SetState(ctx context.Context, id string, st State, actor string) error {
	q, args := stateUpdate(id, st, actor)
	if _, err := s.pool.Exec(ctx, q, args...); err != nil {
		return err
	}

	kind := LogReopened
	switch st {
	case StateAcknowledged:
		kind = LogAcked
	case StateResolved:
		kind = LogResolved
	}
	s.Log(ctx, id, kind, "", actor)
	return nil
}

// stateUpdate builds the statement for one transition, with exactly the
// arguments it uses.
//
// The arguments travel with the query rather than being passed alongside it,
// because they did not: every branch was given `id, actor` while only the
// acknowledge branch had a `$2`, so resolving an alert failed with "mismatched
// param and argument count" and the row simply never changed. Nothing in the
// UI reported it — the PATCH returned the unmodified alert, which reads
// exactly like a resolve that did not stick.
func stateUpdate(id string, st State, actor string) (string, []any) {
	switch st {
	case StateAcknowledged:
		return `update alerts set state = 'acknowledged', acked_by = $2, acked_at = now(), updated_at = now()
			where id = $1`, []any{id, actor}
	case StateResolved:
		return `update alerts set state = 'resolved', resolved_at = now(), updated_at = now()
			where id = $1`, []any{id}
	default:
		// Reopening clears the resolution but keeps who acknowledged it: the
		// same person is usually still the one holding it.
		return `update alerts set state = 'firing', resolved_at = null, updated_at = now()
			where id = $1`, []any{id}
	}
}

// SetNotes replaces the free-text notes on an alert.
func (s *Store) SetNotes(ctx context.Context, id, notes, actor string) error {
	if _, err := s.pool.Exec(ctx,
		`update alerts set notes = $2, updated_at = now() where id = $1`, id, notes); err != nil {
		return err
	}
	s.Log(ctx, id, LogNoted, clip(notes, 120), actor)
	return nil
}

// Attach links an investigation to an alert.
func (s *Store) Attach(ctx context.Context, alertID, runID string) error {
	_, err := s.pool.Exec(ctx,
		`insert into alert_runs (alert_id, run_id) values ($1, $2) on conflict do nothing`, alertID, runID)
	if err == nil {
		s.Log(ctx, alertID, LogRunStarted, runID, "")
	}
	return err
}

// ByRun finds the alert an investigation belongs to, if any.
func (s *Store) ByRun(ctx context.Context, runID string) (string, bool) {
	var id string
	err := s.pool.QueryRow(ctx, `select alert_id from alert_runs where run_id = $1`, runID).Scan(&id)
	return id, err == nil
}

// Log appends one entry. Best-effort: a missing log line must never fail the
// thing it was describing.
func (s *Store) Log(ctx context.Context, alertID, kind, detail, actor string) {
	_, _ = s.pool.Exec(ctx,
		`insert into alert_log (alert_id, kind, detail, actor) values ($1, $2, $3, $4)`,
		alertID, kind, detail, actor)
}

// Timeline reads an alert's log, oldest first.
func (s *Store) Timeline(ctx context.Context, alertID string) ([]LogEntry, error) {
	rows, err := s.pool.Query(ctx,
		`select id, at, kind, detail, actor from alert_log where alert_id = $1 order by id`, alertID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []LogEntry{}
	for rows.Next() {
		var e LogEntry
		if err := rows.Scan(&e.ID, &e.At, &e.Kind, &e.Detail, &e.Actor); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) runsFor(ctx context.Context, alertID string) ([]RunRef, error) {
	rows, err := s.pool.Query(ctx, `
		select ar.run_id, coalesce(r.status, 'unknown'), ar.created_at
		from alert_runs ar left join runs r on r.id = ar.run_id
		where ar.alert_id = $1 order by ar.created_at desc`, alertID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []RunRef{}
	for rows.Next() {
		var r RunRef
		if err := rows.Scan(&r.RunID, &r.Status, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanAlert(row scanner) (*Alert, error) {
	var a Alert
	var labels, payload []byte
	var priority, state, origin string

	if err := row.Scan(&a.ID, &a.Seq, &a.ClusterID, &a.ClusterName, &a.DedupKey, &a.Name, &a.Severity,
		&a.Namespace, &a.Kind, &a.Resource, &a.Summary, &labels, &payload, &priority, &state,
		&origin, &a.FirstSeen, &a.LastSeen, &a.SeenCount, &a.AckedBy, &a.AckedAt, &a.ResolvedAt,
		&a.Notes, &a.UpdatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(labels, &a.Labels)
	a.Payload = payload
	a.Priority = rules.Priority(priority)
	a.State = State(state)
	a.Origin = Origin(origin)
	return &a, nil
}

func orEmptyLabels(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func clip(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// LatestFindings returns the most recent conclusion per alert.
//
// Read in one query for a whole page rather than per row: the dashboard's
// entire purpose is seeing what each alert turned out to be without opening
// anything, and doing that N+1 times makes the page slower the more useful it
// gets.
func (s *Store) LatestFindings(ctx context.Context, alertIDs []string) (map[string]Finding, error) {
	out := map[string]Finding{}
	if len(alertIDs) == 0 {
		return out, nil
	}

	rows, err := s.pool.Query(ctx, `
		select distinct on (ar.alert_id)
			ar.alert_id, r.id, r.status, r.report, r.finished_at, r.created_at
		from alert_runs ar
		join runs r on r.id = ar.run_id
		where ar.alert_id = any($1) and r.report is not null
		order by ar.alert_id, r.created_at desc`, alertIDs)
	if err != nil {
		return out, err
	}
	defer rows.Close()

	for rows.Next() {
		var alertID, runID, status string
		var report []byte
		var finished *time.Time
		var created time.Time
		if err := rows.Scan(&alertID, &runID, &status, &report, &finished, &created); err != nil {
			return out, err
		}
		f := findingFrom(report)
		f.RunID = runID
		f.Unverified = f.Unverified || status == "partial"
		f.At = created
		if finished != nil {
			f.At = *finished
		}
		out[alertID] = f
	}
	return out, rows.Err()
}

// findingFrom flattens a report into the one line a dashboard row needs.
func findingFrom(report []byte) Finding {
	var rep struct {
		Agrees             bool    `json:"agrees"`
		CorrectedRootCause string  `json:"correctedRootCause"`
		SreNotes           string  `json:"sreNotes"`
		Confidence         float64 `json:"confidence"`
		Unverified         bool    `json:"unverified"`
		Remediation        []struct {
			Action string `json:"action"`
			Risk   string `json:"risk"`
		} `json:"remediation"`
	}
	if json.Unmarshal(report, &rep) != nil {
		return Finding{}
	}

	f := Finding{
		Agrees:     rep.Agrees,
		Confidence: rep.Confidence,
		Unverified: rep.Unverified,
		RootCause:  strings.TrimSpace(rep.CorrectedRootCause),
	}
	if f.RootCause == "" {
		// A run that agreed has no correction to make. Its notes are the
		// finding, and showing nothing would make a successful run look empty.
		f.RootCause = clip(strings.TrimSpace(rep.SreNotes), 400)
	}
	if len(rep.Remediation) > 0 {
		f.Action = rep.Remediation[0].Action
		f.Risk = rep.Remediation[0].Risk
	}
	return f
}
