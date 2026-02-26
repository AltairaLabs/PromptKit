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

	"github.com/AltairaLabs/PromptKit/runtime/types"
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
		ttl:    defaultTTLHours * time.Hour, // Default TTL
		prefix: "promptkit",                 // Default prefix
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
// Uses a pipeline to batch the SET and optional user index update into a single round-trip.
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

	// Pipeline: SET conversation + optional SAdd/Expire for user index
	key := s.conversationKey(state.ID)
	pipe := s.client.Pipeline()
	pipe.Set(ctx, key, data, s.ttl)

	if state.UserID != "" {
		indexKey := s.userIndexKey(state.UserID)
		pipe.SAdd(ctx, indexKey, state.ID)
		if s.ttl > 0 {
			pipe.Expire(ctx, indexKey, s.ttl)
		}
	}

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis pipeline failed: %w", err)
	}

	return nil
}

// Fork creates a copy of an existing conversation state with a new ID.
func (s *RedisStore) Fork(ctx context.Context, sourceID, newID string) error {
	if sourceID == "" || newID == "" {
		return ErrInvalidID
	}

	// Load the source state
	source, err := s.Load(ctx, sourceID)
	if err != nil {
		return err
	}

	// Create forked state with new ID
	source.ID = newID
	source.LastAccessedAt = time.Now()

	// Save the forked state
	return s.Save(ctx, source)
}

// Delete removes a conversation state from Redis.
// Uses a pipeline to batch the DEL and optional user index cleanup.
func (s *RedisStore) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidID
	}

	// Load state to get UserID for index cleanup
	state, err := s.Load(ctx, id)
	if err != nil {
		return err
	}

	// Pipeline: DEL conversation key + optional SRem from user index
	key := s.conversationKey(id)
	pipe := s.client.Pipeline()
	delCmd := pipe.Del(ctx, key)

	if state.UserID != "" {
		indexKey := s.userIndexKey(state.UserID)
		pipe.SRem(ctx, indexKey, id)
	}

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis pipeline failed: %w", err)
	}

	if delCmd.Val() == 0 {
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

// messagesKey generates the Redis key for a conversation's message list.
func (s *RedisStore) messagesKey(id string) string {
	return fmt.Sprintf("%s:conversation:%s:messages", s.prefix, id)
}

// metaKey generates the Redis key for a conversation's metadata.
func (s *RedisStore) metaKey(id string) string {
	return fmt.Sprintf("%s:conversation:%s:meta", s.prefix, id)
}

// summariesKey generates the Redis key for a conversation's summaries list.
func (s *RedisStore) summariesKey(id string) string {
	return fmt.Sprintf("%s:conversation:%s:summaries", s.prefix, id)
}

// LoadRecentMessages returns the last n messages using LRANGE on the messages list.
// Falls back to loading from the monolithic key if the list doesn't exist.
func (s *RedisStore) LoadRecentMessages(ctx context.Context, id string, n int) ([]types.Message, error) {
	if id == "" {
		return nil, ErrInvalidID
	}

	key := s.messagesKey(id)
	count, err := s.client.LLen(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("redis llen failed: %w", err)
	}

	// Fall back to monolithic key if list doesn't exist
	if count == 0 {
		return s.loadRecentFromMonolithic(ctx, id, n)
	}

	// Use LRANGE with negative indices to get last n elements
	vals, err := s.client.LRange(ctx, key, int64(-n), -1).Result()
	if err != nil {
		return nil, fmt.Errorf("redis lrange failed: %w", err)
	}

	messages := make([]types.Message, 0, len(vals))
	for _, v := range vals {
		var msg types.Message
		if err := json.Unmarshal([]byte(v), &msg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal message: %w", err)
		}
		messages = append(messages, msg)
	}

	return messages, nil
}

// MessageCount returns the total number of messages.
// Falls back to loading from the monolithic key if the list doesn't exist.
func (s *RedisStore) MessageCount(ctx context.Context, id string) (int, error) {
	if id == "" {
		return 0, ErrInvalidID
	}

	key := s.messagesKey(id)
	count, err := s.client.LLen(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("redis llen failed: %w", err)
	}

	// Fall back to monolithic key if list doesn't exist
	if count == 0 {
		state, err := s.Load(ctx, id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return 0, ErrNotFound
			}
			return 0, err
		}
		return len(state.Messages), nil
	}

	return int(count), nil
}

