package statestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisStore provides a Redis-backed implementation of the Store interface.
// It uses JSON serialization for state storage and supports automatic TTL-based cleanup.
// This implementation is suitable for distributed systems and production deployments.
type RedisStore struct {
	client *redis.Client
	ttl    time.Duration
	prefix string
}

// RedisOption configures a RedisStore.
type RedisOption func(*RedisStore)

// WithTTL sets the time-to-live for conversation states.
// After this duration, conversations will be automatically deleted.
// Default is 24 hours. Set to 0 for no expiration.
func WithTTL(ttl time.Duration) RedisOption {
	return func(s *RedisStore) {
		s.ttl = ttl
	}
}

// WithPrefix sets the key prefix for Redis keys.
// Default is "promptkit".
func WithPrefix(prefix string) RedisOption {
	return func(s *RedisStore) {
		s.prefix = prefix
	}
}

// NewRedisStore creates a new Redis-backed state store.
//
// Example:
//
//	store := NewRedisStore(
//	    redis.NewClient(&redis.Options{Addr: "localhost:6379"}),
//	    WithTTL(24 * time.Hour),
//	    WithPrefix("myapp"),
//	)
func NewRedisStore(client *redis.Client, opts ...RedisOption) *RedisStore {
	store := &RedisStore{
		client: client,
		ttl:    24 * time.Hour, // Default TTL
		prefix: "promptkit",    // Default prefix
	}

	for _, opt := range opts {
		opt(store)
	}

	return store
}

// Load retrieves a conversation state by ID from Redis.
func (s *RedisStore) Load(ctx context.Context, id string) (*ConversationState, error) {
	if id == "" {
		return nil, ErrInvalidID
	}

	key := s.conversationKey(id)
	data, err := s.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("redis get failed: %w", err)
	}

	var state ConversationState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", err)
	}

	return &state, nil
}

// Save persists a conversation state to Redis with TTL.
func (s *RedisStore) Save(ctx context.Context, state *ConversationState) error {
	if state == nil {
		return ErrInvalidState
	}
	if state.ID == "" {
		return ErrInvalidID
	}

	// Update timestamp
	state.LastAccessedAt = time.Now()

	// Serialize to JSON
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// Save to Redis with TTL
	key := s.conversationKey(state.ID)
	if err := s.client.Set(ctx, key, data, s.ttl).Err(); err != nil {
		return fmt.Errorf("redis set failed: %w", err)
	}

	// Update user index if UserID is set
	if state.UserID != "" {
		if err := s.updateUserIndex(ctx, state.UserID, state.ID); err != nil {
			return fmt.Errorf("failed to update user index: %w", err)
		}
	}

	return nil
}

// Delete removes a conversation state from Redis.
func (s *RedisStore) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidID
	}

	// Load state to get UserID for index cleanup
	state, err := s.Load(ctx, id)
	if err != nil {
		return err
	}

	// Remove from user index
	if state.UserID != "" {
		if err := s.removeFromUserIndex(ctx, state.UserID, id); err != nil {
			return fmt.Errorf("failed to remove from user index: %w", err)
		}
	}

	// Delete conversation key
	key := s.conversationKey(id)
	deleted, err := s.client.Del(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("redis del failed: %w", err)
	}

	if deleted == 0 {
		return ErrNotFound
	}

	return nil
}

// List returns conversation IDs matching the given criteria.
func (s *RedisStore) List(ctx context.Context, opts ListOptions) ([]string, error) {
	ids, err := s.fetchConversationIDs(ctx, opts.UserID)
	if err != nil {
		return nil, err
	}

	// If sorting is requested, we need to load states to sort them
	if opts.SortBy != "" {
		if err := s.sortConversations(ctx, ids, opts.SortBy, opts.SortOrder); err != nil {
			return nil, fmt.Errorf("failed to sort conversations: %w", err)
		}
	}

	return s.applyPagination(ids, opts.Offset, opts.Limit), nil
}

