-- +goose Up
-- What each cluster will actually serve this token, measured rather than
-- assumed. Devtron lists clusters it cannot reach, so this is the difference
-- between a run that works and a run that hangs for its whole timeout.
create table cluster_capabilities (
    cluster_id   integer primary key,
    cluster_name text        not null default '',
    reach        text        not null,
    detail       text        not null default '',
    latency_ms   bigint      not null default 0,
    kinds        jsonb       not null default '[]'::jsonb,
    probed_at    timestamptz not null default now()
);

create index cluster_capabilities_reach_idx on cluster_capabilities (reach);

-- +goose Down
drop table if exists cluster_capabilities;
