-- +goose Up
-- Which monitoring endpoints a cluster should use, when discovery finds more
-- than one that answers.
--
-- Discovery is still what finds them: nothing here can be typed in by hand,
-- and a row only ever names a Service the walk already reported. What it
-- settles is the case discovery cannot — a cluster running both vmalert and
-- an Alertmanager, where a name heuristic picks one of them silently and the
-- operator has no way to say it picked wrong.
--
-- A null column means "whatever discovery would choose", which is the state
-- every cluster starts in and most stay in.
create table if not exists cluster_monitoring (
    cluster_id   integer     primary key,
    cluster_name text        not null default '',
    -- {"namespace": "...", "name": "..."} — namespace and name only. The
    -- port, the API base and the flavor are measured, not chosen, so pinning
    -- them would mean a chart upgrade that moved a port silently broke the
    -- choice.
    metrics      jsonb,
    alerts       jsonb,
    updated_at   timestamptz not null default now(),
    updated_by   text        not null default ''
);

-- +goose Down
drop table if exists cluster_monitoring;