// fetchConversationIDs retrieves conversation IDs for a user or all conversations
func (s *RedisStore) fetchConversationIDs(ctx context.Context, userID string) ([]string, error) {
	if userID != "" {
		return s.fetchUserConversations(ctx, userID)
	}
	return s.scanAllConversations(ctx)
}

// fetchUserConversations gets conversations for a specific user from the index
func (s *RedisStore) fetchUserConversations(ctx context.Context, userID string) ([]string, error) {
	indexKey := s.userIndexKey(userID)
	members, err := s.client.SMembers(ctx, indexKey).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("redis smembers failed: %w", err)
	}
	return members, nil
}

// scanAllConversations scans all conversation keys in Redis
func (s *RedisStore) scanAllConversations(ctx context.Context) ([]string, error) {
	var ids []string
	pattern := s.conversationKey("*")
	iter := s.client.Scan(ctx, 0, pattern, 0).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		id := s.extractIDFromKey(key)
		if id != "" {
			ids = append(ids, id)
		}
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("redis scan failed: %w", err)
	}
	return ids, nil
}

// applyPagination applies offset and limit to the conversation ID list
func (s *RedisStore) applyPagination(ids []string, offset, limit int) []string {
	if limit == 0 {
		limit = 100 // Default limit
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

// conversationKey generates the Redis key for a conversation.
func (s *RedisStore) conversationKey(id string) string {
	return fmt.Sprintf("%s:conversation:%s", s.prefix, id)
}

// userIndexKey generates the Redis key for a user's conversation index.
func (s *RedisStore) userIndexKey(userID string) string {
	return fmt.Sprintf("%s:user:%s:conversations", s.prefix, userID)
}

// updateUserIndex adds a conversation ID to the user's index (Redis Set).
func (s *RedisStore) updateUserIndex(ctx context.Context, userID, convID string) error {
	indexKey := s.userIndexKey(userID)
	if err := s.client.SAdd(ctx, indexKey, convID).Err(); err != nil {
		return fmt.Errorf("redis sadd failed: %w", err)
	}

	// Set TTL on index key (same as conversation TTL)
	if s.ttl > 0 {
		if err := s.client.Expire(ctx, indexKey, s.ttl).Err(); err != nil {
			return fmt.Errorf("redis expire failed: %w", err)
		}
	}

	return nil
}

// removeFromUserIndex removes a conversation ID from the user's index.
func (s *RedisStore) removeFromUserIndex(ctx context.Context, userID, convID string) error {
	indexKey := s.userIndexKey(userID)
	if err := s.client.SRem(ctx, indexKey, convID).Err(); err != nil {
		return fmt.Errorf("redis srem failed: %w", err)
	}
	return nil
}

// extractIDFromKey extracts the conversation ID from a Redis key.
func (s *RedisStore) extractIDFromKey(key string) string {
	prefix := s.conversationKey("")
	if strings.HasPrefix(key, prefix) {
		return strings.TrimPrefix(key, prefix)
	}
	return ""
}

// sortConversations sorts conversation IDs by loading their states.
// This is less efficient than in-memory sorting but necessary for Redis.
func (s *RedisStore) sortConversations(ctx context.Context, ids []string, sortBy, sortOrder string) error {
	type stateWithID struct {
		id    string
		state *ConversationState
	}

	// Load all states
	states := make([]stateWithID, 0, len(ids))
	for _, id := range ids {
		state, err := s.Load(ctx, id)
		if err != nil {
			// Skip conversations that no longer exist
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return err
		}
		states = append(states, stateWithID{id: id, state: state})
	}

	ascending := strings.EqualFold(sortOrder, "asc")

	// Sort states
	sort.Slice(states, func(i, j int) bool {
		var less bool
		switch sortBy {
		case "created_at":
			t1 := getCreatedAt(states[i].state)
			t2 := getCreatedAt(states[j].state)
			less = t1.Before(t2)
		case "updated_at", "":
			less = states[i].state.LastAccessedAt.Before(states[j].state.LastAccessedAt)
		default:
			return false
		}

		if ascending {
			return less
		}
		return !less
	})

	// Update ids slice with sorted order
	for i, s := range states {
		ids[i] = s.id
	}

	return nil
}
