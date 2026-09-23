package runs

import (
	"context"
	"encoding/json"
	"time"

	"github.com/devtron-labs/devtron-sre-agent/internal/devtron"
)

// SaveCapabilities upserts a probe sweep.
func (s *Store) SaveCapabilities(ctx context.Context, caps []devtron.Capability) error {
	if len(caps) == 0 {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx)) //nolint:errcheck // no-op after commit
	for _, c := range caps {
		kinds, _ := json.Marshal(c.Kinds)
		if _, err := tx.Exec(ctx, `
			insert into cluster_capabilities
				(cluster_id, cluster_name, reach, detail, latency_ms, kinds, probed_at)
			values ($1, $2, $3, $4, $5, $6, $7)
			on conflict (cluster_id) do update set
				cluster_name = excluded.cluster_name,
				reach        = excluded.reach,
				detail       = excluded.detail,
				latency_ms   = excluded.latency_ms,
				kinds        = excluded.kinds,
				probed_at    = excluded.probed_at`,
			c.ClusterID, c.ClusterName, string(c.Reach), c.Detail, c.LatencyMs, kinds, c.ProbedAt,
		); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// LoadCapabilities returns every stored probe.
func (s *Store) LoadCapabilities(ctx context.Context) ([]devtron.Capability, error) {
	rows, err := s.pool.Query(ctx, `
		select cluster_id, cluster_name, reach, detail, latency_ms, kinds, probed_at
		from cluster_capabilities`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []devtron.Capability{}
	for rows.Next() {
		var (
			c     devtron.Capability
			reach string
			kinds []byte
		)
		if err := rows.Scan(&c.ClusterID, &c.ClusterName, &reach, &c.Detail, &c.LatencyMs, &kinds, &c.ProbedAt); err != nil {
			return nil, err
		}
		c.Reach = devtron.Reach(reach)
		_ = json.Unmarshal(kinds, &c.Kinds)
		out = append(out, c)
	}
	return out, rows.Err()
}

// ClearCapabilities drops every probe, which is what must happen when the
// Devtron installation changes: capabilities measured against one host say
// nothing about another.
func (s *Store) ClearCapabilities(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `delete from cluster_capabilities`)
	return err
}

// StaleBefore is how old a probe may be before it is re-measured. Clusters go
// down and permissions change, but not by the minute.
const StaleBefore = 20 * time.Minute
