package statestore

import (
	"container/heap"
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// =============================================================================
// Min-heap for O(log N) LRU eviction
// =============================================================================

// accessEntry tracks a key's last access time for the LRU heap.
type accessEntry struct {
	key        string
	lastAccess time.Time
	index      int // position in the heap, maintained by heap.Interface
}

// accessHeap is a min-heap of accessEntry ordered by lastAccess (oldest first).
type accessHeap []*accessEntry

func (h accessHeap) Len() int           { return len(h) }
func (h accessHeap) Less(i, j int) bool { return h[i].lastAccess.Before(h[j].lastAccess) }
func (h accessHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i]; h[i].index = i; h[j].index = j }

// Push adds an element to the heap. Required by heap.Interface.
func (h *accessHeap) Push(x interface{}) {
	entry := x.(*accessEntry)
	entry.index = len(*h)
	*h = append(*h, entry)
}

// Pop removes and returns the minimum element from the heap. Required by heap.Interface.
func (h *accessHeap) Pop() interface{} {
	old := *h
	n := len(old)
	entry := old[n-1]
	old[n-1] = nil // avoid memory leak
	entry.index = -1
	*h = old[:n-1]
	return entry
}

// DefaultTTL is the default time-to-live for conversation states in a MemoryStore.
// Entries that have not been accessed within the TTL are considered expired and
// eligible for eviction. Use WithNoTTL() to explicitly disable TTL expiration.
const DefaultTTL = 1 * time.Hour

// DefaultMaxEntries is the default maximum number of entries a MemoryStore will hold.
// When the limit is reached, the least-recently-accessed entry is evicted.
// Use WithNoMaxEntries() to explicitly disable the entry limit.
const DefaultMaxEntries = 10000

// Sort field constants for conversation ordering.
const (
	sortFieldCreatedAt = "created_at"
	sortFieldUpdatedAt = "updated_at"
)

// MemoryStoreOption configures optional behavior for MemoryStore.
type MemoryStoreOption func(*MemoryStore)

// WithMemoryTTL sets the time-to-live for conversation states. Entries that have not
// been accessed within the TTL are considered expired and eligible for eviction.
// A zero or negative TTL disables expiration. By default, DefaultTTL is applied.
// Use WithNoTTL() as an explicit, self-documenting way to disable TTL expiration.
func WithMemoryTTL(ttl time.Duration) MemoryStoreOption {
	return func(s *MemoryStore) {
		s.ttl = ttl
		s.ttlSet = true
	}
}

// WithNoTTL explicitly disables TTL expiration so entries never expire.
// This overrides the DefaultTTL that is otherwise applied automatically.
func WithNoTTL() MemoryStoreOption {
	return func(s *MemoryStore) {
		s.ttl = 0
		s.ttlSet = true
	}
}

// WithMemoryMaxEntries sets the maximum number of entries the store will hold.
// When the limit is reached, the least-recently-accessed entry is evicted.
// A zero or negative value disables the limit. By default, DefaultMaxEntries is applied.
// Use WithNoMaxEntries() as an explicit, self-documenting way to disable the entry limit.
func WithMemoryMaxEntries(n int) MemoryStoreOption {
	return func(s *MemoryStore) {
		if n > 0 {
			s.maxEntries = n
		} else {
			s.maxEntries = 0
		}
		s.maxEntriesSet = true
	}
}

// WithNoMaxEntries explicitly disables the max entries limit so the store can grow unbounded.
// This overrides the DefaultMaxEntries that is otherwise applied automatically.
func WithNoMaxEntries() MemoryStoreOption {
	return func(s *MemoryStore) {
		s.maxEntries = 0
		s.maxEntriesSet = true
	}
}

// WithMemoryEvictionInterval sets the interval for the background cleanup goroutine
// that removes expired entries. If zero, no background cleanup runs and eviction
// happens only lazily on access. Requires a non-zero TTL to have any effect.
func WithMemoryEvictionInterval(d time.Duration) MemoryStoreOption {
	return func(s *MemoryStore) {
		s.evictionInterval = d
	}
}