// AppendMessages appends messages to the conversation's message list using RPUSH.
// Uses Redis pipelining to batch the RPUSH, EXPIRE, and meta update in a single round-trip.
func (s *RedisStore) AppendMessages(ctx context.Context, id string, messages []types.Message) error {
	if id == "" {
		return ErrInvalidID
	}

	if err := s.ensureListFormat(ctx, id); err != nil {
		return err
	}

	key := s.messagesKey(id)
	if err := s.rpushMessages(ctx, key, messages); err != nil {
		return err
	}

	return s.updateMetaTTL(ctx, id, key)
}

// ensureListFormat migrates from monolithic key to list format if needed.
// Uses a pipeline to check both keys in a single round-trip.
func (s *RedisStore) ensureListFormat(ctx context.Context, id string) error {
	key := s.messagesKey(id)
	monoKey := s.conversationKey(id)

	// Pipeline both EXISTS checks into one round-trip
	pipe := s.client.Pipeline()
	listExistsCmd := pipe.Exists(ctx, key)
	monoExistsCmd := pipe.Exists(ctx, monoKey)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis pipeline failed: %w", err)
	}

	if listExistsCmd.Val() > 0 {
		return nil
	}
	if monoExistsCmd.Val() > 0 {
		if err := s.migrateToListFormat(ctx, id); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}
	return nil
}

// rpushMessages pushes messages to a Redis list key using a single batched RPUSH.
// All messages are serialized and sent in one command, reducing round-trips from N to 1.
func (s *RedisStore) rpushMessages(ctx context.Context, key string, messages []types.Message) error {
	if len(messages) == 0 {
		return nil
	}

	vals := make([]interface{}, 0, len(messages))
	for i := range messages {
		data, err := json.Marshal(&messages[i])
		if err != nil {
			return fmt.Errorf("failed to marshal message: %w", err)
		}
		vals = append(vals, data)
	}

	if err := s.client.RPush(ctx, key, vals...).Err(); err != nil {
		return fmt.Errorf("redis rpush failed: %w", err)
	}
	return nil
}

// updateMetaTTL updates TTL and last accessed time.
// Uses a pipeline to batch EXPIRE and SET meta into a single round-trip.
func (s *RedisStore) updateMetaTTL(ctx context.Context, id, key string) error {
	metaKey := s.metaKey(id)
	meta := map[string]any{"last_accessed_at": time.Now()}
	metaData, _ := json.Marshal(meta)

	pipe := s.client.Pipeline()
	if s.ttl > 0 {
		pipe.Expire(ctx, key, s.ttl)
	}
	pipe.Set(ctx, metaKey, metaData, s.ttl)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis pipeline failed: %w", err)
	}
	return nil
}

// LoadSummaries returns all summaries for the conversation.
func (s *RedisStore) LoadSummaries(ctx context.Context, id string) ([]Summary, error) {
	if id == "" {
		return nil, ErrInvalidID
	}

	key := s.summariesKey(id)
	vals, err := s.client.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, fmt.Errorf("redis lrange failed: %w", err)
	}

	if len(vals) == 0 {
		// Fall back to monolithic key
		state, err := s.Load(ctx, id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, nil
			}
			return nil, err
		}
		return state.Summaries, nil
	}

	summaries := make([]Summary, 0, len(vals))
	for _, v := range vals {
		var sm Summary
		if err := json.Unmarshal([]byte(v), &sm); err != nil {
			return nil, fmt.Errorf("failed to unmarshal summary: %w", err)
		}
		summaries = append(summaries, sm)
	}

	return summaries, nil
}

// SaveSummary appends a summary to the conversation's summary list.
// Uses a pipeline to batch RPUSH and EXPIRE into a single round-trip.
func (s *RedisStore) SaveSummary(ctx context.Context, id string, summary Summary) error {
	if id == "" {
		return ErrInvalidID
	}

	key := s.summariesKey(id)
	data, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("failed to marshal summary: %w", err)
	}

	pipe := s.client.Pipeline()
	pipe.RPush(ctx, key, data)
	if s.ttl > 0 {
		pipe.Expire(ctx, key, s.ttl)
	}

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis pipeline failed: %w", err)
	}

	return nil
}

