package runs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a run id does not exist.
var ErrNotFound = errors.New("run not found")

// Store persists runs and their evidence ledger.
type Store struct{ pool *pgxpool.Pool }

// NewStore binds a store to a pool.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Create inserts a queued run and its first ledger entry in one transaction,
// so a run can never exist without the record of why it exists.
func (s *Store) Create(ctx context.Context, req CreateRequest) (*Run, error) {
	kind := "ask"
	if len(req.Alert) > 0 && string(req.Alert) != "null" {
		kind = "alert"
	}
	run := &Run{
		ID:        uuid.NewString(),
		Status:    StatusQueued,
		CreatedAt: time.Now().UTC(),
		Scope: Scope{
			ClusterID: req.ClusterID, ClusterName: req.ClusterName,
			EnvironmentID: req.EnvironmentID, EnvironmentName: req.EnvironmentName,
			Namespace: req.Namespace, AppName: req.AppName, AppType: req.AppType,
		},
		Trigger: Trigger{Kind: kind, Alert: req.Alert, Ask: req.Ask},
		Options: normalizeOptions(req.Options),
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx)) //nolint:errcheck // no-op after commit

	if _, err := tx.Exec(ctx, `
		insert into runs (id, status, created_at, scope, trigger, usage, options)
		values ($1, $2, $3, $4, $5, '{}'::jsonb, $6)`,
		run.ID, run.Status, run.CreatedAt, mustJSON(run.Scope), mustJSON(run.Trigger), mustJSON(run.Options),
	); err != nil {
		return nil, fmt.Errorf("insert run: %w", err)
	}
	if _, _, err := appendTx(ctx, tx, run.ID, EvRunQueued, "", map[string]any{
		"scope": run.Scope, "trigger": kind,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return run, nil
}

const runColumns = `id, status, created_at, started_at, finished_at, scope, trigger,
	intelligence, verdict, report, usage, options, error`

// Get loads one run.
func (s *Store) Get(ctx context.Context, id string) (*Run, error) {
	row := s.pool.QueryRow(ctx, `select `+runColumns+` from runs where id = $1`, id)
	run, err := scanRun(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return run, err
}

// ListFilter narrows a run listing.
type ListFilter struct {
	Status    string
	ClusterID int
	Limit     int
}

// List returns runs newest first.
func (s *Store) List(ctx context.Context, f ListFilter) ([]Run, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		select `+runColumns+` from runs
		where ($1 = '' or status = $1)
		  and ($2 = 0 or (scope ->> 'clusterId')::int = $2)
		order by created_at desc
		limit $3`, f.Status, f.ClusterID, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Run{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// Start marks a run running.
func (s *Store) Start(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx,
		`update runs set status = $2, started_at = coalesce(started_at, now()) where id = $1`,
		id, StatusRunning)
	return err
}

// SetIntelligence stores Devtron's first-pass answer.
func (s *Store) SetIntelligence(ctx context.Context, id string, in *Intelligence) error {
	_, err := s.pool.Exec(ctx, `update runs set intelligence = $2 where id = $1`, id, mustJSON(in))
	return err
}

// SetVerdict stores the judge agent's output.
func (s *Store) SetVerdict(ctx context.Context, id string, v json.RawMessage) error {
	_, err := s.pool.Exec(ctx, `update runs set verdict = $2 where id = $1`, id, jsonOrNil(v))
	return err
}

// SetReport stores the sre agent's output.
func (s *Store) SetReport(ctx context.Context, id string, r json.RawMessage) error {
	_, err := s.pool.Exec(ctx, `update runs set report = $2 where id = $1`, id, jsonOrNil(r))
	return err
}

// Finish closes a run out.
func (s *Store) Finish(ctx context.Context, id, status, errMsg string, usage Usage) error {
	_, err := s.pool.Exec(ctx, `
		update runs set status = $2, finished_at = now(), error = $3, usage = $4
		where id = $1`, id, status, errMsg, mustJSON(usage))
	return err
}

// Cancel marks a run canceled if it has not already finished.
func (s *Store) Cancel(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `
		update runs set status = $2, finished_at = now()
		where id = $1 and status in ($3, $4)`, id, StatusCanceled, StatusQueued, StatusRunning)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("run is not cancelable")
	}
	return nil
}

// Append adds a ledger entry and returns its sequence number and the
// timestamp the database assigned it. The caller must publish that exact
// timestamp: an event delivered live and the same event replayed from the
// ledger have to agree, or the UI shows one entry at two different times.
func (s *Store) Append(ctx context.Context, runID, typ, agent string, payload any) (int, time.Time, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, time.Time{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx)) //nolint:errcheck // no-op after commit
	seq, at, err := appendTx(ctx, tx, runID, typ, agent, payload)
	if err != nil {
		return 0, time.Time{}, err
	}
	return seq, at, tx.Commit(ctx)
}

// appendTx allocates the next sequence inside a transaction. Sequence numbers
// must be gapless per run because findings cite them as [ev:N], which rules
// out a Postgres sequence: those leave holes whenever a transaction rolls
// back.
//
// So the number is read and written in one statement — and that is a
// lost-update waiting to happen. Under READ COMMITTED two concurrent appends
// for the same run both see the same max(seq), both insert it, and the
// second dies on the primary key with SQLSTATE 23505. That is not a rare
// race: the model calls tools in parallel by default, so it fires the first
// time an agent asks two questions in one turn, and the tool whose ledger
// entry lost never runs at all.
//
// An advisory lock keyed on the run serialises allocation for that run only,
// is released with the transaction, and touches nothing else — in
// particular not the runs row, which the worker updates on its own schedule.
func appendTx(ctx context.Context, tx pgx.Tx, runID, typ, agent string, payload any) (int, time.Time, error) {
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext($1))`, runID); err != nil {
		return 0, time.Time{}, fmt.Errorf("lock ledger for %s: %w", typ, err)
	}

	var (
		seq int
		at  time.Time
	)
	err := tx.QueryRow(ctx, `
		insert into run_events (run_id, seq, type, agent, payload)
		values ($1, (select coalesce(max(seq), 0) + 1 from run_events where run_id = $1), $2, $3, $4)
		returning seq, at`, runID, typ, agent, mustJSON(payload)).Scan(&seq, &at)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("append %s event: %w", typ, err)
	}
	return seq, at.UTC(), nil
}

