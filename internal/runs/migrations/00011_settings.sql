-- +goose Up
-- A single row of operator-editable configuration, so Devtron can be pointed
-- at a different host from the UI without a redeploy. Environment variables
-- remain the bootstrap path; this table wins once a row exists.
create table settings (
    id          boolean primary key default true check (id),
    devtron_url text        not null default '',
    -- Stored as given. Anyone with database access already has the deployment's
    -- environment, so encrypting here would protect nothing new; the token is
    -- never returned over the API instead.
    devtron_token text      not null default '',
    updated_at  timestamptz not null default now(),
    updated_by  text        not null default ''
);

-- +goose Down
drop table if exists settings;
