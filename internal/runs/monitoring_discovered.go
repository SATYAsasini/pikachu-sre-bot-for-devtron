package runs

import (
	"context"
	"encoding/json"
	"time"

	"github.com/devtron-labs/devtron-sre-agent/internal/devtron"
)

// SaveMonitoringStack records what discovery found for a cluster.
//
// Discovery used to live only in memory behind a fifteen minute timer, which
// made the answer to "what monitoring does this cluster run" something the
// process forgot on every restart and re-derived under whoever asked next.
// On an installation with fifty clusters that is the difference between an
// alert list that loads and one that walks every cluster first.
func (s *Store) SaveMonitoringStack(ctx context.Context, stack *devtron.MonitoringStack) error {
	if stack == nil || stack.ClusterID == 0 {
		return nil
	}
	body, err := json.Marshal(stack)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		insert into cluster_monitoring_discovered (cluster_id, stack, discovered_at, updated_at)
		values ($1, $2, $3, now())
		on conflict (cluster_id) do update set
			stack         = excluded.stack,
			discovered_at = excluded.discovered_at,
			updated_at    = now()`,
		stack.ClusterID, body, stack.DiscoveredAt)
	return err
}

// LoadMonitoringStacks returns every stored stack, for hydrating at boot.
//
// A stack that cannot be decoded is skipped rather than failing the load: one
// malformed row must not cost an installation its whole monitoring map.
func (s *Store) LoadMonitoringStacks(ctx context.Context) (map[int]*devtron.MonitoringStack, error) {
	rows, err := s.pool.Query(ctx,
		`select cluster_id, stack from cluster_monitoring_discovered`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int]*devtron.MonitoringStack{}
	for rows.Next() {
		var (
			id   int
			body []byte
		)
		if err := rows.Scan(&id, &body); err != nil {
			return nil, err
		}
		var stack devtron.MonitoringStack
		if json.Unmarshal(body, &stack) != nil || stack.ClusterID == 0 {
			continue
		}
		out[id] = &stack
	}
	return out, rows.Err()
}

// ForgetMonitoringStack drops a cluster's stored stack, which is what must
// happen when the Devtron installation changes: a stack discovered against
// one host says nothing about another.
func (s *Store) ForgetMonitoringStacks(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `delete from cluster_monitoring_discovered`)
	return err
}

// MonitoringStackAge is how old a stored stack is, for a caller deciding
// whether to offer a re-probe. Never used to expire one.
func MonitoringStackAge(stack *devtron.MonitoringStack) time.Duration {
	if stack == nil || stack.DiscoveredAt.IsZero() {
		return 0
	}
	return time.Since(stack.DiscoveredAt)
}