// Events returns ledger entries after a sequence number.
func (s *Store) Events(ctx context.Context, runID string, after, limit int) ([]Event, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	rows, err := s.pool.Query(ctx, `
		select seq, at, type, agent, payload from run_events
		where run_id = $1 and seq > $2 order by seq limit $3`, runID, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.Seq, &e.At, &e.Type, &e.Agent, &e.Payload); err != nil {
			return nil, err
		}
		e.At = e.At.UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}

// Reclaim moves runs that were mid-flight when the process died back to a
// terminal state, so a restart never leaves a run stuck on "running" forever.
func (s *Store) Reclaim(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		update runs set status = $1, finished_at = now(),
		       error = 'the agent restarted while this run was in flight'
		where status in ($2, $3)`, StatusFailed, StatusQueued, StatusRunning)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanRun(row scanner) (*Run, error) {
	var (
		r                       Run
		scope, trig, usage, opt []byte
		intel, verdict, report  []byte
	)
	// Every column in runColumns needs a destination here, in order. It is
	// worth being fussy about: pgx reports a mismatch as "number of field
	// descriptions must equal number of destinations", which does not name
	// the column, and the only symptom was runs sitting in `queued` forever
	// because the worker could not load the row it had just been handed.
	if err := row.Scan(&r.ID, &r.Status, &r.CreatedAt, &r.StartedAt, &r.FinishedAt,
		&scope, &trig, &intel, &verdict, &report, &usage, &opt, &r.Error); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(scope, &r.Scope)
	_ = json.Unmarshal(trig, &r.Trigger)
	_ = json.Unmarshal(usage, &r.Usage)
	if len(opt) > 0 && string(opt) != "null" {
		_ = json.Unmarshal(opt, &r.Options)
	}
	if len(intel) > 0 && string(intel) != "null" {
		r.Intelligence = &Intelligence{}
		_ = json.Unmarshal(intel, r.Intelligence)
	}
	r.Verdict = jsonOrNil(verdict)
	r.Report = jsonOrNil(report)
	r.DurationMs = durationOf(&r)
	return &r, nil
}

func durationOf(r *Run) int64 {
	if r.StartedAt == nil {
		return 0
	}
	end := time.Now()
	if r.FinishedAt != nil {
		end = *r.FinishedAt
	}
	return end.Sub(*r.StartedAt).Milliseconds()
}

// normalizeOptions fills in the defaults so downstream code never has to
// distinguish "unset" from "auto".
func normalizeOptions(o RunOptions) RunOptions {
	if o.Depth == "" {
		o.Depth = DepthAuto
	}
	if o.Metrics == "" {
		o.Metrics = SourceAuto
	}
	if o.Logs == "" {
		o.Logs = SourceAuto
	}
	return o
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{}`)
	}
	return b
}

// jsonOrNil normalises SQL NULL and the literal "null" to a nil RawMessage,
// so the API serves `null` rather than the string "null".
func jsonOrNil(b []byte) json.RawMessage {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	return json.RawMessage(b)
}
