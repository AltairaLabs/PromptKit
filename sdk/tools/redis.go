package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// DefaultRedisKeyPrefix namespaces pending-approval keys in Redis.
const DefaultRedisKeyPrefix = "promptkit:pending:"

// RedisPendingStore is a durable PendingStore backed by Redis. Pending calls
// survive process restarts and can be resolved by a different instance of the
// same agent. Claim is atomic across processes via GETDEL, so exactly one
// instance wins a concurrent resolve.
//
// Records are stored one key per call under {prefix}{convID}:{id} with a native
// TTL, so expiry is handled by Redis (no cleanup goroutine). The caller owns the
// *redis.Client lifecycle; this store does not close it.
//
// Conversation IDs and call IDs must not contain the key separator ':' — the
// SDK's generated IDs (UUID conversation IDs, provider call IDs) do not. List
// additionally filters on the exact decoded ConversationID, so a ':' in an id
// cannot leak one conversation's calls into another's listing.
type RedisPendingStore struct {
	client    *redis.Client
	keyPrefix string
	ttl       time.Duration
}

// compile-time assertion that RedisPendingStore satisfies the interface.
var _ PendingStore = (*RedisPendingStore)(nil)

// RedisOption configures a RedisPendingStore.
type RedisOption func(*RedisPendingStore)

// WithRedisKeyPrefix overrides the key namespace (default DefaultRedisKeyPrefix).
func WithRedisKeyPrefix(prefix string) RedisOption {
	return func(s *RedisPendingStore) { s.keyPrefix = prefix }
}

// WithRedisTTL overrides the per-entry TTL (default DefaultPendingTTL).
func WithRedisTTL(ttl time.Duration) RedisOption {
	return func(s *RedisPendingStore) { s.ttl = ttl }
}

// NewRedisPendingStore creates a durable pending store over the given Redis
// client:
//
//	store := tools.NewRedisPendingStore(
//	    redis.NewClient(&redis.Options{Addr: "localhost:6379"}),
//	)
//	conv, _ := sdk.Open(pack, "assist", sdk.WithPendingStore(store))
func NewRedisPendingStore(client *redis.Client, opts ...RedisOption) *RedisPendingStore {
	s := &RedisPendingStore{
		client:    client,
		keyPrefix: DefaultRedisKeyPrefix,
		ttl:       DefaultPendingTTL,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *RedisPendingStore) key(convID, id string) string {
	return fmt.Sprintf("%s%s:%s", s.keyPrefix, convID, id)
}

func (s *RedisPendingStore) scanPattern(convID string) string {
	return fmt.Sprintf("%s%s:*", s.keyPrefix, convID)
}

// Add persists a pending call with a native TTL.
func (s *RedisPendingStore) Add(ctx context.Context, call *PendingToolCall) error {
	if call.CreatedAt.IsZero() {
		// Best-effort stamp; callers that need a deterministic clock set it first.
		call.CreatedAt = time.Now()
	}
	data, err := json.Marshal(call)
	if err != nil {
		return fmt.Errorf("marshal pending call: %w", err)
	}
	if err := s.client.Set(ctx, s.key(call.ConversationID, call.ID), data, s.ttl).Err(); err != nil {
		return fmt.Errorf("redis set pending call: %w", err)
	}
	return nil
}

// Get returns a read-only view of a pending call.
func (s *RedisPendingStore) Get(ctx context.Context, convID, id string) (*PendingToolCall, bool, error) {
	data, err := s.client.Get(ctx, s.key(convID, id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("redis get pending call: %w", err)
	}
	call, err := decodePendingCall(data)
	if err != nil {
		return nil, false, err
	}
	return call, true, nil
}

// List returns all pending calls for a conversation via a scoped SCAN.
func (s *RedisPendingStore) List(ctx context.Context, convID string) ([]*PendingToolCall, error) {
	result := make([]*PendingToolCall, 0)
	iter := s.client.Scan(ctx, 0, s.scanPattern(convID), 0).Iterator()
	for iter.Next(ctx) {
		data, err := s.client.Get(ctx, iter.Val()).Bytes()
		if errors.Is(err, redis.Nil) {
			continue // expired between SCAN and GET
		}
		if err != nil {
			return nil, fmt.Errorf("redis get during list: %w", err)
		}
		call, err := decodePendingCall(data)
		if err != nil {
			return nil, err
		}
		// Exact match on the decoded ConversationID, not just the SCAN prefix:
		// a conversation whose ID starts with convID+":" would otherwise leak in.
		if call.ConversationID != convID {
			continue
		}
		result = append(result, call)
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("redis scan pending calls: %w", err)
	}
	return result, nil
}

// Claim atomically removes and returns a pending call via GETDEL, guaranteeing a
// single winner across processes.
func (s *RedisPendingStore) Claim(ctx context.Context, convID, id string) (*PendingToolCall, bool, error) {
	data, err := s.client.GetDel(ctx, s.key(convID, id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("redis getdel pending call: %w", err)
	}
	call, err := decodePendingCall(data)
	if err != nil {
		return nil, false, err
	}
	return call, true, nil
}

func decodePendingCall(data []byte) (*PendingToolCall, error) {
	var call PendingToolCall
	if err := json.Unmarshal(data, &call); err != nil {
		return nil, fmt.Errorf("unmarshal pending call: %w", err)
	}
	return &call, nil
}
