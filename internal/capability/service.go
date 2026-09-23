// Package capability answers one question the cluster list cannot: which
// clusters will actually serve this token.
//
// Devtron lists every cluster it knows about, including ones the orchestrator
// can no longer reach. On a live install only 2 of 23 listed clusters served
// workloads; the rest timed out, errored, or came back empty. Offering all 23
// in a picker means most choices lead to a run that hangs for its full
// timeout and concludes nothing.
//
// So the product shows the intersection: listed AND measured as usable.
package capability

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/devtron-labs/devtron-sre-agent/internal/devtron"
)

// Store persists probe results across restarts.
type Store interface {
	SaveCapabilities(ctx context.Context, caps []devtron.Capability) error
	LoadCapabilities(ctx context.Context) ([]devtron.Capability, error)
	ClearCapabilities(ctx context.Context) error
}

// Service caches and refreshes cluster capabilities.
type Service struct {
	dc     *devtron.Client
	prober *devtron.Prober
	store  Store
	log    *slog.Logger
	ttl    time.Duration

	mu   sync.RWMutex
	byID map[int]devtron.Capability

	// sweeping serialises refreshes: a sweep is expensive and several
	// concurrent ones against an already-struggling orchestrator help nobody.
	sweeping sync.Mutex
}

// New builds a service.
func New(dc *devtron.Client, store Store, log *slog.Logger, ttl time.Duration) *Service {
	if ttl <= 0 {
		ttl = 20 * time.Minute
	}
	return &Service{
		dc: dc, prober: devtron.NewProber(dc), store: store, log: log, ttl: ttl,
		byID: map[int]devtron.Capability{},
	}
}

// Hydrate loads stored probes at boot so the first page view is instant.
func (s *Service) Hydrate(ctx context.Context) {
	caps, err := s.store.LoadCapabilities(ctx)
	if err != nil {
		s.log.Warn("could not load cluster capabilities", "err", err)
		return
	}
	s.put(caps)
	if len(caps) > 0 {
		s.log.Info("cluster capabilities restored", "clusters", len(caps), "usable", len(s.usableIDs()))
	}
}

// Get returns one cluster's capability, or nil when it has never been probed.
func (s *Service) Get(clusterID int) *devtron.Capability {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.byID[clusterID]
	if !ok {
		return nil
	}
	return &c
}

// All returns every known capability, best reach first.
func (s *Service) All() []devtron.Capability {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]devtron.Capability, 0, len(s.byID))
	for _, c := range s.byID {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Reach != out[j].Reach {
			return rank(out[i].Reach) < rank(out[j].Reach)
		}
		return out[i].ClusterName < out[j].ClusterName
	})
	return out
}

// Stale reports whether a sweep is due.
func (s *Service) Stale() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.byID) == 0 {
		return true
	}
	for _, c := range s.byID {
		if time.Since(c.ProbedAt) > s.ttl {
			return true
		}
	}
	return false
}

// Clusters returns the clusters worth offering.
//
// usableOnly is the product default: the intersection of what Devtron lists
// and what actually answered. Passing false returns everything, annotated,
// which is what the settings and diagnostics views want.
func (s *Service) Clusters(ctx context.Context, usableOnly bool) ([]devtron.Cluster, error) {
	all, err := s.dc.Clusters(ctx)
	if err != nil {
		return nil, err
	}
	if !usableOnly {
		return all, nil
	}
	// Never probed yet: return everything rather than an empty screen, and
	// let the sweep narrow it once it lands.
	if len(s.All()) == 0 {
		return all, nil
	}
	out := make([]devtron.Cluster, 0, len(all))
	for _, c := range all {
		if cap := s.Get(c.ID); cap != nil && cap.Reach.Investigable() {
			out = append(out, c)
		}
	}
	return out, nil
}

// Refresh sweeps every cluster and persists the result.
func (s *Service) Refresh(ctx context.Context) ([]devtron.Capability, error) {
	s.sweeping.Lock()
	defer s.sweeping.Unlock()

	clusters, err := s.dc.Clusters(ctx)
	if err != nil {
		return nil, err
	}
	started := time.Now()
	caps := s.prober.ProbeAll(ctx, clusters)
	s.put(caps)
	if err := s.store.SaveCapabilities(ctx, caps); err != nil {
		s.log.Warn("could not persist cluster capabilities", "err", err)
	}

	usable := 0
	for _, c := range caps {
		if c.Reach.Investigable() {
			usable++
		}
	}
	s.log.Info("cluster capability sweep",
		"clusters", len(caps), "usable", usable, "took", time.Since(started).Round(time.Millisecond))
	return caps, nil
}

// RefreshInBackground sweeps without blocking a request.
func (s *Service) RefreshInBackground(ctx context.Context) {
	go func() {
		bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Minute)
		defer cancel()
		if _, err := s.Refresh(bg); err != nil {
			s.log.Warn("capability sweep failed", "err", err)
		}
	}()
}

// Reset clears everything, for when the Devtron installation changes.
func (s *Service) Reset(ctx context.Context) {
	s.mu.Lock()
	s.byID = map[int]devtron.Capability{}
	s.mu.Unlock()
	if err := s.store.ClearCapabilities(ctx); err != nil {
		s.log.Warn("could not clear cluster capabilities", "err", err)
	}
}

func (s *Service) put(caps []devtron.Capability) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range caps {
		s.byID[c.ClusterID] = c
	}
}

func (s *Service) usableIDs() []int {
	var out []int
	for id, c := range s.byID {
		if c.Reach.Investigable() {
			out = append(out, id)
		}
	}
	return out
}

func rank(r devtron.Reach) int {
	switch r {
	case devtron.ReachUsable:
		return 0
	case devtron.ReachEmpty:
		return 1
	case devtron.ReachForbidden:
		return 2
	case devtron.ReachError:
		return 3
	case devtron.ReachUnreachable:
		return 4
	}
	return 5
}
