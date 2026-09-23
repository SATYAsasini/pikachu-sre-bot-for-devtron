-- +goose Up
-- Per-run operator choices: how deep to go, and which data sources the agent
-- may read. Stored with the run so a repeat is reproducible and the UI can
-- show what was asked for, not just what happened.
alter table runs add column if not exists options jsonb not null default '{}'::jsonb;

-- +goose Down
alter table runs drop column if exists options;
