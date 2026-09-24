-- +goose Up
-- Per-cluster alert rules: which alerts are worth showing, which are worth
-- investigating unprompted, and how urgent each one is.
--
-- One row per cluster rather than one per rule. The lists are ordered and
-- edited as a whole — priority is first-match-wins, so the order *is* the
-- meaning — and a table of rows would need a position column that only ever
-- gets rewritten in full anyway.
create table if not exists cluster_rules (
    cluster_id   int primary key,
    cluster_name text        not null default '',
    show         jsonb       not null default '[]'::jsonb,
    mute         jsonb       not null default '[]'::jsonb,
    auto         jsonb       not null default '[]'::jsonb,
    auto_enabled boolean     not null default false,
    priority     jsonb       not null default '[]'::jsonb,
    updated_at   timestamptz not null default now(),
    updated_by   text        not null default ''
);

-- +goose Down
drop table if exists cluster_rules;