// migrateToListFormat migrates a conversation from monolithic JSON to list format.
// Uses pipelining for the batched RPUSH of messages and summaries.
func (s *RedisStore) migrateToListFormat(ctx context.Context, id string) error {
	state, err := s.Load(ctx, id)
	if err != nil {
		return err
	}

	key := s.messagesKey(id)
	if err := s.rpushMessages(ctx, key, state.Messages); err != nil {
		return err
	}
	if err := s.expireIfTTL(ctx, key); err != nil {
		return err
	}

	return s.migrateSummaries(ctx, id, state.Summaries)
}

// migrateSummaries migrates summaries to list format using a single batched RPUSH.
func (s *RedisStore) migrateSummaries(ctx context.Context, id string, summaries []Summary) error {
	if len(summaries) == 0 {
		return nil
	}

	sumKey := s.summariesKey(id)
	vals := make([]interface{}, 0, len(summaries))
	for i := range summaries {
		data, err := json.Marshal(&summaries[i])
		if err != nil {
			return fmt.Errorf("failed to marshal summary: %w", err)
		}
		vals = append(vals, data)
	}

	if err := s.client.RPush(ctx, sumKey, vals...).Err(); err != nil {
		return fmt.Errorf("redis rpush failed: %w", err)
	}
	return s.expireIfTTL(ctx, sumKey)
}

// expireIfTTL sets expiration on a key if TTL is configured.
func (s *RedisStore) expireIfTTL(ctx context.Context, key string) error {
	if s.ttl > 0 {
		if err := s.client.Expire(ctx, key, s.ttl).Err(); err != nil {
			return fmt.Errorf("redis expire failed: %w", err)
		}
	}
	return nil
}

// loadRecentFromMonolithic loads recent messages from the monolithic key format.
func (s *RedisStore) loadRecentFromMonolithic(ctx context.Context, id string, n int) ([]types.Message, error) {
	state, err := s.Load(ctx, id)
	if err != nil {
		return nil, err
	}
	msgs := state.Messages
	if n >= len(msgs) {
		return msgs, nil
	}
	return msgs[len(msgs)-n:], nil
}

// extractIDFromKey extracts the conversation ID from a Redis key.
func (s *RedisStore) extractIDFromKey(key string) string {
	prefix := s.conversationKey("")
	if strings.HasPrefix(key, prefix) {
		return strings.TrimPrefix(key, prefix)
	}
	return ""
}

// sortConversations sorts conversation IDs using pipelined GET to fetch all states
// in a single round-trip, then sorts in memory.
func (s *RedisStore) sortConversations(ctx context.Context, ids []string, sortBy, sortOrder string) error {
	if len(ids) == 0 {
		return nil
	}

	states, err := s.pipelinedLoadStates(ctx, ids)
	if err != nil {
		return err
	}

	ascending := strings.EqualFold(sortOrder, "asc")
	sortStatesByField(states, sortBy, ascending)

	// Update ids slice with sorted order
	for i, st := range states {
		ids[i] = st.id
	}

	return nil
}

// stateWithID pairs a conversation ID with its loaded state for sorting.
type stateWithID struct {
	id    string
	state *ConversationState
}

// pipelinedLoadStates fetches multiple conversation states using a single pipelined GET.
func (s *RedisStore) pipelinedLoadStates(ctx context.Context, ids []string) ([]stateWithID, error) {
	pipe := s.client.Pipeline()
	cmds := make([]*redis.StringCmd, len(ids))
	for i, id := range ids {
		cmds[i] = pipe.Get(ctx, s.conversationKey(id))
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("redis pipeline failed: %w", err)
	}

	states := make([]stateWithID, 0, len(ids))
	for i, cmd := range cmds {
		data, err := cmd.Bytes()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				continue
			}
			return nil, fmt.Errorf("redis get failed: %w", err)
		}
		var state ConversationState
		if err := json.Unmarshal(data, &state); err != nil {
			return nil, fmt.Errorf("failed to unmarshal state: %w", err)
		}
		states = append(states, stateWithID{id: ids[i], state: &state})
	}
	return states, nil
}

// sortStatesByField sorts a slice of stateWithID entries by the given field and direction.
func sortStatesByField(states []stateWithID, sortBy string, ascending bool) {
	sort.Slice(states, func(i, j int) bool {
		var less bool
		switch sortBy {
		case SortByCreatedAt:
			t1 := getCreatedAt(states[i].state)
			t2 := getCreatedAt(states[j].state)
			less = t1.Before(t2)
		case SortByUpdatedAt, "":
			less = states[i].state.LastAccessedAt.Before(states[j].state.LastAccessedAt)
		default:
			return false
		}

		if ascending {
			return less
		}
		return !less
	})
}
