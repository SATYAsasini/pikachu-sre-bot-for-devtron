package capability

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/devtron-labs/devtron-sre-agent/internal/devtron"
)

func newTestService(t *testing.T, o fakeOpts) (*Service, *fakeOrchestrator, *memStore) {
	t.Helper()
	f, dc := newFakeOrchestrator(o)
	t.Cleanup(f.Close)
	store := &memStore{}
	s := New(dc, store, quietLog(), time.Minute)
	fastProber(s, 150*time.Millisecond)
	return s, f, store
}

func TestRefreshMeasuresEveryCluster(t *testing.T) {
	t.Parallel()

	s, f, store := newTestService(t, fakeOpts{
		Clusters: fxClusters(2, 3),
		Usable:   map[int]bool{1: true, 2: true},
	})

	caps, err := s.Refresh(t.Context())
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if len(caps) != 5 {
		t.Fatalf("want 5 capabilities, got %d", len(caps))
	}
	usable := 0
	for _, c := range caps {
		if c.Reach.Investigable() {
			usable++
		}
	}
	if usable != 2 {
		t.Errorf("want 2 usable, got %d", usable)
	}
	if f.sweeps() != 1 {
		t.Errorf("one refresh cost %d sweeps", f.sweeps())
	}
	if store.saveCount() != 1 {
		t.Errorf("want one persist, got %d", store.saveCount())
	}
}

// The reported bug: POST /v1/clusters/refresh appeared to hang. It was not
// hung — it was queued behind background sweeps and then ran its own. Every
// caller that arrives during a sweep must join it, not follow it.
func TestConcurrentRefreshesCostOneSweep(t *testing.T) {
	t.Parallel()

	s, f, _ := newTestService(t, fakeOpts{
		Clusters:   fxClusters(1, 2),
		Usable:     map[int]bool{1: true},
		ProbeDelay: 60 * time.Millisecond,
	})

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Refresh(t.Context()); err != nil {
				t.Errorf("refresh: %v", err)
			}
		}()
	}
	wg.Wait()

	if n := f.sweeps(); n != 1 {
		t.Errorf("ten concurrent refreshes cost %d sweeps, want 1", n)
	}
}

// Every caller still gets the answer, not just the one that started it.
func TestJoinersGetTheResult(t *testing.T) {
	t.Parallel()

	s, _, _ := newTestService(t, fakeOpts{
		Clusters:   fxClusters(2, 1),
		Usable:     map[int]bool{1: true, 2: true},
		ProbeDelay: 50 * time.Millisecond,
	})

	results := make([][]int, 5)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			caps, err := s.Refresh(t.Context())
			if err != nil {
				t.Errorf("refresh: %v", err)
				return
			}
			ids := make([]int, 0, len(caps))
			for _, c := range caps {
				ids = append(ids, c.ClusterID)
			}
			results[i] = ids
		}()
	}
	wg.Wait()

	for i, got := range results {
		if len(got) != 3 {
			t.Errorf("caller %d got %d capabilities, want 3", i, len(got))
		}
	}
}

