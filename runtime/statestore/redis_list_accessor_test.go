package statestore

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RedisStore implements ListAccessor backed by Redis lists (RPUSH per
// item, LRANGE on read). The tests below pin per-implementation details
// — index-set housekeeping, EXISTS-based ErrNotFound disambiguation, and
// Delete cleanup of all list keys.

func TestRedisStore_AppendList_NewList(t *testing.T) {
	store, mr := setupRedisStore(t)
	ctx := context.Background()

	require.NoError(t, store.Save(ctx, &ConversationState{ID: "conv-1"}))
	require.NoError(t, store.AppendList(ctx, "conv-1", "events", [][]byte{[]byte(`{"v":1}`)}))

	// Underlying list key was created with the right name.
	assert.True(t, mr.Exists(store.listKey("conv-1", "events")))
	// Lists index set received the list name.
	indexed, err := mr.SMembers(store.listsIndexKey("conv-1"))
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"events"}, indexed)
}

func TestRedisStore_AppendList_AppendsToExisting(t *testing.T) {
	store, _ := setupRedisStore(t)
	ctx := context.Background()

	require.NoError(t, store.Save(ctx, &ConversationState{ID: "conv-1"}))
	require.NoError(t, store.AppendList(ctx, "conv-1", "events", [][]byte{[]byte("a"), []byte("b")}))
	require.NoError(t, store.AppendList(ctx, "conv-1", "events", [][]byte{[]byte("c")}))

	got, err := store.LoadList(ctx, "conv-1", "events")
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, "a", string(got[0]))
	assert.Equal(t, "b", string(got[1]))
	assert.Equal(t, "c", string(got[2]))
}

func TestRedisStore_AppendList_EmptyItemsNoOp(t *testing.T) {
	store, mr := setupRedisStore(t)
	ctx := context.Background()

	require.NoError(t, store.AppendList(ctx, "conv-1", "events", nil))
	assert.False(t, mr.Exists(store.listKey("conv-1", "events")))
}

func TestRedisStore_LoadList_DistinguishesEmptyFromMissing(t *testing.T) {
	store, _ := setupRedisStore(t)
	ctx := context.Background()

	// Conversation exists but list never written → (nil, nil).
	require.NoError(t, store.Save(ctx, &ConversationState{ID: "conv-1"}))
	got, err := store.LoadList(ctx, "conv-1", "events")
	require.NoError(t, err)
	assert.Nil(t, got)

	// No conversation at all → ErrNotFound.
	_, err = store.LoadList(ctx, "missing", "events")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRedisStore_LoadList_PreservesAppendOrder(t *testing.T) {
	store, _ := setupRedisStore(t)
	ctx := context.Background()
	require.NoError(t, store.Save(ctx, &ConversationState{ID: "conv-1"}))

	for i := range 25 {
		require.NoError(t, store.AppendList(ctx, "conv-1", "events", [][]byte{
			fmt.Appendf(nil, "entry-%d", i),
		}))
	}

	got, err := store.LoadList(ctx, "conv-1", "events")
	require.NoError(t, err)
	require.Len(t, got, 25)
	for i, item := range got {
		assert.Equal(t, fmt.Sprintf("entry-%d", i), string(item))
	}
}

func TestRedisStore_ListLen_Tracks(t *testing.T) {
	store, _ := setupRedisStore(t)
	ctx := context.Background()
	require.NoError(t, store.Save(ctx, &ConversationState{ID: "conv-1"}))

	n, err := store.ListLen(ctx, "conv-1", "events")
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	require.NoError(t, store.AppendList(ctx, "conv-1", "events", [][]byte{[]byte("a"), []byte("b")}))
	n, err = store.ListLen(ctx, "conv-1", "events")
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	// Missing conversation returns ErrNotFound.
	_, err = store.ListLen(ctx, "missing", "events")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRedisStore_AppendList_Concurrent(t *testing.T) {
	store, _ := setupRedisStore(t)
	ctx := context.Background()
	require.NoError(t, store.Save(ctx, &ConversationState{ID: "conv-1"}))

	const goroutines = 8
	const itemsPerGoroutine = 25

	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := range itemsPerGoroutine {
				payload := fmt.Appendf(nil, "g%d-i%d", g, i)
				require.NoError(t, store.AppendList(ctx, "conv-1", "events", [][]byte{payload}))
			}
		}(g)
	}
	wg.Wait()

	n, err := store.ListLen(ctx, "conv-1", "events")
	require.NoError(t, err)
	assert.Equal(t, goroutines*itemsPerGoroutine, n)
}

// TestRedisStore_Delete_RemovesLists ensures the per-conversation list
// keys plus the index set are removed when the conversation is deleted —
// without this Delete, list keys would leak.
func TestRedisStore_Delete_RemovesLists(t *testing.T) {
	store, mr := setupRedisStore(t)
	ctx := context.Background()

	require.NoError(t, store.Save(ctx, &ConversationState{ID: "conv-1", UserID: "user-1"}))
	require.NoError(t, store.AppendList(ctx, "conv-1", "events", [][]byte{[]byte("a")}))
	require.NoError(t, store.AppendList(ctx, "conv-1", "audit", [][]byte{[]byte("b")}))

	require.NoError(t, store.Delete(ctx, "conv-1"))

	assert.False(t, mr.Exists(store.listKey("conv-1", "events")))
	assert.False(t, mr.Exists(store.listKey("conv-1", "audit")))
	assert.False(t, mr.Exists(store.listsIndexKey("conv-1")))
}
