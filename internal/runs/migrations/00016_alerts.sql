-- +goose Up
-- Alerts we have taken responsibility for.
--
-- The live feed from Alertmanager is not this. That is a question we ask the
-- cluster and forget; these are entities with a lifecycle, notes, a priority
-- and the investigations that were run against them. An alert becomes one of
-- these when somebody debugs it, or when an auto-rule claims it.
-- Numbers come from here rather than from a `bigserial`, because a serial's
-- default is evaluated before the conflict is detected: every poll of an
-- alert that already exists would burn a number, and the fifth incident on a
-- cluster would be "#48291". The insert asks for a number only when it is
-- actually inserting.
create sequence if not exists alerts_seq;

create table if not exists alerts (
    id           text        primary key,
    -- The handle people actually use. "#42 is still open" is a sentence; a
    -- uuid is not, and an incident that cannot be said out loud in a channel
    -- does not get discussed.
    seq          bigint      not null unique,
    cluster_id   int         not null,
    cluster_name text        not null default '',

    -- Versioned so the hashing rule can change without orphaning every row
    -- that already exists. Format: "auto:1:<sha256 of name|namespace|resource>".
    dedup_key    text        not null,

    -- A snapshot of the payload as it was when we took it on. The live alert
    -- may change or vanish; what we investigated must not.
    name         text        not null,
    severity     text        not null default '',
    namespace    text        not null default '',
    kind         text        not null default '',
    resource     text        not null default '',
    summary      text        not null default '',
    labels       jsonb       not null default '{}'::jsonb,
    payload      jsonb       not null default '{}'::jsonb,

    priority     text        not null default 'P2',
    -- firing | acknowledged | resolved
    state        text        not null default 'firing',
    -- manual | rule
    origin       text        not null default 'manual',

    first_seen   timestamptz not null default now(),
    last_seen    timestamptz not null default now(),
    seen_count   int         not null default 1,

    acked_by     text        not null default '',
    acked_at     timestamptz,
    resolved_at  timestamptz,
    notes        text        not null default '',

    updated_at   timestamptz not null default now()
);

-- One row per alert per cluster, however many times it fires. This is the
-- whole point: 23 days of the same alert is one entity with a count, not
-- thousands of rows.
create unique index if not exists alerts_dedup on alerts (cluster_id, dedup_key);
create index if not exists alerts_state on alerts (cluster_id, state, priority);

-- Which investigations were run against an alert. Several, over time: a
-- recurrence is worth looking at again, and the old finding is still worth
-- keeping.
create table if not exists alert_runs (
    alert_id   text        not null references alerts(id) on delete cascade,
    run_id     text        not null,
    created_at timestamptz not null default now(),
    primary key (alert_id, run_id)
);

-- Append-only. Includes the non-events — "we did not notify because this was
-- a duplicate" is the question people actually ask.
create table if not exists alert_log (
    id       bigserial   primary key,
    alert_id text        not null references alerts(id) on delete cascade,
    at       timestamptz not null default now(),
    kind     text        not null,
    detail   text        not null default '',
    actor    text        not null default ''
);

create index if not exists alert_log_alert on alert_log (alert_id, id);

-- +goose Down
drop table if exists alert_log;
drop table if exists alert_runs;
drop table if exists alerts;
drop sequence if exists alerts_seq;
