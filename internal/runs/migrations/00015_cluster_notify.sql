-- +goose Up
-- Where a cluster's findings go. Same row as its rules: both are "how this
-- cluster is configured", and splitting them would mean two round trips to
-- render one settings page.
alter table cluster_rules add column if not exists notify jsonb not null default '{}'::jsonb;

-- +goose Down
alter table cluster_rules drop column if exists notify;
