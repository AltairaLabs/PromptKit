package statestore

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Sort field constants for conversation ordering.
const (
	sortFieldCreatedAt = "created_at"
	sortFieldUpdatedAt = "updated_at"
)

// MemoryStoreOption configures optional behavior for MemoryStore.
type MemoryStoreOption func(*MemoryStore)

// WithMemoryTTL sets the time-to-live for conversation states. Entries that have not
// been accessed within the TTL are considered expired and eligible for eviction.
// A zero or negative TTL means entries never expire (the default).
func WithMemoryTTL(ttl time.Duration) MemoryStoreOption {
	return func(s *MemoryStore) {
		s.ttl = ttl
	}
}

// WithMemoryMaxEntries sets the maximum number of entries the store will hold.
// When the limit is reached, the least-recently-accessed entry is evicted.
// A zero value means no limit (the default).
func WithMemoryMaxEntries(n int) MemoryStoreOption {
	return func(s *MemoryStore) {
		if n > 0 {
			s.maxEntries = n
		}
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

	// TTL and eviction settings
	ttl              time.Duration // zero means no expiry
	maxEntries       int           // zero means no limit
	evictionInterval time.Duration // zero means no background cleanup

	// Background cleanup
	stopCh chan struct{} // closed to signal the cleanup goroutine to stop
}

// NewMemoryStore creates a new in-memory state store.
// Options can be provided to configure TTL, max entries, and background eviction.
func NewMemoryStore(opts ...MemoryStoreOption) *MemoryStore {
	s := &MemoryStore{
		states:    make(map[string]*ConversationState),
		userIndex: make(map[string]map[string]struct{}),
	}
	for _, opt := range opts {
		opt(s)
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
	if s.stopCh != nil {
		select {
		case <-s.stopCh:
			// already closed
		default:
			close(s.stopCh)
		}
	}
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

	state.LastAccessedAt = time.Now()

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
	stateCopy.LastAccessedAt = time.Now()

	// Update main storage
	s.states[state.ID] = stateCopy

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
	forked.LastAccessedAt = time.Now()

	// Store the forked state
	s.states[newID] = forked

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

// List returns conversation IDs matching the given criteria.
// Expired entries are excluded from results.
func (s *MemoryStore) List(ctx context.Context, opts ListOptions) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Collect matching conversation IDs
	var candidates []string
	if opts.UserID != "" {
		// Filter by user
		userConvs, exists := s.userIndex[opts.UserID]
		if !exists {
			return []string{}, nil
		}
		candidates = make([]string, 0, len(userConvs))
		for id := range userConvs {
			candidates = append(candidates, id)
		}
	} else {
		// Return all conversations
		candidates = make([]string, 0, len(s.states))
		for id := range s.states {
			candidates = append(candidates, id)
		}
	}

	// Filter out expired entries
	ids := make([]string, 0, len(candidates))
	for _, id := range candidates {
		if state, ok := s.states[id]; ok && !s.isExpired(state) {
			ids = append(ids, id)
		}
	}

	// Sort if requested
	if opts.SortBy != "" {
		s.sortConversations(ids, opts.SortBy, opts.SortOrder)
	}

	// Apply pagination
	limit := opts.Limit
	if limit == 0 {
		limit = 100 // Default limit
	}

	start := opts.Offset
	if start >= len(ids) {
		return []string{}, nil
	}

	end := start + limit
	if end > len(ids) {
		end = len(ids)
	}

	return ids[start:end], nil
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

	state.LastAccessedAt = time.Now()

	msgs := state.Messages
	if n >= len(msgs) {
		n = len(msgs)
	}
	start := len(msgs) - n

	// Deep copy only the requested slice using structural cloning
	return cloneMessages(msgs[start:]), nil
}

// MessageCount returns the total number of messages in the conversation.
// Expired entries return ErrNotFound.
func (s *MemoryStore) MessageCount(ctx context.Context, id string) (int, error) {
	if id == "" {
		return 0, ErrInvalidID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, exists := s.states[id]
	if !exists {
		return 0, ErrNotFound
	}

	if s.isExpired(state) {
		s.deleteStateLocked(id, state)
		return 0, ErrNotFound
	}

	state.LastAccessedAt = time.Now()

	return len(state.Messages), nil
}

// AppendMessages appends messages to the conversation's message history.
// If the entry is expired, it is treated as non-existent and a new state is created.
func (s *MemoryStore) AppendMessages(ctx context.Context, id string, messages []types.Message) error {
	if id == "" {
		return ErrInvalidID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, exists := s.states[id]
	if exists && s.isExpired(state) {
		s.deleteStateLocked(id, state)
		exists = false
	}

	if !exists {
		// Evict if at capacity
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

	// Deep copy new messages before appending using structural cloning
	state.Messages = append(state.Messages, cloneMessages(messages)...)
	state.LastAccessedAt = time.Now()

	return nil
}

// LoadSummaries returns all summaries for the given conversation.
// Expired entries return nil.
func (s *MemoryStore) LoadSummaries(ctx context.Context, id string) ([]Summary, error) {
	if id == "" {
		return nil, ErrInvalidID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, exists := s.states[id]
	if !exists {
		return nil, nil
	}

	if s.isExpired(state) {
		s.deleteStateLocked(id, state)
		return nil, nil
	}

	state.LastAccessedAt = time.Now()

	if len(state.Summaries) == 0 {
		return nil, nil
	}

	// Deep copy summaries using structural cloning
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
	state.LastAccessedAt = time.Now()

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
}

// evictLRULocked removes the least-recently-accessed entry from the store.
// Must be called with the write lock held.
func (s *MemoryStore) evictLRULocked() {
	var oldestID string
	var oldestTime time.Time
	first := true

	for id, state := range s.states {
		if first || state.LastAccessedAt.Before(oldestTime) {
			oldestID = id
			oldestTime = state.LastAccessedAt
			first = false
		}
	}

	if !first {
		if state, ok := s.states[oldestID]; ok {
			s.deleteStateLocked(oldestID, state)
		}
	}
}

// evictExpiredLocked removes all expired entries from the store.
// Must be called with the write lock held.
func (s *MemoryStore) evictExpiredLocked() {
	for id, state := range s.states {
		if s.isExpired(state) {
			s.deleteStateLocked(id, state)
		}
	}
}

// backgroundEviction runs periodic cleanup of expired entries.
func (s *MemoryStore) backgroundEviction() {
	ticker := time.NewTicker(s.evictionInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.mu.Lock()
			s.evictExpiredLocked()
			s.mu.Unlock()
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

// cloneStringPtr returns a deep copy of a *string.
func cloneStringPtr(s *string) *string {
	if s == nil {
		return nil
	}
	v := *s
	return &v
}

// cloneIntPtr returns a deep copy of a *int.
func cloneIntPtr(p *int) *int {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// cloneInt64Ptr returns a deep copy of a *int64.
func cloneInt64Ptr(p *int64) *int64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
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

// cloneMediaContent returns a deep copy of a *types.MediaContent.
func cloneMediaContent(mc *types.MediaContent) *types.MediaContent {
	if mc == nil {
		return nil
	}
	return &types.MediaContent{
		Data:             cloneStringPtr(mc.Data),
		FilePath:         cloneStringPtr(mc.FilePath),
		URL:              cloneStringPtr(mc.URL),
		StorageReference: cloneStringPtr(mc.StorageReference),
		MIMEType:         mc.MIMEType,
		Format:           cloneStringPtr(mc.Format),
		SizeKB:           cloneInt64Ptr(mc.SizeKB),
		Detail:           cloneStringPtr(mc.Detail),
		Caption:          cloneStringPtr(mc.Caption),
		Duration:         cloneIntPtr(mc.Duration),
		BitRate:          cloneIntPtr(mc.BitRate),
		Channels:         cloneIntPtr(mc.Channels),
		Width:            cloneIntPtr(mc.Width),
		Height:           cloneIntPtr(mc.Height),
		FPS:              cloneIntPtr(mc.FPS),
		PolicyName:       cloneStringPtr(mc.PolicyName),
	}
}

// cloneContentPart returns a deep copy of a types.ContentPart.
func cloneContentPart(cp *types.ContentPart) types.ContentPart {
	return types.ContentPart{
		Type:  cp.Type,
		Text:  cloneStringPtr(cp.Text),
		Media: cloneMediaContent(cp.Media),
	}
}

// cloneToolCall returns a deep copy of a types.MessageToolCall.
func cloneToolCall(tc *types.MessageToolCall) types.MessageToolCall {
	return types.MessageToolCall{
		ID:   tc.ID,
		Name: tc.Name,
		Args: cloneRawMessage(tc.Args),
	}
}

// cloneToolResult returns a deep copy of a *types.MessageToolResult.
func cloneToolResult(tr *types.MessageToolResult) *types.MessageToolResult {
	if tr == nil {
		return nil
	}
	cp := *tr
	return &cp
}

// cloneCostInfo returns a deep copy of a *types.CostInfo.
func cloneCostInfo(ci *types.CostInfo) *types.CostInfo {
	if ci == nil {
		return nil
	}
	cp := *ci
	return &cp
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
func cloneMessage(msg *types.Message) types.Message {
	cp := types.Message{
		Role:       msg.Role,
		Content:    msg.Content,
		Source:     msg.Source,
		Timestamp:  msg.Timestamp,
		LatencyMs:  msg.LatencyMs,
		CostInfo:   cloneCostInfo(msg.CostInfo),
		ToolResult: cloneToolResult(msg.ToolResult),
		Meta:       cloneMapStringInterface(msg.Meta),
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
