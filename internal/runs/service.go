package runs

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
)

// Broker fans ledger entries out to live SSE subscribers. The database stays
// the durable record; this only avoids polling for the seconds a browser is
// actually watching. One process owns both, so nothing distributed is needed.
type Broker struct {
	mu   sync.RWMutex
	subs map[string]map[int]chan Event
	next int
}

// NewBroker builds an empty broker.
func NewBroker() *Broker { return &Broker{subs: map[string]map[int]chan Event{}} }

// Subscribe returns a channel of future events for a run and a cancel func.
func (b *Broker) Subscribe(runID string) (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.subs[runID] == nil {
		b.subs[runID] = map[int]chan Event{}
	}
	id := b.next
	b.next++
	ch := make(chan Event, 64)
	b.subs[runID][id] = ch
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if m := b.subs[runID]; m != nil {
			if c, ok := m[id]; ok {
				delete(m, id)
				close(c)
			}
			if len(m) == 0 {
				delete(b.subs, runID)
			}
		}
	}
}

// Publish delivers an event to current subscribers. A subscriber that cannot
// keep up is skipped rather than allowed to block the run: the client can
// always re-read the ledger from the database by sequence number.
func (b *Broker) Publish(runID string, e Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs[runID] {
		select {
		case ch <- e:
		default:
		}
	}
}

// Service is the run lifecycle façade the API and the worker share.
type Service struct {
	Store  *Store
	Broker *Broker
	Log    *slog.Logger
}

// NewService wires a service.
func NewService(store *Store, broker *Broker, log *slog.Logger) *Service {
	return &Service{Store: store, Broker: broker, Log: log}
}

// Append writes a ledger entry and publishes it. It returns the sequence
// number so a finding can cite it as [ev:N].
func (s *Service) Append(ctx context.Context, runID, typ, agent string, payload any) (int, error) {
	seq, at, err := s.Store.Append(ctx, runID, typ, agent, payload)
	if err != nil {
		if s.Log != nil {
			s.Log.Error("ledger append failed", "run", runID, "type", typ, "err", err)
		}
		return 0, err
	}
	raw, _ := json.Marshal(payload)
	// Publish the database's timestamp, not a fresh one, so a live event and
	// its replay are byte-identical.
	s.Broker.Publish(runID, Event{
		Seq: seq, At: at, Type: typ, Agent: agent, Payload: raw,
	})
	return seq, nil
}

// LedgerFunc is the append signature the agent runtime is given, so the
// agents package never needs to know about storage.
type LedgerFunc func(ctx context.Context, typ, agent string, payload any) (int, error)

// Ledger returns a LedgerFunc bound to one run.
func (s *Service) Ledger(runID string) LedgerFunc {
	return func(ctx context.Context, typ, agent string, payload any) (int, error) {
		// Ledger writes must survive the cancellation that is being recorded,
		// otherwise the reason a run stopped is the one thing never written.
		return s.Append(context.WithoutCancel(ctx), runID, typ, agent, payload)
	}
}

// Finish closes a run and publishes the terminal status so open streams end.
func (s *Service) Finish(ctx context.Context, runID, status, errMsg string, usage Usage) error {
	ctx = context.WithoutCancel(ctx)
	if _, err := s.Append(ctx, runID, EvStatus, "", map[string]any{
		"status": status, "error": errMsg, "usage": usage,
	}); err != nil && s.Log != nil {
		s.Log.Warn("could not record terminal status", "run", runID, "err", err)
	}
	return s.Store.Finish(ctx, runID, status, errMsg, usage)
}
