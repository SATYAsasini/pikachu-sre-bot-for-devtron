-- +goose Up
-- alert_runs.run_id was text; runs.id is uuid.
--
-- Postgres has no implicit cast between them, so every query joining the two
-- failed at runtime with "operator does not exist: uuid = text". Both callers
-- swallowed the error — the dashboard's finding lookup behind `if err == nil`,
-- the per-alert run list behind a discarded error — so the failure was
-- completely silent: every alert read "Not investigated yet" no matter how
-- many investigations had succeeded against it, and the Investigations panel
-- was always empty. The one query that worked, ByRun, is the one that does
-- not join.
--
-- The values were always uuids; only the declared type was wrong.
alter table alert_runs alter column run_id type uuid using run_id::uuid;

-- +goose Down
alter table alert_runs alter column run_id type text using run_id::text;
