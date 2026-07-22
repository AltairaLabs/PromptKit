package tools

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRedisStore(t *testing.T, opts ...RedisOption) *RedisPendingStore {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewRedisPendingStore(client, opts...)
}

func TestRedisPendingStore_AddGetListClaim(t *testing.T) {
	ctx := context.Background()
	store := newTestRedisStore(t)

	call := &PendingToolCall{
		ID: "call-1", ConversationID: testConv, Name: "send",
		Arguments: map[string]any{"body": "hi"}, Reason: "approval",
	}
	require.NoError(t, store.Add(ctx, call))

	got, ok, err := store.Get(ctx, testConv, "call-1")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "send", got.Name)
	assert.Equal(t, "hi", got.Arguments["body"])

	list, err := store.List(ctx, testConv)
	require.NoError(t, err)
	assert.Len(t, list, 1)

	claimed, ok, err := store.Claim(ctx, testConv, "call-1")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "call-1", claimed.ID)

	_, present, err := store.Get(ctx, testConv, "call-1")
	require.NoError(t, err)
	assert.False(t, present, "GETDEL must remove the key")
}

func TestRedisPendingStore_ListScopedByConversation(t *testing.T) {
	ctx := context.Background()
	store := newTestRedisStore(t)
	require.NoError(t, store.Add(ctx, &PendingToolCall{ID: "a", ConversationID: "c1", Name: "t"}))
	require.NoError(t, store.Add(ctx, &PendingToolCall{ID: "b", ConversationID: "c1", Name: "t"}))
	require.NoError(t, store.Add(ctx, &PendingToolCall{ID: "c", ConversationID: "c2", Name: "t"}))

	c1, err := store.List(ctx, "c1")
	require.NoError(t, err)
	assert.Len(t, c1, 2)
	c2, err := store.List(ctx, "c2")
	require.NoError(t, err)
	assert.Len(t, c2, 1)
}

// TestRedisPendingStore_ClaimSingleWinner is the cross-process invariant modeled
// in-process: GETDEL guarantees exactly one winner among concurrent claimers.
func TestRedisPendingStore_ClaimSingleWinner(t *testing.T) {
	ctx := context.Background()
	store := newTestRedisStore(t)
	require.NoError(t, store.Add(ctx, &PendingToolCall{ID: "x", ConversationID: testConv, Name: "t"}))

	const racers = 32
	var winners atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, ok, err := store.Claim(ctx, testConv, "x"); err == nil && ok {
				winners.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	assert.Equal(t, int32(1), winners.Load(), "GETDEL must yield exactly one winner")
}

// TestRedisPendingStore_DurableAcrossHandles proves a call added via one store
// handle is resolvable via a different handle on the same backend — the
// cross-process/restart durability property.
func TestRedisPendingStore_DurableAcrossHandles(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)

	writer := NewRedisPendingStore(redis.NewClient(&redis.Options{Addr: mr.Addr()}))
	require.NoError(t, writer.Add(ctx, &PendingToolCall{ID: "1", ConversationID: testConv, Name: "t"}))

	reader := NewRedisPendingStore(redis.NewClient(&redis.Options{Addr: mr.Addr()}))
	got, ok, err := reader.Claim(ctx, testConv, "1")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "1", got.ID)
}

func TestRedisPendingStore_CustomPrefix(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewRedisPendingStore(client, WithRedisKeyPrefix("hitl:"))

	require.NoError(t, store.Add(ctx, &PendingToolCall{ID: "1", ConversationID: testConv, Name: "t"}))
	require.True(t, mr.Exists("hitl:"+testConv+":1"), "key must use the custom prefix")

	got, ok, err := store.Get(ctx, testConv, "1")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "1", got.ID)
}

// TestRedisPendingStore_DecodeError plants malformed JSON and confirms every
// read path surfaces the error rather than a zero-value call.
func TestRedisPendingStore_DecodeError(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewRedisPendingStore(client)

	require.NoError(t, mr.Set(store.key(testConv, "bad"), "{not-json"))

	_, _, err := store.Get(ctx, testConv, "bad")
	require.Error(t, err)

	_, err = store.List(ctx, testConv)
	require.Error(t, err)

	_, _, err = store.Claim(ctx, testConv, "bad")
	require.Error(t, err)
}

// TestRedisPendingStore_ConnError confirms a dead backend surfaces errors on
// every method rather than silently succeeding.
func TestRedisPendingStore_ConnError(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := NewRedisPendingStore(client)
	mr.Close() // kill the backend

	assert.Error(t, store.Add(ctx, &PendingToolCall{ID: "1", ConversationID: testConv, Name: "t"}))
	_, _, err := store.Get(ctx, testConv, "1")
	assert.Error(t, err)
	_, err = store.List(ctx, testConv)
	assert.Error(t, err)
	_, _, err = store.Claim(ctx, testConv, "1")
	assert.Error(t, err)
}

func TestRedisPendingStore_TTLExpires(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewRedisPendingStore(client, WithRedisTTL(1*time.Minute))

	require.NoError(t, store.Add(ctx, &PendingToolCall{ID: "1", ConversationID: testConv, Name: "t"}))
	mr.FastForward(2 * time.Minute)

	_, ok, err := store.Get(ctx, testConv, "1")
	require.NoError(t, err)
	assert.False(t, ok, "entry should expire via native TTL")
}
