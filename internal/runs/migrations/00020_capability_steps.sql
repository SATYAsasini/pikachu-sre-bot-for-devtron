-- +goose Up
-- What was asked to reach a cluster's verdict, in order.
--
-- The verdict alone is not enough to act on. "usable, readable in the
-- namespaces Devtron maps to this cluster" does not say whether the
-- cluster-wide read was refused or merely returned nothing, and those call
-- for completely different fixes — one is the token, the other is the
-- cluster. Keeping the steps means that question is answered on the screen
-- instead of by reading the source.
alter table cluster_capabilities
    add column if not exists steps jsonb;

-- +goose Down
alter table cluster_capabilities drop column if exists steps;
