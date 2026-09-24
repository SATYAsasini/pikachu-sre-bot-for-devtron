package runs

import (
	"context"
	"encoding/json"

	"github.com/devtron-labs/devtron-sre-agent/internal/rules"
)

// LoadRules returns one cluster's rules.
//
// A cluster with no row is not an error and not an empty product: it returns
// a zero Config, which shows every alert and auto-triggers nothing. Defaults
// that hide things until configured are how a monitoring tool convinces
// somebody a cluster is quiet.
func (s *Store) LoadRules(ctx context.Context, clusterID int) (rules.Config, error) {
	cfg := rules.Config{ClusterID: clusterID}
	var show, mute, auto, prio []byte

	err := s.pool.QueryRow(ctx, `
		select show, mute, auto, auto_enabled, priority
		from cluster_rules where cluster_id = $1`, clusterID).
		Scan(&show, &mute, &auto, &cfg.AutoEnabled, &prio)
	if err != nil {
		// No row: the zero config is the correct answer.
		return cfg, nil
	}

	_ = json.Unmarshal(show, &cfg.Show)
	_ = json.Unmarshal(mute, &cfg.Mute)
	_ = json.Unmarshal(auto, &cfg.Auto)
	_ = json.Unmarshal(prio, &cfg.Priority)
	return cfg, nil
}

// SaveRules replaces a cluster's rules wholesale.
func (s *Store) SaveRules(ctx context.Context, cfg rules.Config, clusterName, by string) error {
	show, _ := json.Marshal(orEmptyRules(cfg.Show))
	mute, _ := json.Marshal(orEmptyRules(cfg.Mute))
	auto, _ := json.Marshal(orEmptyRules(cfg.Auto))
	prio, _ := json.Marshal(orEmptyRules(cfg.Priority))

	_, err := s.pool.Exec(ctx, `
		insert into cluster_rules
			(cluster_id, cluster_name, show, mute, auto, auto_enabled, priority, updated_at, updated_by)
		values ($1, $2, $3, $4, $5, $6, $7, now(), $8)
		on conflict (cluster_id) do update set
			cluster_name = excluded.cluster_name,
			show         = excluded.show,
			mute         = excluded.mute,
			auto         = excluded.auto,
			auto_enabled = excluded.auto_enabled,
			priority     = excluded.priority,
			updated_at   = now(),
			updated_by   = excluded.updated_by`,
		cfg.ClusterID, clusterName, show, mute, auto, cfg.AutoEnabled, prio, by)
	return err
}

// orEmptyRules keeps `[]` out of the database as `null`, so a reader never has
// to tell the two apart.
func orEmptyRules(r []rules.Rule) []rules.Rule {
	if r == nil {
		return []rules.Rule{}
	}
	return r
}
