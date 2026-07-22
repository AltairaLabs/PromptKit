package tools

import (
	"context"
	"maps"
	"sync"
	"time"
)

// pendingCleanupInterval is the interval between TTL cleanup sweeps.
const pendingCleanupInterval = 30 * time.Second

// PendingStoreOption configures a MemoryPendingStore during construction.
type PendingStoreOption func(*MemoryPendingStore)

// WithPendingTTL sets the time-to-live for pending tool calls.
// Entries older than this are removed during periodic cleanup.
func WithPendingTTL(ttl time.Duration) PendingStoreOption {
	return func(s *MemoryPendingStore) {
		s.ttl = ttl
	}
}

// WithMaxPending sets the maximum number of pending tool calls allowed.
func WithMaxPending(limit int) PendingStoreOption {
	return func(s *MemoryPendingStore) {
		s.maxPending = limit
	}
}

// MemoryPendingStore is the default in-process PendingStore. It keys pending
// calls by (conversationID, id), bounds total entries, and sweeps expired
// entries on a background goroutine. It is the zero-config default when no
// durable store is injected via WithPendingStore.
type MemoryPendingStore struct {
	pending    map[string]*PendingToolCall // key: convID + "\x00" + id
	mu         sync.RWMutex
	ttl        time.Duration
	maxPending int
	stopCh     chan struct{}
	stopped    chan struct{}
	nowFunc    func() time.Time // for testing
}

// compile-time assertion that MemoryPendingStore satisfies the interface.
var _ PendingStore = (*MemoryPendingStore)(nil)

// NewMemoryPendingStore creates an in-memory pending store with TTL-based
// cleanup. Call Close() when the store is no longer needed to stop the cleanup
// goroutine.
func NewMemoryPendingStore(opts ...PendingStoreOption) *MemoryPendingStore {
	s := &MemoryPendingStore{
		pending:    make(map[string]*PendingToolCall),
		ttl:        DefaultPendingTTL,
		maxPending: DefaultMaxPending,
		stopCh:     make(chan struct{}),
		stopped:    make(chan struct{}),
		nowFunc:    time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	go s.cleanupLoop()
	return s
}

func pendingKey(convID, id string) string { return convID + "\x00" + id }

// cleanupLoop periodically removes expired entries.
func (s *MemoryPendingStore) cleanupLoop() {
	defer close(s.stopped)
	ticker := time.NewTicker(pendingCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.removeExpired()
		}
	}
}

// removeExpired removes all entries that have exceeded the TTL.
func (s *MemoryPendingStore) removeExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.nowFunc()
	for key, call := range s.pending {
		if now.Sub(call.CreatedAt) > s.ttl {
			delete(s.pending, key)
		}
	}
}

// Close stops the background cleanup goroutine and waits for it to finish.
// It satisfies the optional Closer capability.
func (s *MemoryPendingStore) Close() error {
	select {
	case <-s.stopCh:
		// Already closed
		return nil
	default:
		close(s.stopCh)
	}
	<-s.stopped
	return nil
}

// Add stores a pending tool call. Returns ErrPendingStoreFull if the store has
// reached its maximum capacity.
func (s *MemoryPendingStore) Add(_ context.Context, call *PendingToolCall) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.pending) >= s.maxPending {
		return ErrPendingStoreFull
	}

	call.CreatedAt = s.nowFunc()
	s.pending[pendingKey(call.ConversationID, call.ID)] = call
	return nil
}

// Get retrieves a read-only view of a pending tool call. It returns a copy so a
// caller cannot mutate the stored record — matching RedisPendingStore, which
// decodes a fresh value.
func (s *MemoryPendingStore) Get(_ context.Context, convID, id string) (*PendingToolCall, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	call, ok := s.pending[pendingKey(convID, id)]
	if !ok {
		return nil, false, nil
	}
	return copyCall(call), true, nil
}

// List returns copies of all pending tool calls for a conversation.
func (s *MemoryPendingStore) List(_ context.Context, convID string) ([]*PendingToolCall, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*PendingToolCall, 0)
	for _, call := range s.pending {
		if call.ConversationID == convID {
			result = append(result, copyCall(call))
		}
	}
	return result, nil
}

// copyCall returns a defensive copy so read paths cannot mutate the stored
// record. Claim intentionally returns the stored value (it is removed anyway).
func copyCall(c *PendingToolCall) *PendingToolCall {
	cp := *c
	cp.Arguments = maps.Clone(c.Arguments)
	return &cp
}

// Claim atomically removes and returns a pending tool call. The delete-under-lock
// makes it single-winner: concurrent claimers for the same id see at most one
// ok=true.
func (s *MemoryPendingStore) Claim(_ context.Context, convID, id string) (*PendingToolCall, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := pendingKey(convID, id)
	call, ok := s.pending[key]
	if !ok {
		return nil, false, nil
	}
	delete(s.pending, key)
	return call, true, nil
}
