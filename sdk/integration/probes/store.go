package probes

import (
	"context"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
)

// probedStore wraps a statestore.Store and counts Load/Save/Fork calls.
// Counts are reset by Probes.ResetCounters at the start of each observation
// window, so init-time traffic from sdk.Open does not pollute Send-scoped
// assertions.
type probedStore struct {
	inner statestore.Store

	mu    sync.Mutex
	loads int
	saves int
	forks int
}

func newProbedStore(inner statestore.Store) *probedStore {
	return &probedStore{inner: inner}
}

// Load forwards to the inner store and increments the Load counter.
func (s *probedStore) Load(ctx context.Context, id string) (*statestore.ConversationState, error) {
	s.mu.Lock()
	s.loads++
	s.mu.Unlock()
	return s.inner.Load(ctx, id)
}

// Save forwards to the inner store and increments the Save counter.
func (s *probedStore) Save(ctx context.Context, state *statestore.ConversationState) error {
	s.mu.Lock()
	s.saves++
	s.mu.Unlock()
	return s.inner.Save(ctx, state)
}

// Fork forwards to the inner store and increments the Fork counter.
func (s *probedStore) Fork(ctx context.Context, sourceID, newID string) error {
	s.mu.Lock()
	s.forks++
	s.mu.Unlock()
	return s.inner.Fork(ctx, sourceID, newID)
}

func (s *probedStore) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loads, s.saves, s.forks = 0, 0, 0
}

func (s *probedStore) snapshot() (loads, saves, forks int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loads, s.saves, s.forks
}

// Compile-time assertion that probedStore implements statestore.Store.
var _ statestore.Store = (*probedStore)(nil)
