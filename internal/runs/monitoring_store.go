package runs

import (
	"context"
	"encoding/json"

	"github.com/devtron-labs/devtron-sre-agent/internal/devtron"
)

// LoadMonitoringChoice returns the endpoints an operator pinned for a cluster.
//
// No row is the normal state, not an error: it means "use whatever discovery
// picks", which is what every cluster does until somebody says otherwise.
func (s *Store) LoadMonitoringChoice(ctx context.Context, clusterID int) (devtron.Choice, error) {
	var metrics, alerts []byte
	err := s.pool.QueryRow(ctx,
		`select metrics, alerts from cluster_monitoring where cluster_id = $1`, clusterID).
		Scan(&metrics, &alerts)
	if err != nil {
		return devtron.Choice{}, nil
	}
	return devtron.Choice{
		Metrics: decodePick(metrics),
		Alerts:  decodePick(alerts),
	}, nil
}

// SaveMonitoringChoice replaces a cluster's pins. A nil half clears that half
// back to discovery, which is what the "Auto" row on the picker sends.
func (s *Store) SaveMonitoringChoice(ctx context.Context, clusterID int, clusterName string, c devtron.Choice, by string) error {
	_, err := s.pool.Exec(ctx, `
		insert into cluster_monitoring (cluster_id, cluster_name, metrics, alerts, updated_at, updated_by)
		values ($1, $2, $3, $4, now(), $5)
		on conflict (cluster_id) do update set
			cluster_name = excluded.cluster_name,
			metrics      = excluded.metrics,
			alerts       = excluded.alerts,
			updated_at   = now(),
			updated_by   = excluded.updated_by`,
		clusterID, clusterName, encodePick(c.Metrics), encodePick(c.Alerts), by)
	return err
}

// ClearMonitoringChoice drops both pins, putting the cluster back on
// discovery entirely.
func (s *Store) ClearMonitoringChoice(ctx context.Context, clusterID int) error {
	_, err := s.pool.Exec(ctx, `delete from cluster_monitoring where cluster_id = $1`, clusterID)
	return err
}

// decodePick turns a stored column into a pick. A null column, an empty one
// or one holding something unreadable all mean the same thing — nothing is
// pinned — because the alternative is a cluster that cannot be read because
// its configuration row is malformed.
func decodePick(raw []byte) *devtron.Pick {
	if len(raw) == 0 {
		return nil
	}
	var p devtron.Pick
	if err := json.Unmarshal(raw, &p); err != nil || !p.Valid() {
		return nil
	}
	return &p
}

func encodePick(p *devtron.Pick) []byte {
	if p == nil || !p.Valid() {
		return nil
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil
	}
	return b
}

// PinnedMonitoringClusters is the set of clusters whose monitoring endpoints
// an operator has pinned.
//
// One query for the whole cluster list, on purpose. The alternative — asking
// each card whether its cluster is pinned — would kick off a discovery walk
// per cluster on a screen whose job is to let you pick one.
func (s *Store) PinnedMonitoringClusters(ctx context.Context) (map[int]bool, error) {
	rows, err := s.pool.Query(ctx,
		`select cluster_id from cluster_monitoring where metrics is not null or alerts is not null`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int]bool{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
