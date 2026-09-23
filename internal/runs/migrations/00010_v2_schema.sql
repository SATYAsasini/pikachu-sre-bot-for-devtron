-- +goose Up
-- v2 replaces the original multi-service schema entirely. The old tables are
-- dropped by name rather than by dropping the schema, because ADK owns
-- sessions / events / app_states / user_states in this same database and
-- migrates them itself.
--
-- Numbered above the v1 migrations so an existing development database
-- applies this one; on a fresh database every DROP is a no-op.
drop table if exists findings         cascade;
drop table if exists run_events       cascade;
drop table if exists runs             cascade;
drop table if exists alert_groups     cascade;
drop table if exists alerts           cascade;
drop table if exists debug_policies   cascade;
drop table if exists jobs             cascade;
drop table if exists artifacts        cascade;
drop table if exists artifact_blobs   cascade;
drop table if exists memories         cascade;
drop table if exists knowledge_documents cascade;
drop table if exists cluster_context_packs cascade;
drop table if exists clusters         cascade;
drop table if exists feedback         cascade;
drop table if exists audit_log        cascade;
drop table if exists api_keys         cascade;
drop table if exists tenants          cascade;

create table runs (
    id              uuid primary key,
    status          text        not null,
    created_at      timestamptz not null default now(),
    started_at      timestamptz,
    finished_at     timestamptz,
    scope           jsonb       not null default '{}'::jsonb,
    trigger         jsonb       not null default '{}'::jsonb,
    intelligence    jsonb,
    verdict         jsonb,
    report          jsonb,
    usage           jsonb       not null default '{}'::jsonb,
    error           text        not null default ''
);

create index runs_created_at_idx on runs (created_at desc);
create index runs_status_idx     on runs (status);
create index runs_cluster_idx    on runs ((scope ->> 'clusterId'));

-- The evidence ledger. Append-only: findings cite these sequence numbers as
-- [ev:N], so a row must never be rewritten.
create table run_events (
    run_id  uuid        not null references runs (id) on delete cascade,
    seq     integer     not null,
    at      timestamptz not null default now(),
    type    text        not null,
    agent   text        not null default '',
    payload jsonb       not null default '{}'::jsonb,
    primary key (run_id, seq)
);

-- +goose Down
drop table if exists run_events;
drop table if exists runs;
