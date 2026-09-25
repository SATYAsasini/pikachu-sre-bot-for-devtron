package runs

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Ledger sequence numbers are allocated by reading max(seq) and writing
// max+1 in one statement, because findings cite them as [ev:N] and a
// Postgres sequence would leave holes. That read-then-write is a
// lost-update: two concurrent appends for the same run both see the same
// maximum, both insert it, and the second dies on the primary key.
//
// It is not a rare race. The model calls tools in parallel by default, so it
// fires the first time an agent asks two questions in one turn — observed on
// a real run as `duplicate key value violates unique constraint
// "run_events_pkey" (SQLSTATE 23505)`, with the losing tool never running at
// all.
//
// Needs a database. Skipped without one rather than failing, so the rest of
// the suite stays runnable anywhere.
func TestAppendIsSafeUnderParallelToolCalls(t *testing.T) {
	url := os.Getenv("SRE_DATABASE_URL")
	if url == "" {
		t.Skip("set SRE_DATABASE_URL to exercise the ledger against a real database")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Skipf("no database: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("no database: %v", err)
	}

	store := NewStore(pool)
	run, err := store.Create(ctx, CreateRequest{
		ClusterID: 1, ClusterName: "ledger-race-test", Ask: "concurrency probe",
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.WithoutCancel(ctx), `delete from runs where id = $1`, run.ID)
	})

	// Far more than a model would issue at once, so a lost update is
	// overwhelmingly likely if the allocation is not serialised.
	const parallel = 24
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
		seqs []int
	)
	for i := range parallel {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			seq, _, err := store.Append(ctx, run.ID, "tool_call", "sre", map[string]any{"i": i})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			seqs = append(seqs, seq)
		}(i)
	}
	wg.Wait()

	for _, err := range errs {
		t.Errorf("append failed: %v", err)
	}
	if len(seqs) != parallel {
		t.Fatalf("want %d appends, got %d", parallel, len(seqs))
	}

	// Gapless and unique, which is the whole reason the numbers are
	// allocated this way rather than from a sequence.
	seen := map[int]bool{}
	lo, hi := seqs[0], seqs[0]
	for _, s := range seqs {
		if seen[s] {
			t.Fatalf("sequence %d was handed out twice", s)
		}
		seen[s] = true
		lo, hi = min(lo, s), max(hi, s)
	}
	if hi-lo+1 != parallel {
		t.Errorf("sequences are not contiguous: %d..%d over %d appends", lo, hi, parallel)
	}
}
