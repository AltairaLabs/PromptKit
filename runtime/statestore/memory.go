package statestore

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"
)

// MemoryStore provides an in-memory implementation of the Store interface.
// It is thread-safe and suitable for development, testing, and single-instance deployments.
// For distributed systems, use RedisStore or a database-backed implementation.
type MemoryStore struct {
	mu     sync.RWMutex
	states map[string]*ConversationState

	// Index for efficient user-based lookups
	userIndex map[string][]string // userID -> []conversationID
}

// NewMemoryStore creates a new in-memory state store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		states:    make(map[string]*ConversationState),
		userIndex: make(map[string][]string),
	}
}

// Load retrieves a conversation state by ID.
// Returns a deep copy to prevent external mutations.
func (s *MemoryStore) Load(ctx context.Context, id string) (*ConversationState, error) {
	if id == "" {
		return nil, ErrInvalidID
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	state, exists := s.states[id]
	if !exists {
		return nil, ErrNotFound
	}

	// Return a deep copy to prevent external mutations
	return deepCopyState(state), nil
}

// Save persists a conversation state. If it already exists, it will be updated.
func (s *MemoryStore) Save(ctx context.Context, state *ConversationState) error {
	if state == nil {
		return ErrInvalidState
	}
	if state.ID == "" {
		return ErrInvalidID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

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

	// Remove from user index
	if state.UserID != "" {
		s.removeFromUserIndex(state.UserID, id)
	}

	// Remove from main storage
	delete(s.states, id)

	return nil
}

// List returns conversation IDs matching the given criteria.
func (s *MemoryStore) List(ctx context.Context, opts ListOptions) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Collect matching conversation IDs
	var ids []string
	if opts.UserID != "" {
		// Filter by user
		userConvs, exists := s.userIndex[opts.UserID]
		if !exists {
			return []string{}, nil
		}
		ids = make([]string, len(userConvs))
		copy(ids, userConvs)
	} else {
		// Return all conversations
		ids = make([]string, 0, len(s.states))
		for id := range s.states {
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

// updateUserIndex adds a conversation ID to the user's index.
// Must be called with mutex locked.
func (s *MemoryStore) updateUserIndex(userID, convID string) {
	convs, exists := s.userIndex[userID]
	if !exists {
		s.userIndex[userID] = []string{convID}
		return
	}

	// Check if already indexed
	for _, id := range convs {
		if id == convID {
			return
		}
	}

	// Add to index
	s.userIndex[userID] = append(convs, convID)
}

// removeFromUserIndex removes a conversation ID from the user's index.
// Must be called with mutex locked.
func (s *MemoryStore) removeFromUserIndex(userID, convID string) {
	convs, exists := s.userIndex[userID]
	if !exists {
		return
	}

	// Remove conversation ID
	filtered := make([]string, 0, len(convs))
	for _, id := range convs {
		if id != convID {
			filtered = append(filtered, id)
		}
	}

	if len(filtered) == 0 {
		delete(s.userIndex, userID)
	} else {
		s.userIndex[userID] = filtered
	}
}

// sortConversations sorts conversation IDs based on the specified criteria.
// Must be called with read lock held.
func (s *MemoryStore) sortConversations(ids []string, sortBy, sortOrder string) {
	ascending := strings.ToLower(sortOrder) == "asc"

	sort.Slice(ids, func(i, j int) bool {
		state1, exists1 := s.states[ids[i]]
		state2, exists2 := s.states[ids[j]]

		if !exists1 || !exists2 {
			return false
		}

		var less bool
		switch sortBy {
		case "created_at":
			// Use first message timestamp as created_at proxy
			t1 := getCreatedAt(state1)
			t2 := getCreatedAt(state2)
			less = t1.Before(t2)
		case "updated_at", "":
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

// deepCopyState creates a deep copy of a conversation state.
func deepCopyState(state *ConversationState) *ConversationState {
	if state == nil {
		return nil
	}

	// Use JSON marshaling for deep copy (simple and reliable)
	data, err := json.Marshal(state)
	if err != nil {
		// This should never happen with valid state
		return nil
	}

	var stateCopy ConversationState
	if err := json.Unmarshal(data, &stateCopy); err != nil {
		return nil
	}

	return &stateCopy
}
