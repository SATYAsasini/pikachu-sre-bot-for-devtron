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
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/devtron-labs/devtron-sre-agent/internal/devtron"
)

// SweepBudget bounds one capability sweep, however many clusters there are.
// A sweep of 23 clusters against a struggling orchestrator measured ~25s;
// three minutes is generous without being unbounded.
const SweepBudget = 3 * time.Minute

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

	// flight collapses concurrent sweeps into one. This used to be a plain
	// mutex, which serialised them instead of deduplicating them — every
	// caller still got its own full sweep, just later. See Refresh.
	flight singleflight.Group
	// inflight lets callers see that a sweep is already on its way without
	// blocking on it.
	inflight atomic.Bool
	// Progress counters, so a sweep that now takes a minute or two can be
	// watched filling in rather than waited out behind a spinner.
	probed    atomic.Int64
	total     atomic.Int64
	startedAt atomic.Int64 // unix nanos
}

// Progress is what a sweep looks like from outside while it runs.
type Progress struct {
	Sweeping bool      `json:"sweeping"`
	Probed   int       `json:"probed"`
	Total    int       `json:"total"`
	Started  time.Time `json:"startedAt,omitzero"`
}

// Progress reports how far the current sweep has got. Safe to call at any
// time; between sweeps it reports the last one's totals.
func (s *Service) Progress() Progress {
	p := Progress{
		Sweeping: s.inflight.Load(),
		Probed:   int(s.probed.Load()),
		Total:    int(s.total.Load()),
	}
	if n := s.startedAt.Load(); n > 0 {
		p.Started = time.Unix(0, n)
	}
	return p
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

// Tune overrides the probe bounds. Zero or negative values keep the defaults,
// so an unset config changes nothing.
func (s *Service) Tune(timeout time.Duration, concurrency, inFlight int) {
	if timeout > 0 {
		s.prober.Timeout = timeout
	}
	if concurrency > 0 {
		s.prober.Concurrency = concurrency
	}
	if inFlight > 0 {
		s.prober.MaxInFlight = inFlight
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
//
// A sweep already running means one is not due, whatever the timestamps say.
// Without that, every poll of the cluster list during the ~25s a sweep takes
// saw stale timestamps — the results are only stored at the end — and asked
// for another one. The dashboard polls, so the queue never drained and the
// orchestrator was swept continuously.
func (s *Service) Stale() bool {
	if s.inflight.Load() {
		return false
	}
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
		if measured := s.Get(c.ID); measured != nil && measured.Reach.Investigable() {
			out = append(out, c)
		}
	}
	return out, nil
}

// Refresh sweeps every cluster and persists the result.
//
// Callers that arrive while a sweep is running join that one and get its
// result. They used to queue behind it and then run another: clicking
// "re-measure" while the dashboard had a background sweep going cost the
// operator two full sweeps back to back, which reads as a hung button.
//
// The sweep itself is detached from the caller, so whoever started it giving
// up does not abandon everyone who joined.
func (s *Service) Refresh(ctx context.Context) ([]devtron.Capability, error) {
	ch := s.flight.DoChan("sweep", func() (any, error) {
		s.inflight.Store(true)
		defer s.inflight.Store(false)

		bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), SweepBudget)
		defer cancel()
		return s.sweep(bg, nil)
	})

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		return res.Val.([]devtron.Capability), nil
	}
}

