-- +goose Up
-- What discovery found for each cluster, kept.
--
-- This lived only in memory behind a fifteen minute timer, so the answer to
-- "what monitoring does this cluster run" was forgotten on every restart and
-- re-derived under whoever asked next. On an installation with fifty
-- clusters that is the difference between an alert list that loads and one
-- that walks every cluster before it can show anything.
--
-- A stored stack is authoritative until somebody asks for a new measurement
-- or a read against it fails. It is not expired on a timer: an answer that
-- changes under you cannot be the basis for filtering alerts by cluster.
create table if not exists cluster_monitoring_discovered (
    cluster_id    integer     primary key,
    -- The whole MonitoringStack, candidates and notes included, so the
    -- picker can be drawn from a restored row without re-probing.
    stack         jsonb       not null,
    -- When the measurement was taken, as distinct from when the row was
    -- written. The UI shows the first and nobody needs the second, but a
    -- row rewritten unchanged should not look freshly measured.
    discovered_at timestamptz not null,
    updated_at    timestamptz not null default now()
);

-- +goose Down
drop table if exists cluster_monitoring_discovered;
