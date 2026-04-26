package probes

import (
	"context"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
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

// The probed store also surfaces optional statestore interfaces by delegating
// to the inner store when it implements them. Without these passthroughs,
// IncrementalSaveStage's type assertions against MessageReader / MessageAppender
// would fail on the wrapper and silently disable auto-summarization /
// incremental save — which would make probe-based contract tests vacuous.

// LoadRecentMessages delegates if the inner supports MessageReader;
// otherwise returns ErrNotFound so callers fall back to Load().
func (s *probedStore) LoadRecentMessages(
	ctx context.Context, id string, n int,
) ([]types.Message, error) {
	if r, ok := s.inner.(statestore.MessageReader); ok {
		return r.LoadRecentMessages(ctx, id, n)
	}
	return nil, statestore.ErrNotFound
}

// MessageCount delegates if the inner supports MessageReader; otherwise
// returns ErrNotFound.
func (s *probedStore) MessageCount(ctx context.Context, id string) (int, error) {
	if r, ok := s.inner.(statestore.MessageReader); ok {
		return r.MessageCount(ctx, id)
	}
	return 0, statestore.ErrNotFound
}

// AppendMessages delegates if the inner supports MessageAppender; otherwise
// returns ErrNotFound. Save calls remain counted via the wrapper's Save.
func (s *probedStore) AppendMessages(
	ctx context.Context, id string, messages []types.Message,
) error {
	if a, ok := s.inner.(statestore.MessageAppender); ok {
		return a.AppendMessages(ctx, id, messages)
	}
	return statestore.ErrNotFound
}

// LoadMetadata delegates if the inner supports MetadataAccessor; otherwise
// returns ErrNotFound.
func (s *probedStore) LoadMetadata(
	ctx context.Context, id string,
) (map[string]any, error) {
	if a, ok := s.inner.(statestore.MetadataAccessor); ok {
		return a.LoadMetadata(ctx, id)
	}
	return nil, statestore.ErrNotFound
}

// LoadSummaries delegates if the inner supports SummaryAccessor; otherwise
// returns nil (no summaries) so callers don't error.
func (s *probedStore) LoadSummaries(
	ctx context.Context, id string,
) ([]statestore.Summary, error) {
	if a, ok := s.inner.(statestore.SummaryAccessor); ok {
		return a.LoadSummaries(ctx, id)
	}
	return nil, nil
}

// SaveSummary delegates if the inner supports SummaryAccessor; otherwise
// returns ErrNotFound.
func (s *probedStore) SaveSummary(
	ctx context.Context, id string, summary statestore.Summary,
) error {
	if a, ok := s.inner.(statestore.SummaryAccessor); ok {
		return a.SaveSummary(ctx, id, summary)
	}
	return statestore.ErrNotFound
}