func (s *Service) sweep(ctx context.Context, force map[int]bool) ([]devtron.Capability, error) {
	clusters, err := s.dc.Clusters(ctx)
	if err != nil {
		return nil, err
	}
	// Most likely to be useful first, so the picker fills with answers rather
	// than with a queue of timeouts.
	sort.SliceStable(clusters, func(i, j int) bool {
		return s.probeRank(clusters[i].ID) < s.probeRank(clusters[j].ID)
	})

	started := time.Now()
	s.total.Store(int64(len(clusters)))
	s.probed.Store(0)
	s.startedAt.Store(started.UnixNano())

	// Published as they land. A sweep of a large install takes long enough
	// that holding every answer back until the last cluster has timed out
	// makes the whole thing look hung.
	caps := s.prober.ProbeAll(ctx, clusters, devtron.SweepOptions{
		OnResult: func(c devtron.Capability) {
			s.putOne(c)
			s.probed.Add(1)
		},
		Force: force,
	})

	// And replaced at the end, which is what prunes the clusters Devtron no
	// longer lists. Incremental publishing can only ever add.
	s.replace(caps)
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

// ProbeOne measures a single cluster, whatever Devtron's connection status
// says about it.
//
// Devtron's status is trusted by default because it has been right every
// time it was checked, but it is still a cached opinion held by another
// service. An operator who has just fixed a cluster should not have to wait
// for Devtron to notice before this one will look.
func (s *Service) ProbeOne(ctx context.Context, clusterID int) (devtron.Capability, error) {
	clusters, err := s.dc.Clusters(ctx)
	if err != nil {
		return devtron.Capability{}, err
	}
	for _, c := range clusters {
		if c.ID != clusterID {
			continue
		}
		var namespaces []string
		if envs, err := s.dc.EnvironmentsInCluster(ctx, clusterID); err == nil {
			for _, e := range envs {
				if e.Namespace != "" {
					namespaces = append(namespaces, e.Namespace)
				}
			}
		}
		measured := s.prober.Probe(ctx, c.ID, c.ClusterName, namespaces)
		s.putOne(measured)
		if err := s.store.SaveCapabilities(ctx, s.All()); err != nil {
			s.log.Warn("could not persist the cluster capability", "cluster", clusterID, "err", err)
		}
		return measured, nil
	}
	return devtron.Capability{}, fmt.Errorf("devtron does not list a cluster with id %d", clusterID)
}

// RefreshInBackground sweeps without blocking a request, and does nothing
// when a sweep is already under way.
func (s *Service) RefreshInBackground(ctx context.Context) {
	if s.inflight.Load() {
		return
	}
	go func() {
		// Refresh detaches and budgets the sweep itself; this context only
		// decides how long this particular caller waits to join one.
		bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), SweepBudget)
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

// replace swaps the whole measurement set for a completed sweep's results.
//
// Merging was wrong, and expensively so. A cluster Devtron no longer lists
// stayed in the map with whatever timestamp it had when it was last seen, so
// Stale() — which is true if *any* entry has aged out — was true forever.
// Every poll of the cluster list then started another sweep the moment the
// previous one ended. On the installation this was found on, two clusters
// left over from an earlier Devtron were enough to make the service sweep
// continuously for as long as it ran.
//
// A sweep sees the current cluster list, so it is authoritative about which
// clusters exist. An empty one cannot get here: Refresh fails first when the
// list could not be read.
func (s *Service) replace(caps []devtron.Capability) {
	next := make(map[int]devtron.Capability, len(caps))
	for _, c := range caps {
		next[c.ClusterID] = c
	}
	s.mu.Lock()
	s.byID = next
	s.mu.Unlock()
}

// putOne publishes a single cluster's verdict mid-sweep.
func (s *Service) putOne(c devtron.Capability) {
	s.mu.Lock()
	s.byID[c.ClusterID] = c
	s.mu.Unlock()
}

// probeRank orders a sweep by what the last one found: clusters that worked
// before are measured first, clusters that could not be reached last.
func (s *Service) probeRank(clusterID int) int {
	s.mu.RLock()
	c, ok := s.byID[clusterID]
	s.mu.RUnlock()
	if !ok {
		return 1 // never measured: worth an early look
	}
	switch c.Reach {
	case devtron.ReachUsable:
		return 0
	case devtron.ReachEmpty:
		return 2
	case devtron.ReachForbidden:
		return 3
	case devtron.ReachError:
		return 4
	case devtron.ReachUnreachable:
		return 5
	}
	return 1
}

// put merges, which is what restoring from the store wants.
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