// MemoryStore provides an in-memory implementation of the Store interface.
// It is thread-safe and suitable for development, testing, and single-instance deployments.
// For distributed systems, use RedisStore or a database-backed implementation.
type MemoryStore struct {
	mu     sync.RWMutex
	states map[string]*ConversationState

	// Index for efficient user-based lookups
	userIndex map[string]map[string]struct{} // userID -> set of conversationIDs

	// LRU eviction heap (min-heap ordered by lastAccess)
	lruHeap   accessHeap
	heapIndex map[string]*accessEntry // key -> heap entry for O(log N) updates

	// TTL and eviction settings
	ttl              time.Duration // zero means no expiry
	maxEntries       int           // zero means no limit
	evictionInterval time.Duration // zero means no background cleanup
	ttlSet           bool          // true if TTL was explicitly configured via options
	maxEntriesSet    bool          // true if max entries was explicitly configured via options

	// Background cleanup
	stopCh    chan struct{} // closed to signal the cleanup goroutine to stop
	closeOnce sync.Once
}

// NewMemoryStore creates a new in-memory state store.
// By default, DefaultTTL and DefaultMaxEntries are applied to prevent unbounded memory
// growth in server scenarios. Options can be provided to override these defaults.
// Use WithNoTTL() and/or WithNoMaxEntries() to explicitly disable limits.
func NewMemoryStore(opts ...MemoryStoreOption) *MemoryStore {
	s := &MemoryStore{
		states:    make(map[string]*ConversationState),
		userIndex: make(map[string]map[string]struct{}),
		heapIndex: make(map[string]*accessEntry),
	}
	for _, opt := range opts {
		opt(s)
	}
	// Apply defaults for any settings not explicitly configured
	if !s.ttlSet {
		s.ttl = DefaultTTL
	}
	if !s.maxEntriesSet {
		s.maxEntries = DefaultMaxEntries
	}
	if s.ttl > 0 && s.evictionInterval > 0 {
		s.stopCh = make(chan struct{})
		go s.backgroundEviction()
	}
	return s
}

// Close stops the background eviction goroutine, if running.
// It is safe to call Close multiple times.
func (s *MemoryStore) Close() {
	s.closeOnce.Do(func() {
		if s.stopCh != nil {
			close(s.stopCh)
		}
	})
}

