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

// redisStateMeta holds the non-message, non-summary fields of a ConversationState.
// It is stored as a single JSON value in the meta key, keeping it small and fast to serialize.
type redisStateMeta struct {
	ID             string                 `json:"id"`
	UserID         string                 `json:"user_id"`
	SystemPrompt   string                 `json:"system_prompt,omitempty"`
	TokenCount     int                    `json:"token_count,omitempty"`
	LastAccessedAt time.Time              `json:"last_accessed_at"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

// Load retrieves a conversation state by ID from Redis.
// Tries the decomposed format (meta key + messages list + summaries list) first,
// then falls back to the legacy monolithic JSON string for backward compatibility.
func (s *RedisStore) Load(ctx context.Context, id string) (*ConversationState, error) {
	if id == "" {
		return nil, ErrInvalidID
	}

	// Try decomposed format first
	state, err := s.loadDecomposed(ctx, id)
	if err == nil {
		return state, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	// Fall back to legacy monolithic format
	return s.loadMonolithic(ctx, id)
}

// loadDecomposed loads a conversation state from decomposed keys (meta + messages list + summaries list).
func (s *RedisStore) loadDecomposed(ctx context.Context, id string) (*ConversationState, error) {
	meta, err := s.loadMeta(ctx, id)
	if err != nil {
		return nil, err
	}

	state := metaToState(meta)

	state.Messages, err = s.loadMessagesList(ctx, id)
	if err != nil {
		return nil, err
	}

	state.Summaries, err = s.loadSummariesList(ctx, id)
	if err != nil {
		return nil, err
	}

	return state, nil
}

// loadMeta loads and unmarshals the meta key for a conversation.
func (s *RedisStore) loadMeta(ctx context.Context, id string) (*redisStateMeta, error) {
	data, err := s.client.Get(ctx, s.metaKey(id)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("redis get meta failed: %w", err)
	}

	var meta redisStateMeta
	if unmarshalErr := json.Unmarshal(data, &meta); unmarshalErr != nil {
		return nil, fmt.Errorf("failed to unmarshal meta: %w", unmarshalErr)
	}
	return &meta, nil
}

// metaToState converts a redisStateMeta to a ConversationState (without messages/summaries).
func metaToState(meta *redisStateMeta) *ConversationState {
	return &ConversationState{
		ID:             meta.ID,
		UserID:         meta.UserID,
		SystemPrompt:   meta.SystemPrompt,
		TokenCount:     meta.TokenCount,
		LastAccessedAt: meta.LastAccessedAt,
		Metadata:       meta.Metadata,
	}
}

// loadMessagesList loads messages from a Redis list key.
func (s *RedisStore) loadMessagesList(ctx context.Context, id string) ([]types.Message, error) {
	vals, err := s.client.LRange(ctx, s.messagesKey(id), 0, -1).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("redis lrange messages failed: %w", err)
	}
	messages := make([]types.Message, 0, len(vals))
	for _, v := range vals {
		var msg types.Message
		if unmarshalErr := json.Unmarshal([]byte(v), &msg); unmarshalErr != nil {
			return nil, fmt.Errorf("failed to unmarshal message: %w", unmarshalErr)
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

// loadSummariesList loads summaries from a Redis list key.
func (s *RedisStore) loadSummariesList(ctx context.Context, id string) ([]Summary, error) {
	vals, err := s.client.LRange(ctx, s.summariesKey(id), 0, -1).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("redis lrange summaries failed: %w", err)
	}
	if len(vals) == 0 {
		return nil, nil
	}
	summaries := make([]Summary, 0, len(vals))
	for _, v := range vals {
		var sm Summary
		if unmarshalErr := json.Unmarshal([]byte(v), &sm); unmarshalErr != nil {
			return nil, fmt.Errorf("failed to unmarshal summary: %w", unmarshalErr)
		}
		summaries = append(summaries, sm)
	}
	return summaries, nil
}

// loadMonolithic loads a conversation state from the legacy monolithic JSON string.
func (s *RedisStore) loadMonolithic(ctx context.Context, id string) (*ConversationState, error) {
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

// Save persists a conversation state to Redis using decomposed keys.
// Metadata is stored in a meta key, messages in a Redis list, and summaries in a separate list.
// Messages use append-only delta writes: only new messages (beyond what is already stored)
// are RPUSHed, avoiding the cost of DEL+RPUSH for the entire list on every save.
// If the message count in Redis exceeds the local count (e.g., external truncation),
// a full rewrite is performed.
func (s *RedisStore) Save(ctx context.Context, state *ConversationState) error {
	if state == nil {
		return ErrInvalidState
	}
	if state.ID == "" {
		return ErrInvalidID
	}

	// Update timestamp
	state.LastAccessedAt = time.Now()

	// Serialize metadata (small — excludes messages and summaries)
	metaData, err := json.Marshal(stateToMeta(state))
	if err != nil {
		return fmt.Errorf("failed to marshal meta: %w", err)
	}

	// Check how many messages are already stored so we can do a delta RPUSH
	msgKey := s.messagesKey(state.ID)
	existingCount, err := s.client.LLen(ctx, msgKey).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("redis llen failed: %w", err)
	}

	// Build pipeline: write meta, delta-append messages, replace summaries, cleanup legacy, update index
	pipe := s.client.Pipeline()
	pipe.Set(ctx, s.metaKey(state.ID), metaData, s.ttl)

	newCount := int64(len(state.Messages))
	if existingCount > 0 && existingCount <= newCount {
		// Append-only delta: only RPUSH messages beyond what is already stored
		delta := state.Messages[existingCount:]
		if len(delta) > 0 {
			vals, marshalErr := marshalMessageSlice(delta)
			if marshalErr != nil {
				return marshalErr
			}
			pipe.RPush(ctx, msgKey, vals...)
		}
		if s.ttl > 0 {
			pipe.Expire(ctx, msgKey, s.ttl)
		}
	} else {
		// Full rewrite: count mismatch (truncation, first save, or empty list)
		if replaceErr := s.pipeReplaceList(ctx, pipe, msgKey, marshalMessages(state.Messages)); replaceErr != nil {
			return replaceErr
		}
	}

	if err := s.pipeReplaceList(ctx, pipe, s.summariesKey(state.ID), marshalSummaries(state.Summaries)); err != nil {
		return err
	}

	// Remove legacy monolithic key if it exists
	pipe.Del(ctx, s.conversationKey(state.ID))

	s.pipeUpdateUserIndex(ctx, pipe, state.UserID, state.ID)
	s.pipeUpdateGlobalIndex(ctx, pipe, state.ID)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis pipeline failed: %w", err)
	}

	return nil
}

// marshalMessageSlice serializes a message slice to a list of interface{} values.
func marshalMessageSlice(msgs []types.Message) ([]interface{}, error) {
	vals := make([]interface{}, 0, len(msgs))
	for i := range msgs {
		data, err := json.Marshal(&msgs[i])
		if err != nil {
			return nil, fmt.Errorf("failed to marshal message: %w", err)
		}
		vals = append(vals, data)
	}
	return vals, nil
}

// stateToMeta extracts the metadata fields from a ConversationState.
func stateToMeta(state *ConversationState) redisStateMeta {
	return redisStateMeta{
		ID:             state.ID,
		UserID:         state.UserID,
		SystemPrompt:   state.SystemPrompt,
		TokenCount:     state.TokenCount,
		LastAccessedAt: state.LastAccessedAt,
		Metadata:       state.Metadata,
	}
}

// marshalMessages serializes messages to a list of JSON byte slices.
// Returns a marshalResult that is evaluated lazily when added to the pipeline.
func marshalMessages(msgs []types.Message) func() ([]interface{}, error) {
	return func() ([]interface{}, error) {
		vals := make([]interface{}, 0, len(msgs))
		for i := range msgs {
			data, err := json.Marshal(&msgs[i])
			if err != nil {
				return nil, fmt.Errorf("failed to marshal message: %w", err)
			}
			vals = append(vals, data)
		}
		return vals, nil
	}
}

// marshalSummaries serializes summaries to a list of JSON byte slices.
func marshalSummaries(sums []Summary) func() ([]interface{}, error) {
	return func() ([]interface{}, error) {
		vals := make([]interface{}, 0, len(sums))
		for i := range sums {
			data, err := json.Marshal(&sums[i])
			if err != nil {
				return nil, fmt.Errorf("failed to marshal summary: %w", err)
			}
			vals = append(vals, data)
		}
		return vals, nil
	}
}

// pipeReplaceList deletes a list key and re-populates it via RPUSH in the given pipeline.
func (s *RedisStore) pipeReplaceList(
	ctx context.Context, pipe redis.Pipeliner, key string, marshalFn func() ([]interface{}, error),
) error {
	pipe.Del(ctx, key)
	vals, err := marshalFn()
	if err != nil {
		return err
	}
	if len(vals) > 0 {
		pipe.RPush(ctx, key, vals...)
		if s.ttl > 0 {
			pipe.Expire(ctx, key, s.ttl)
		}
	}
	return nil
}

// pipeUpdateUserIndex adds commands to the pipeline to update the user conversation index.
func (s *RedisStore) pipeUpdateUserIndex(ctx context.Context, pipe redis.Pipeliner, userID, convID string) {
	if userID != "" {
		indexKey := s.userIndexKey(userID)
		pipe.SAdd(ctx, indexKey, convID)
		if s.ttl > 0 {
			pipe.Expire(ctx, indexKey, s.ttl)
		}
	}
}

// pipeUpdateGlobalIndex adds commands to the pipeline to maintain a global conversation ID set.
// This avoids expensive SCAN operations when listing all conversations.
func (s *RedisStore) pipeUpdateGlobalIndex(ctx context.Context, pipe redis.Pipeliner, convID string) {
	indexKey := s.globalIndexKey()
	pipe.SAdd(ctx, indexKey, convID)
	// No TTL on global index — entries are removed explicitly on Delete.
}

// globalIndexKey returns the Redis key for the global conversation index set.
func (s *RedisStore) globalIndexKey() string {
	return fmt.Sprintf("%s:conversations:index", s.prefix)
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
// Removes both decomposed keys (meta, messages, summaries) and legacy monolithic key.
// Uses a pipeline to batch all DEL commands and optional user index cleanup.
func (s *RedisStore) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidID
	}

	// Load state to get UserID for index cleanup
	state, err := s.Load(ctx, id)
	if err != nil {
		return err
	}

	// Pipeline: DEL all keys for this conversation + optional SRem from user index
	pipe := s.client.Pipeline()
	delCmd := pipe.Del(ctx,
		s.conversationKey(id), // legacy monolithic key
		s.metaKey(id),         // decomposed meta
		s.messagesKey(id),     // decomposed messages list
		s.summariesKey(id),    // decomposed summaries list
	)

	if state.UserID != "" {
		indexKey := s.userIndexKey(state.UserID)
		pipe.SRem(ctx, indexKey, id)
	}

	// Remove from global conversation index
	pipe.SRem(ctx, s.globalIndexKey(), id)

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

// scanAllConversations returns all conversation IDs using the global index set.
// Falls back to SCAN if the global index is empty (e.g., legacy data).
func (s *RedisStore) scanAllConversations(ctx context.Context) ([]string, error) {
	// Try global index set first (O(N) SMEMBERS vs O(N) SCAN with cursor overhead)
	indexKey := s.globalIndexKey()
	members, err := s.client.SMembers(ctx, indexKey).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("redis smembers global index failed: %w", err)
	}
	if len(members) > 0 {
		return members, nil
	}

	// Fallback: SCAN for legacy data not yet indexed
	return s.scanAllConversationsLegacy(ctx)
}

// scanAllConversationsLegacy scans all conversation keys in Redis.
// Scans both legacy monolithic keys and decomposed meta keys to find all conversations.
func (s *RedisStore) scanAllConversationsLegacy(ctx context.Context) ([]string, error) {
	seen := make(map[string]struct{})

	// Scan legacy monolithic keys: prefix:conversation:ID
	pattern := s.conversationKey("*")
	iter := s.client.Scan(ctx, 0, pattern, 0).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		id := s.extractIDFromKey(key)
		if id != "" && !s.isSubKey(id) {
			seen[id] = struct{}{}
		}
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("redis scan failed: %w", err)
	}

	// Scan decomposed meta keys: prefix:conversation:ID:meta
	metaPattern := fmt.Sprintf("%s:conversation:*:meta", s.prefix)
	iter = s.client.Scan(ctx, 0, metaPattern, 0).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		id := s.extractIDFromMetaKey(key)
		if id != "" {
			seen[id] = struct{}{}
		}
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("redis scan meta failed: %w", err)
	}

	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
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

// LoadMetadata returns just the metadata map for the given conversation.
// This only loads the meta key, avoiding deserialization of the messages and summaries lists.
func (s *RedisStore) LoadMetadata(ctx context.Context, id string) (map[string]interface{}, error) {
	if id == "" {
		return nil, ErrInvalidID
	}

	// Try decomposed meta key first
	meta, err := s.loadMeta(ctx, id)
	if err == nil {
		return meta.Metadata, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	// Fall back to monolithic key (loads full state, but this is the legacy path)
	state, err := s.loadMonolithic(ctx, id)
	if err != nil {
		return nil, err
	}
	return state.Metadata, nil
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

// updateMetaTTL updates TTL and last accessed time on the meta key.
// Loads the existing meta, updates the timestamp, and writes it back to preserve all fields.
// Uses a pipeline for the EXPIRE + SET to minimize round-trips.
func (s *RedisStore) updateMetaTTL(ctx context.Context, id, msgKey string) error {
	metaKey := s.metaKey(id)

	// Load existing meta to preserve all fields
	var meta redisStateMeta
	data, err := s.client.Get(ctx, metaKey).Bytes()
	if err == nil {
		// Existing meta found — update timestamp
		if unmarshalErr := json.Unmarshal(data, &meta); unmarshalErr != nil {
			return fmt.Errorf("failed to unmarshal meta: %w", unmarshalErr)
		}
	} else if !errors.Is(err, redis.Nil) {
		return fmt.Errorf("redis get meta failed: %w", err)
	}
	// If meta doesn't exist, we create a minimal one with just the ID and timestamp
	meta.ID = id
	meta.LastAccessedAt = time.Now()

	metaData, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal meta: %w", err)
	}

	pipe := s.client.Pipeline()
	if s.ttl > 0 {
		pipe.Expire(ctx, msgKey, s.ttl)
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

// isSubKey returns true if the extracted ID contains a colon suffix, indicating
// it is a sub-key (e.g., :messages, :meta, :summaries) rather than a conversation ID.
func (s *RedisStore) isSubKey(id string) bool {
	return strings.Contains(id, ":")
}

// extractIDFromMetaKey extracts the conversation ID from a meta key.
// Meta keys have the format: prefix:conversation:ID:meta
func (s *RedisStore) extractIDFromMetaKey(key string) string {
	prefix := s.conversationKey("")
	suffix := ":meta"
	if strings.HasPrefix(key, prefix) && strings.HasSuffix(key, suffix) {
		id := strings.TrimPrefix(key, prefix)
		id = strings.TrimSuffix(id, suffix)
		return id
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

// pipelinedLoadStates fetches multiple conversation states using pipelined GETs.
// Tries decomposed meta keys first, then falls back to legacy monolithic keys.
func (s *RedisStore) pipelinedLoadStates(ctx context.Context, ids []string) ([]stateWithID, error) {
	// Pipeline: GET meta key and GET monolithic key for each ID
	pipe := s.client.Pipeline()
	metaCmds := make([]*redis.StringCmd, len(ids))
	monoCmds := make([]*redis.StringCmd, len(ids))
	for i, id := range ids {
		metaCmds[i] = pipe.Get(ctx, s.metaKey(id))
		monoCmds[i] = pipe.Get(ctx, s.conversationKey(id))
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("redis pipeline failed: %w", err)
	}

	states := make([]stateWithID, 0, len(ids))
	for i := range ids {
		st, err := s.resolveStateFromCmds(ids[i], metaCmds[i], monoCmds[i])
		if err != nil {
			return nil, err
		}
		if st != nil {
			states = append(states, stateWithID{id: ids[i], state: st})
		}
	}
	return states, nil
}

// resolveStateFromCmds tries the decomposed meta command first, falls back to monolithic.
// Returns nil state (no error) if neither key exists.
func (s *RedisStore) resolveStateFromCmds(
	id string, metaCmd, monoCmd *redis.StringCmd,
) (*ConversationState, error) {
	// Try decomposed meta first
	data, err := metaCmd.Bytes()
	if err == nil {
		var meta redisStateMeta
		if unmarshalErr := json.Unmarshal(data, &meta); unmarshalErr != nil {
			return nil, fmt.Errorf("failed to unmarshal meta for %s: %w", id, unmarshalErr)
		}
		return metaToState(&meta), nil
	}
	if !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("redis get meta failed: %w", err)
	}

	// Fall back to monolithic key
	data, err = monoCmd.Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, fmt.Errorf("redis get failed: %w", err)
	}
	var state ConversationState
	if unmarshalErr := json.Unmarshal(data, &state); unmarshalErr != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", unmarshalErr)
	}
	return &state, nil
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
