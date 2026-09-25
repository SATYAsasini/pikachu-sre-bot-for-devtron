-- +goose Up
-- What a token can see in a cluster, and whether the verdict was measured.
--
-- The reach check is one call now — list Namespaces — and the namespace list
-- it returns is the useful part, not a by-product: it is what the cluster's
-- own page shows, and what a namespace-scoped token proves itself with. And
-- a verdict taken from Devtron's own connection status is a different kind
-- of claim from one we measured, so it says which it is.
alter table cluster_capabilities
    add column if not exists namespaces   jsonb,
    add column if not exists from_devtron boolean not null default false;

-- +goose Down
alter table cluster_capabilities
    drop column if exists namespaces,
    drop column if exists from_devtron;