// Load retrieves a conversation state by ID.
// Returns a deep copy to prevent external mutations.
// Expired entries are lazily evicted on access and return ErrNotFound.
func (s *MemoryStore) Load(ctx context.Context, id string) (*ConversationState, error) {
	if id == "" {
		return nil, ErrInvalidID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, exists := s.states[id]
	if !exists {
		return nil, ErrNotFound
	}

	if s.isExpired(state) {
		s.deleteStateLocked(id, state)
		return nil, ErrNotFound
	}

	now := time.Now()
	state.LastAccessedAt = now
	s.touchLRULocked(id, now)

	// Return a deep copy to prevent external mutations
	return deepCopyState(state), nil
}

// Save persists a conversation state. If it already exists, it will be updated.
// When a max entries limit is set and the store is full, the least-recently-accessed
// entry is evicted to make room.
func (s *MemoryStore) Save(ctx context.Context, state *ConversationState) error {
	if state == nil {
		return ErrInvalidState
	}
	if state.ID == "" {
		return ErrInvalidID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Evict if at capacity and this is a new entry
	if s.maxEntries > 0 {
		_, isUpdate := s.states[state.ID]
		if !isUpdate && len(s.states) >= s.maxEntries {
			s.evictLRULocked()
		}
	}

	// Store a deep copy to prevent external mutations
	stateCopy := deepCopyState(state)
	now := time.Now()
	stateCopy.LastAccessedAt = now

	// Update main storage
	s.states[state.ID] = stateCopy
	s.touchLRULocked(state.ID, now)

	// Update user index if UserID is set
	if state.UserID != "" {
		s.updateUserIndex(state.UserID, state.ID)
	}

	return nil
}

// Fork creates a copy of an existing conversation state with a new ID.
func (s *MemoryStore) Fork(ctx context.Context, sourceID, newID string) error {
	if sourceID == "" || newID == "" {
		return ErrInvalidID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Load source state
	source, exists := s.states[sourceID]
	if !exists {
		return ErrNotFound
	}

	if s.isExpired(source) {
		s.deleteStateLocked(sourceID, source)
		return ErrNotFound
	}

	// Evict if at capacity and this is a new entry
	if s.maxEntries > 0 {
		_, isUpdate := s.states[newID]
		if !isUpdate && len(s.states) >= s.maxEntries {
			s.evictLRULocked()
		}
	}

	// Deep copy the state
	forked := deepCopyState(source)
	forked.ID = newID
	now := time.Now()
	forked.LastAccessedAt = now

	// Store the forked state
	s.states[newID] = forked
	s.touchLRULocked(newID, now)

	// Update user index if UserID is set
	if forked.UserID != "" {
		s.updateUserIndex(forked.UserID, newID)
	}

	return nil
}

// Delete removes a conversation state by ID.
func (s *MemoryStore) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, exists := s.states[id]
	if !exists {
		return ErrNotFound
	}

	s.deleteStateLocked(id, state)

	return nil
}

// defaultListLimit is applied when ListOptions.Limit is zero.
const defaultListLimit = 100

// List returns conversation IDs matching the given criteria.
// Expired entries are excluded from results.
func (s *MemoryStore) List(ctx context.Context, opts ListOptions) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := s.filterExpiredIDs(s.listCandidateIDs(opts.UserID))

	if opts.SortBy != "" {
		s.sortConversations(ids, opts.SortBy, opts.SortOrder)
	}

	return paginate(ids, opts.Offset, opts.Limit), nil
}

// listCandidateIDs returns the full set of conversation IDs to consider for a
// List call. When userID is non-empty, the result is scoped to that user's
// index; otherwise every known conversation is included. Caller must hold
// at least the read lock.
func (s *MemoryStore) listCandidateIDs(userID string) []string {
	if userID != "" {
		userConvs, exists := s.userIndex[userID]
		if !exists {
			return nil
		}
		ids := make([]string, 0, len(userConvs))
		for id := range userConvs {
			ids = append(ids, id)
		}
		return ids
	}
	ids := make([]string, 0, len(s.states))
	for id := range s.states {
		ids = append(ids, id)
	}
	return ids
}

// filterExpiredIDs drops any IDs whose state has expired or is missing.
// Caller must hold at least the read lock.
func (s *MemoryStore) filterExpiredIDs(candidates []string) []string {
	alive := make([]string, 0, len(candidates))
	for _, id := range candidates {
		if state, ok := s.states[id]; ok && !s.isExpired(state) {
			alive = append(alive, id)
		}
	}
	return alive
}

// paginate applies offset/limit to a slice of IDs, substituting the default
// limit when zero and returning an empty slice once offset passes the end.
func paginate(ids []string, offset, limit int) []string {
	if limit == 0 {
		limit = defaultListLimit
	}
	if offset >= len(ids) {
		return []string{}
	}
	end := offset + limit
	if end > len(ids) {
		end = len(ids)
	}
	return ids[offset:end]
}

// LoadRecentMessages returns the last n messages for the given conversation.
// Expired entries return ErrNotFound.
func (s *MemoryStore) LoadRecentMessages(ctx context.Context, id string, n int) ([]types.Message, error) {
	if id == "" {
		return nil, ErrInvalidID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, exists := s.states[id]
	if !exists {
		return nil, ErrNotFound
	}

	if s.isExpired(state) {
		s.deleteStateLocked(id, state)
		return nil, ErrNotFound
	}

	now := time.Now()
	state.LastAccessedAt = now
	s.touchLRULocked(id, now)

	msgs := state.Messages
	if n >= len(msgs) {
		n = len(msgs)
	}
	start := len(msgs) - n

	// Deep copy only the requested slice using structural cloning
	return cloneMessages(msgs[start:]), nil
}

// MessageCount returns the total number of messages in the conversation.
// Uses a read lock since it only needs the message count, not a mutable reference.
// Expired entries return ErrNotFound but are not eagerly evicted under RLock;
// they will be cleaned up on the next write operation or background eviction.
func (s *MemoryStore) MessageCount(ctx context.Context, id string) (int, error) {
	if id == "" {
		return 0, ErrInvalidID
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	state, exists := s.states[id]
	if !exists {
		return 0, ErrNotFound
	}

	if s.isExpired(state) {
		return 0, ErrNotFound
	}

	return len(state.Messages), nil
}

// LoadMetadata returns just the metadata map for the given conversation.
// This avoids the cost of deep-copying the entire message history, making it
// significantly cheaper than Load() for callers that only need metadata.
// Expired entries are lazily evicted and return ErrNotFound.
func (s *MemoryStore) LoadMetadata(ctx context.Context, id string) (map[string]interface{}, error) {
	if id == "" {
		return nil, ErrInvalidID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, exists := s.states[id]
	if !exists {
		return nil, ErrNotFound
	}

	if s.isExpired(state) {
		s.deleteStateLocked(id, state)
		return nil, ErrNotFound
	}

	now := time.Now()
	state.LastAccessedAt = now
	s.touchLRULocked(id, now)

	// Only deep copy the metadata map, not the entire state
	return cloneMapStringInterface(state.Metadata), nil
}

// getOrCreateStateLocked returns the state for the given ID, creating it if needed.
// Caller must hold s.mu write lock.
func (s *MemoryStore) getOrCreateStateLocked(id string) *ConversationState {
	state, exists := s.states[id]
	if exists && s.isExpired(state) {
		s.deleteStateLocked(id, state)
		exists = false
	}
	if !exists {
		if s.maxEntries > 0 && len(s.states) >= s.maxEntries {
			s.evictLRULocked()
		}
		state = &ConversationState{
			ID:       id,
			Messages: make([]types.Message, 0),
			Metadata: make(map[string]interface{}),
		}
		s.states[id] = state
	}
	return state
}

// appendAndTouch clones messages, appends them, and updates LRU tracking.
// Caller must hold s.mu write lock.
func (s *MemoryStore) appendAndTouch(id string, state *ConversationState, messages []types.Message) {
	state.Messages = append(state.Messages, cloneMessages(messages)...)
	now := time.Now()
	state.LastAccessedAt = now
	s.touchLRULocked(id, now)
}

// AppendMessages appends messages to the conversation's message history.
// If the entry is expired, it is treated as non-existent and a new state is created.
func (s *MemoryStore) AppendMessages(ctx context.Context, id string, messages []types.Message) error {
	if id == "" {
		return ErrInvalidID
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.getOrCreateStateLocked(id)
	s.appendAndTouch(id, state, messages)
	return nil
}

// LogAppend appends messages with sequence-based idempotent deduplication.
// Messages before startSeq are already persisted and are skipped.
func (s *MemoryStore) LogAppend(ctx context.Context, id string, startSeq int, messages []types.Message) (int, error) {
	if id == "" {
		return 0, ErrInvalidID
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.getOrCreateStateLocked(id)
	currentLen := len(state.Messages)

	// Clamp startSeq to current length (handles gaps gracefully)
	if startSeq > currentLen {
		startSeq = currentLen
	}

	// Skip messages already persisted (idempotent deduplication)
	skip := currentLen - startSeq
	if skip >= len(messages) {
		return currentLen, nil
	}

	s.appendAndTouch(id, state, messages[skip:])
	return len(state.Messages), nil
}

// LogLoad returns messages for the conversation.
// If recent > 0, returns only the last N messages.
// Returns an empty slice if the conversation doesn't exist.
func (s *MemoryStore) LogLoad(ctx context.Context, id string, recent int) ([]types.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state, exists := s.states[id]
	if !exists || s.isExpired(state) {
		return nil, nil
	}

	msgs := state.Messages
	if recent > 0 && recent < len(msgs) {
		msgs = msgs[len(msgs)-recent:]
	}

	return cloneMessages(msgs), nil
}

// LogLen returns the total message count for the conversation.
// Returns 0 if the conversation doesn't exist.
func (s *MemoryStore) LogLen(ctx context.Context, id string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state, exists := s.states[id]
	if !exists || s.isExpired(state) {
		return 0, nil
	}

	return len(state.Messages), nil
}

// LoadSummaries returns all summaries for the given conversation.
// Uses a read lock since summaries contain only value types and cloning
// is a simple slice copy that doesn't benefit from write-lock protection.
// Expired entries return nil but are not eagerly evicted under RLock.
func (s *MemoryStore) LoadSummaries(ctx context.Context, id string) ([]Summary, error) {
	if id == "" {
		return nil, ErrInvalidID
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	state, exists := s.states[id]
	if !exists {
		return nil, nil
	}

	if s.isExpired(state) {
		return nil, nil
	}

	if len(state.Summaries) == 0 {
		return nil, nil
	}

	// Summary contains only value types (int, string, time.Time),
	// so a simple slice copy suffices for isolation.
	return cloneSummaries(state.Summaries), nil
}

// SaveSummary appends a summary to the conversation's summary list.
// Expired entries return ErrNotFound.
func (s *MemoryStore) SaveSummary(ctx context.Context, id string, summary Summary) error {
	if id == "" {
		return ErrInvalidID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, exists := s.states[id]
	if !exists {
		return ErrNotFound
	}

	if s.isExpired(state) {
		s.deleteStateLocked(id, state)
		return ErrNotFound
	}

	state.Summaries = append(state.Summaries, summary)
	now := time.Now()
	state.LastAccessedAt = now
	s.touchLRULocked(id, now)

	return nil
}

// Len returns the number of entries currently in the store, including expired entries
// that have not yet been evicted. This is primarily useful for testing.
func (s *MemoryStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.states)
}

// =============================================================================
// TTL and eviction helpers
// =============================================================================

// isExpired returns true if the entry has exceeded its TTL.
// A zero TTL means entries never expire.
func (s *MemoryStore) isExpired(state *ConversationState) bool {
	if s.ttl <= 0 {
		return false
	}
	return time.Since(state.LastAccessedAt) > s.ttl
}

// deleteStateLocked removes a conversation state and its index entries.
// Must be called with the write lock held.
func (s *MemoryStore) deleteStateLocked(id string, state *ConversationState) {
	if state.UserID != "" {
		s.removeFromUserIndex(state.UserID, id)
	}
	delete(s.states, id)

	// Remove from LRU heap
	if entry, ok := s.heapIndex[id]; ok {
		heap.Remove(&s.lruHeap, entry.index)
		delete(s.heapIndex, id)
	}
}

// touchLRULocked updates or inserts a key in the LRU heap with the given access time.
// Must be called with the write lock held.
func (s *MemoryStore) touchLRULocked(key string, accessTime time.Time) {
	if entry, ok := s.heapIndex[key]; ok {
		entry.lastAccess = accessTime
		heap.Fix(&s.lruHeap, entry.index)
	} else {
		entry = &accessEntry{key: key, lastAccess: accessTime}
		heap.Push(&s.lruHeap, entry)
		s.heapIndex[key] = entry
	}
}

// evictLRULocked removes the least-recently-accessed entry from the store.
// Uses a min-heap for O(log N) eviction instead of O(N) full scan.
// Must be called with the write lock held.
func (s *MemoryStore) evictLRULocked() {
	for s.lruHeap.Len() > 0 {
		entry := heap.Pop(&s.lruHeap).(*accessEntry)
		delete(s.heapIndex, entry.key)

		if state, ok := s.states[entry.key]; ok {
			if state.UserID != "" {
				s.removeFromUserIndex(state.UserID, entry.key)
			}
			delete(s.states, entry.key)
			logger.Debug("state store LRU eviction",
				"conversation_id", entry.key,
				"store_size", len(s.states))
			return
		}
		// Entry was already deleted from states (e.g., by TTL expiry); pop next.
	}
}

// collectExpiredKeys scans under RLock and returns the IDs of all expired entries.
func (s *MemoryStore) collectExpiredKeys() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var expired []string
	for id, state := range s.states {
		if s.isExpired(state) {
			expired = append(expired, id)
		}
	}
	return expired
}

// deleteExpiredKeys takes a write lock and deletes the given keys if they are still expired.
// Keys that were refreshed between the scan and delete phases are skipped.
func (s *MemoryStore) deleteExpiredKeys(keys []string) {
	if len(keys) == 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	evicted := 0
	for _, id := range keys {
		state, ok := s.states[id]
		if ok && s.isExpired(state) {
			s.deleteStateLocked(id, state)
			evicted++
		}
	}
	if evicted > 0 {
		logger.Debug("state store TTL eviction",
			"evicted", evicted, "store_size", len(s.states))
	}
}

// backgroundEviction runs periodic cleanup of expired entries.
// Uses a two-phase approach: RLock scan to collect expired keys, then write lock to delete.
// This allows concurrent reads during the scan phase.
func (s *MemoryStore) backgroundEviction() {
	ticker := time.NewTicker(s.evictionInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			expired := s.collectExpiredKeys()
			s.deleteExpiredKeys(expired)
		}
	}
}

// =============================================================================
// User index helpers (O(1) set-based)
// =============================================================================

// updateUserIndex adds a conversation ID to the user's index.
// Must be called with mutex locked.
func (s *MemoryStore) updateUserIndex(userID, convID string) {
	convs, exists := s.userIndex[userID]
	if !exists {
		s.userIndex[userID] = map[string]struct{}{convID: {}}
		return
	}
	convs[convID] = struct{}{}
}

// removeFromUserIndex removes a conversation ID from the user's index.
// Must be called with mutex locked.
func (s *MemoryStore) removeFromUserIndex(userID, convID string) {
	convs, exists := s.userIndex[userID]
	if !exists {
		return
	}

	delete(convs, convID)

	if len(convs) == 0 {
		delete(s.userIndex, userID)
	}
}

// sortConversations sorts conversation IDs based on the specified criteria.
// Must be called with read lock held.
func (s *MemoryStore) sortConversations(ids []string, sortBy, sortOrder string) {
	ascending := strings.EqualFold(sortOrder, "asc")

	sort.Slice(ids, func(i, j int) bool {
		state1, exists1 := s.states[ids[i]]
		state2, exists2 := s.states[ids[j]]

		if !exists1 || !exists2 {
			return false
		}

		var less bool
		switch sortBy {
		case sortFieldCreatedAt:
			// Use first message timestamp as created_at proxy
			t1 := getCreatedAt(state1)
			t2 := getCreatedAt(state2)
			less = t1.Before(t2)
		case sortFieldUpdatedAt, "":
			// Default: sort by last accessed
			less = state1.LastAccessedAt.Before(state2.LastAccessedAt)
		default:
			// Unknown sort field, no sorting
			return false
		}

		if ascending {
			return less
		}
		return !less
	})
}

// getCreatedAt extracts the creation time from a conversation state.
func getCreatedAt(state *ConversationState) time.Time {
	if len(state.Messages) > 0 {
		return state.Messages[0].Timestamp
	}
	return state.LastAccessedAt
}

// =============================================================================
// Structural deep copy helpers
//
// These replace the previous JSON marshal/unmarshal approach with type-aware
// structural cloning for significantly better performance. Each clone function
// handles a specific type, avoiding serialization overhead entirely.
// =============================================================================

// deepCopyState creates a deep copy of a conversation state using structural cloning.
// This avoids the overhead of JSON marshal/unmarshal while preserving full copy semantics.
func deepCopyState(state *ConversationState) *ConversationState {
	if state == nil {
		return nil
	}

	return &ConversationState{
		ID:             state.ID,
		UserID:         state.UserID,
		Messages:       cloneMessages(state.Messages),
		SystemPrompt:   state.SystemPrompt,
		Summaries:      cloneSummaries(state.Summaries),
		TokenCount:     state.TokenCount,
		LastAccessedAt: state.LastAccessedAt,
		Metadata:       cloneMapStringInterface(state.Metadata),
	}
}

// cloneRawMessage returns a deep copy of a json.RawMessage.
func cloneRawMessage(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}
	cp := make(json.RawMessage, len(raw))
	copy(cp, raw)
	return cp
}

// cloneMapStringInterface returns a deep copy of a map[string]interface{}.
// It handles nested maps, slices, and primitive values.
func cloneMapStringInterface(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	cp := make(map[string]interface{}, len(m))
	for k, v := range m {
		cp[k] = cloneInterface(v)
	}
	return cp
}

// cloneInterface deep copies an arbitrary interface{} value.
func cloneInterface(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		return cloneMapStringInterface(val)
	case []interface{}:
		cp := make([]interface{}, len(val))
		for i, item := range val {
			cp[i] = cloneInterface(item)
		}
		return cp
	case []string:
		cp := make([]string, len(val))
		copy(cp, val)
		return cp
	default:
		// Primitive types (string, bool, float64, int, nil, etc.) are immutable
		return val
	}
}

// batchCopyStringPtrs copies non-nil *string values using a single slab allocation.
// Each source/dest pair is matched by index. Nil sources are skipped, leaving the dest nil.
func batchCopyStringPtrs(dests []**string, srcs []*string) {
	count := 0
	for _, s := range srcs {
		if s != nil {
			count++
		}
	}
	if count == 0 {
		return
	}
	slab := make([]string, count)
	idx := 0
	for i, s := range srcs {
		if s != nil {
			slab[idx] = *s
			*dests[i] = &slab[idx]
			idx++
		}
	}
}

// batchCopyIntPtrs copies non-nil *int values using a single slab allocation.
func batchCopyIntPtrs(dests []**int, srcs []*int) {
	count := 0
	for _, p := range srcs {
		if p != nil {
			count++
		}
	}
	if count == 0 {
		return
	}
	slab := make([]int, count)
	idx := 0
	for i, p := range srcs {
		if p != nil {
			slab[idx] = *p
			*dests[i] = &slab[idx]
			idx++
		}
	}
}

// cloneMediaContent returns a deep copy of a *types.MediaContent.
// It batch-allocates pointer storage to reduce the number of heap allocations.
// Strings are immutable in Go, so only the pointer (not the underlying bytes) needs copying.
func cloneMediaContent(mc *types.MediaContent) *types.MediaContent {
	if mc == nil {
		return nil
	}

	cp := &types.MediaContent{
		MIMEType: mc.MIMEType,
	}

	// Batch-allocate and copy string pointers (reduces up to 8 allocs to 1)
	batchCopyStringPtrs(
		[]**string{&cp.Data, &cp.FilePath, &cp.URL, &cp.StorageReference,
			&cp.Format, &cp.Detail, &cp.Caption, &cp.PolicyName},
		[]*string{mc.Data, mc.FilePath, mc.URL, mc.StorageReference,
			mc.Format, mc.Detail, mc.Caption, mc.PolicyName},
	)

	// Batch-allocate and copy int pointers (reduces up to 6 allocs to 1)
	batchCopyIntPtrs(
		[]**int{&cp.Duration, &cp.BitRate, &cp.Channels, &cp.Width, &cp.Height, &cp.FPS},
		[]*int{mc.Duration, mc.BitRate, mc.Channels, mc.Width, mc.Height, mc.FPS},
	)

	// int64 pointer (only SizeKB)
	if mc.SizeKB != nil {
		v := *mc.SizeKB
		cp.SizeKB = &v
	}

	return cp
}

// cloneContentPart returns a deep copy of a types.ContentPart.
// For text-only parts (no Media), this avoids the cloneMediaContent call entirely.
func cloneContentPart(cp *types.ContentPart) types.ContentPart {
	result := types.ContentPart{
		Type: cp.Type,
	}
	if cp.Text != nil {
		v := *cp.Text
		result.Text = &v
	}
	if cp.Media != nil {
		result.Media = cloneMediaContent(cp.Media)
	}
	return result
}

// cloneToolCall returns a deep copy of a types.MessageToolCall.
func cloneToolCall(tc *types.MessageToolCall) types.MessageToolCall {
	return types.MessageToolCall{
		ID:   tc.ID,
		Name: tc.Name,
		Args: cloneRawMessage(tc.Args),
	}
}

// cloneValidationResult returns a deep copy of a types.ValidationResult.
func cloneValidationResult(vr *types.ValidationResult) types.ValidationResult {
	return types.ValidationResult{
		ValidatorType: vr.ValidatorType,
		Passed:        vr.Passed,
		Details:       cloneMapStringInterface(vr.Details),
		Timestamp:     vr.Timestamp,
	}
}

// cloneMessage returns a deep copy of a types.Message.
// String fields (Role, Content, Source) are assigned directly since Go strings are immutable.
// Only reference types (slices, maps, pointers) need deep copying for isolation.
func cloneMessage(msg *types.Message) types.Message {
	cp := types.Message{
		Role:      msg.Role,
		Content:   msg.Content,
		Source:    msg.Source,
		Timestamp: msg.Timestamp,
		LatencyMs: msg.LatencyMs,
	}

	// Only clone reference-type fields when non-nil to avoid unnecessary allocations
	if msg.CostInfo != nil {
		v := *msg.CostInfo
		cp.CostInfo = &v
	}
	if msg.ToolResult != nil {
		cp.ToolResult = cloneToolResult(msg.ToolResult)
	}
	if len(msg.Meta) > 0 {
		cp.Meta = cloneMapStringInterface(msg.Meta)
	}
	if len(msg.Parts) > 0 {
		cp.Parts = make([]types.ContentPart, len(msg.Parts))
		for i := range msg.Parts {
			cp.Parts[i] = cloneContentPart(&msg.Parts[i])
		}
	}
	if len(msg.ToolCalls) > 0 {
		cp.ToolCalls = make([]types.MessageToolCall, len(msg.ToolCalls))
		for i := range msg.ToolCalls {
			cp.ToolCalls[i] = cloneToolCall(&msg.ToolCalls[i])
		}
	}
	if len(msg.Validations) > 0 {
		cp.Validations = make([]types.ValidationResult, len(msg.Validations))
		for i := range msg.Validations {
			cp.Validations[i] = cloneValidationResult(&msg.Validations[i])
		}
	}
	return cp
}

// cloneToolResult returns a deep copy of a MessageToolResult, including its Parts slice.
func cloneToolResult(tr *types.MessageToolResult) *types.MessageToolResult {
	v := *tr
	if len(v.Parts) > 0 {
		v.Parts = make([]types.ContentPart, len(tr.Parts))
		for i := range tr.Parts {
			v.Parts[i] = cloneContentPart(&tr.Parts[i])
		}
	}
	return &v
}

// cloneMessages returns a deep copy of a slice of types.Message.
func cloneMessages(msgs []types.Message) []types.Message {
	if msgs == nil {
		return nil
	}
	cp := make([]types.Message, len(msgs))
	for i := range msgs {
		cp[i] = cloneMessage(&msgs[i])
	}
	return cp
}

// cloneSummaries returns a deep copy of a slice of Summary.
// Summary contains only value types (int, string, time.Time), so a simple copy suffices.
func cloneSummaries(summaries []Summary) []Summary {
	if summaries == nil {
		return nil
	}
	cp := make([]Summary, len(summaries))
	copy(cp, summaries)
	return cp
}
