package statestore

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MemoryStore implements ListAccessor for append-only typed collections
// (e.g. workflow.History, workflow.ArtifactHistory). The tests below
// pin the contract every implementation of ListAccessor must satisfy.

func TestMemoryStore_AppendList_NewList(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	require.NoError(t, store.AppendList(ctx, "conv-1", "events", [][]byte{[]byte(`{"v":1}`)}))

	got, err := store.LoadList(ctx, "conv-1", "events")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, `{"v":1}`, string(got[0]))
}

func TestMemoryStore_AppendList_AppendsToExisting(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	require.NoError(t, store.AppendList(ctx, "conv-1", "events", [][]byte{[]byte(`a`), []byte(`b`)}))
	require.NoError(t, store.AppendList(ctx, "conv-1", "events", [][]byte{[]byte(`c`)}))

	got, err := store.LoadList(ctx, "conv-1", "events")
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, "a", string(got[0]))
	assert.Equal(t, "b", string(got[1]))
	assert.Equal(t, "c", string(got[2]))
}

func TestMemoryStore_AppendList_DoesNotMutateInput(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	original := []byte("original-bytes")
	require.NoError(t, store.AppendList(ctx, "conv-1", "events", [][]byte{original}))

	// Mutate the caller's buffer; loaded items must not change.
	for i := range original {
		original[i] = 'x'
	}

	got, err := store.LoadList(ctx, "conv-1", "events")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "original-bytes", string(got[0]))
}

func TestMemoryStore_AppendList_EmptyItemsNoOp(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	require.NoError(t, store.AppendList(ctx, "conv-1", "events", nil))

	// Conversation should not have been auto-created by an empty append.
	_, err := store.LoadList(ctx, "conv-1", "events")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMemoryStore_AppendList_InvalidID(t *testing.T) {
	store := NewMemoryStore()
	err := store.AppendList(context.Background(), "", "events", [][]byte{[]byte("x")})
	assert.ErrorIs(t, err, ErrInvalidID)
}

func TestMemoryStore_LoadList_Empty(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Conversation exists, list does not — (nil, nil).
	require.NoError(t, store.AppendList(ctx, "conv-1", "other", [][]byte{[]byte("x")}))
	got, err := store.LoadList(ctx, "conv-1", "missing-list")
	require.NoError(t, err)
	assert.Nil(t, got)

	// Conversation doesn't exist — ErrNotFound.
	_, err = store.LoadList(ctx, "conv-missing", "events")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMemoryStore_LoadList_PreservesAppendOrder(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	for i := range 50 {
		require.NoError(t, store.AppendList(ctx, "conv-1", "events", [][]byte{
			fmt.Appendf(nil, "entry-%d", i),
		}))
	}

	got, err := store.LoadList(ctx, "conv-1", "events")
	require.NoError(t, err)
	require.Len(t, got, 50)
	for i, item := range got {
		assert.Equal(t, fmt.Sprintf("entry-%d", i), string(item))
	}
}

func TestMemoryStore_ListLen_Tracks(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Conversation must exist for ListLen to return 0 instead of ErrNotFound.
	require.NoError(t, store.AppendList(ctx, "conv-1", "warm", [][]byte{[]byte("x")}))

	// Empty list on existing conversation.
	n, err := store.ListLen(ctx, "conv-1", "events")
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	require.NoError(t, store.AppendList(ctx, "conv-1", "events", [][]byte{[]byte("a"), []byte("b")}))
	n, err = store.ListLen(ctx, "conv-1", "events")
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	// Missing conversation returns ErrNotFound.
	_, err = store.ListLen(ctx, "conv-missing", "events")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMemoryStore_AppendList_Concurrent(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	const goroutines = 16
	const itemsPerGoroutine = 50

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

func TestMemoryStore_LoadList_DeepCopiesItems(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	require.NoError(t, store.AppendList(ctx, "conv-1", "events", [][]byte{[]byte("payload")}))

	first, err := store.LoadList(ctx, "conv-1", "events")
	require.NoError(t, err)

	// Mutate the returned slice — a second load must not observe the change.
	for i := range first[0] {
		first[0][i] = 'X'
	}

	second, err := store.LoadList(ctx, "conv-1", "events")
	require.NoError(t, err)
	assert.Equal(t, "payload", string(second[0]))
}

func TestMemoryStore_DeleteRemovesLists(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	require.NoError(t, store.AppendList(ctx, "conv-1", "events", [][]byte{[]byte("x")}))
	// Delete needs the conversation to exist via Save first (Delete loads
	// state for index cleanup), so seed it.
	require.NoError(t, store.Save(ctx, &ConversationState{ID: "conv-1"}))

	require.NoError(t, store.Delete(ctx, "conv-1"))

	_, err := store.LoadList(ctx, "conv-1", "events")
	assert.ErrorIs(t, err, ErrNotFound)
}