// Stale() driving RefreshInBackground is what turned one sweep into a
// permanent loop: results are only stored at the end, so every poll during
// the sweep saw stale timestamps and asked for another.
func TestStaleIsFalseWhileASweepIsRunning(t *testing.T) {
	t.Parallel()

	s, f, _ := newTestService(t, fakeOpts{
		Clusters:   fxClusters(1, 1),
		Usable:     map[int]bool{1: true},
		ProbeDelay: 200 * time.Millisecond,
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := s.Refresh(t.Context()); err != nil {
			t.Errorf("refresh: %v", err)
		}
	}()

	if !awaitSweep(s, 2*time.Second) {
		t.Fatal("the sweep never started")
	}

	// Poll the way the dashboard does while the sweep is in flight.
	deadline := time.Now().Add(100 * time.Millisecond)
	polls := 0
	for time.Now().Before(deadline) {
		if s.Stale() {
			t.Fatal("a sweep is already running, so none is due")
		}
		s.RefreshInBackground(t.Context())
		polls++
		time.Sleep(10 * time.Millisecond)
	}
	<-done

	if polls == 0 {
		t.Fatal("the test never polled during the sweep")
	}
	if n := f.sweeps(); n != 1 {
		t.Errorf("%d polls during one sweep produced %d sweeps, want 1", polls, n)
	}
}

// And once it has finished, a fresh result is not stale either.
func TestStaleIsFalseAfterASweep(t *testing.T) {
	t.Parallel()

	s, _, _ := newTestService(t, fakeOpts{
		Clusters: fxClusters(1, 0),
		Usable:   map[int]bool{1: true},
	})
	if !s.Stale() {
		t.Error("nothing measured yet is stale")
	}
	if _, err := s.Refresh(t.Context()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if s.Stale() {
		t.Error("a sweep that just finished is not stale")
	}
}

// A caller that gives up must not take the sweep down with it — the joiners
// and the cache still want the answer.
func TestCancelledCallerDoesNotAbortTheSweep(t *testing.T) {
	t.Parallel()

	s, f, _ := newTestService(t, fakeOpts{
		Clusters:   fxClusters(1, 1),
		Usable:     map[int]bool{1: true},
		ProbeDelay: 120 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := s.Refresh(ctx); err == nil {
		t.Fatal("a cancelled caller should get an error")
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(s.All()) == 2 {
			if n := f.sweeps(); n != 1 {
				t.Errorf("sweeps: %d, want 1", n)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the detached sweep never landed in the cache")
}

func TestRefreshSurfacesAClusterListFailure(t *testing.T) {
	t.Parallel()

	f, dc := newFakeOrchestrator(fakeOpts{Clusters: nil})
	f.Close() // nothing is listening
	s := New(dc, &memStore{}, quietLog(), time.Minute)

	if _, err := s.Refresh(t.Context()); err == nil {
		t.Fatal("an unreachable orchestrator should be an error, not an empty sweep")
	}
}

// A persistence failure must not lose the measurement: the sweep still
// answers and the cache still updates.
func TestSaveFailureDoesNotLoseTheSweep(t *testing.T) {
	t.Parallel()

	f, dc := newFakeOrchestrator(fakeOpts{
		Clusters: fxClusters(1, 0),
		Usable:   map[int]bool{1: true},
	})
	t.Cleanup(f.Close)
	store := &memStore{SaveErr: errors.New("disk on fire")}
	s := New(dc, store, quietLog(), time.Minute)
	fastProber(s, 150*time.Millisecond)

	caps, err := s.Refresh(t.Context())
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if len(caps) != 1 || s.Get(1) == nil {
		t.Errorf("the measurement was lost with the save: %+v", caps)
	}
}

func TestUnicodeClusterNamesSurvive(t *testing.T) {
	t.Parallel()

	s, _, _ := newTestService(t, fakeOpts{
		Clusters: fxUnicodeClusters(),
		Usable:   map[int]bool{1: true, 2: true},
	})

	caps, err := s.Refresh(t.Context())
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	names := map[string]bool{}
	for _, c := range caps {
		names[c.ClusterName] = true
	}
	for _, want := range []string{"本番クラスタ", "생산-클러스터"} {
		if !names[want] {
			t.Errorf("lost %q; got %v", want, names)
		}
	}
}

func TestEmptyClusterListIsNotAnError(t *testing.T) {
	t.Parallel()

	s, _, _ := newTestService(t, fakeOpts{Clusters: []map[string]any{}})

	caps, err := s.Refresh(t.Context())
	if err != nil {
		t.Fatalf("an install with no clusters is a fact, not a failure: %v", err)
	}
	if len(caps) != 0 {
		t.Errorf("want no capabilities, got %d", len(caps))
	}
}

// The other engine of the sweep loop, and the subtler one: clusters that a
// previous Devtron had. They were restored at boot, never re-measured
// because the sweep does not list them any more, and so kept Stale() true
// forever — one poll of the cluster list per finished sweep, indefinitely.
func TestASweepForgetsClustersDevtronNoLongerLists(t *testing.T) {
	t.Parallel()

	s, _, store := newTestService(t, fakeOpts{
		Clusters: fxClusters(1, 1),
		Usable:   map[int]bool{1: true},
	})

	// Two clusters from an older install, measured long ago.
	old := time.Now().Add(-48 * time.Hour)
	store.saved = []devtron.Capability{
		{ClusterID: 900, ClusterName: "decommissioned-a", Reach: devtron.ReachUsable, ProbedAt: old},
		{ClusterID: 901, ClusterName: "decommissioned-b", Reach: devtron.ReachUnreachable, ProbedAt: old},
	}
	s.Hydrate(t.Context())
	if len(s.All()) != 2 || !s.Stale() {
		t.Fatalf("setup: %d restored, stale=%v", len(s.All()), s.Stale())
	}

	if _, err := s.Refresh(t.Context()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	if got := len(s.All()); got != 2 {
		t.Errorf("want only the 2 clusters this sweep saw, got %d", got)
	}
	for _, id := range []int{900, 901} {
		if s.Get(id) != nil {
			t.Errorf("cluster %d is gone from Devtron and must not survive a sweep", id)
		}
	}
	// The whole point: one finished sweep means nothing is due.
	if s.Stale() {
		t.Error("a completed sweep left the service permanently stale")
	}
}

// Restoring from the store still merges — that is a different job from a
// sweep, and it must not throw away a measurement it did not make.
func TestHydrateMerges(t *testing.T) {
	t.Parallel()

	s, _, store := newTestService(t, fakeOpts{
		Clusters: fxClusters(1, 0),
		Usable:   map[int]bool{1: true},
	})
	store.saved = []devtron.Capability{
		{ClusterID: 7, ClusterName: "from-disk", Reach: devtron.ReachUsable, ProbedAt: time.Now()},
	}
	s.Hydrate(t.Context())
	s.Hydrate(t.Context())

	if len(s.All()) != 1 || s.Get(7) == nil {
		t.Errorf("hydrate should restore what was stored: %+v", s.All())
	}
}

// A sweep of a large install runs for a minute or more. Holding every answer
// back until the last cluster has timed out is what made it look hung.
func TestResultsArePublishedAsTheyLand(t *testing.T) {
	t.Parallel()

	s, _, _ := newTestService(t, fakeOpts{
		Clusters:   fxClusters(2, 2),
		Usable:     map[int]bool{1: true, 2: true},
		ProbeDelay: 120 * time.Millisecond,
	})
	// One at a time, so "some are done and some are not" is a real state.
	s.prober.Concurrency = 1

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := s.Refresh(t.Context()); err != nil {
			t.Errorf("refresh: %v", err)
		}
	}()

	if !awaitSweep(s, 2*time.Second) {
		t.Fatal("the sweep never started")
	}

	// Somewhere in the middle, the service must already know something.
	deadline := time.Now().Add(4 * time.Second)
	sawPartial := false
	for time.Now().Before(deadline) {
		p := s.Progress()
		if p.Sweeping && p.Probed > 0 && p.Probed < p.Total {
			if len(s.All()) < p.Probed {
				t.Fatalf("progress says %d measured but only %d published", p.Probed, len(s.All()))
			}
			sawPartial = true
			break
		}
		if !p.Sweeping {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	<-done

	if !sawPartial {
		t.Error("never observed a partially finished sweep publishing results")
	}
	if p := s.Progress(); p.Sweeping || p.Probed != 4 || p.Total != 4 {
		t.Errorf("after the sweep: %+v", p)
	}
}

func TestProgressIsZeroBeforeAnySweep(t *testing.T) {
	t.Parallel()

	s, _, _ := newTestService(t, fakeOpts{Clusters: fxClusters(1, 0), Usable: map[int]bool{1: true}})
	p := s.Progress()
	if p.Sweeping || p.Probed != 0 || p.Total != 0 {
		t.Errorf("want an idle zero progress, got %+v", p)
	}
	if !p.Started.IsZero() {
		t.Errorf("nothing has started, got %v", p.Started)
	}
}

// Clusters that worked last time are measured first, so the picker fills with
// answers rather than with a queue of timeouts.
func TestSweepOrdersKnownGoodClustersFirst(t *testing.T) {
	t.Parallel()

	s, _, store := newTestService(t, fakeOpts{
		Clusters: fxClusters(1, 2),
		Usable:   map[int]bool{1: true},
	})
	store.saved = []devtron.Capability{
		{ClusterID: 101, ClusterName: "dead-a", Reach: devtron.ReachUnreachable, ProbedAt: time.Now()},
		{ClusterID: 1, ClusterName: "live-a", Reach: devtron.ReachUsable, ProbedAt: time.Now()},
		{ClusterID: 102, ClusterName: "dead-b", Reach: devtron.ReachError, ProbedAt: time.Now()},
	}
	s.Hydrate(t.Context())

	if got := s.probeRank(1); got != 0 {
		t.Errorf("a cluster that worked should sort first, got rank %d", got)
	}
	if got := s.probeRank(101); got != 5 {
		t.Errorf("an unreachable cluster should sort last, got rank %d", got)
	}
	if got := s.probeRank(102); got != 4 {
		t.Errorf("an errored cluster ranks above an unreachable one, got %d", got)
	}
	// Never measured sits between: worth an early look, but not ahead of a
	// cluster already known to work.
	if got := s.probeRank(999); got <= 0 || got >= 4 {
		t.Errorf("an unmeasured cluster should sort early but not first, got %d", got)
	}
}

func TestTuneOverridesProbeBounds(t *testing.T) {
	t.Parallel()

	s, _, _ := newTestService(t, fakeOpts{Clusters: fxClusters(1, 0), Usable: map[int]bool{1: true}})
	before := s.prober.Timeout

	// Zero and negative values are "not configured" and must change nothing.
	s.Tune(0, 0)
	s.Tune(-1, -1)
	if s.prober.Timeout != before {
		t.Errorf("an unset config changed the timeout to %v", s.prober.Timeout)
	}

	s.Tune(45*time.Second, 7)
	if s.prober.Timeout != 45*time.Second || s.prober.Concurrency != 7 {
		t.Errorf("tune did not apply: %v / %d", s.prober.Timeout, s.prober.Concurrency)
	}
}
