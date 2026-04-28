package statestore

import (
	"context"
	"fmt"
	"sync"
	"testing"

	runtimestore "github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ArenaStateStore mirrors MemoryStore's ListAccessor semantics for use
// from arena (which builds its own store rather than going through the
// SDK). The tests below pin those semantics — input/output deep-copy,
// auto-create on first append, distinguishing empty list from missing
// conversation.

func TestArenaStateStore_AppendList_NewList(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	require.NoError(t, store.AppendList(ctx, "conv-1", "events", [][]byte{[]byte(`{"v":1}`)}))

	got, err := store.LoadList(ctx, "conv-1", "events")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, `{"v":1}`, string(got[0]))
}

func TestArenaStateStore_AppendList_AppendsToExisting(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	require.NoError(t, store.AppendList(ctx, "conv-1", "events", [][]byte{[]byte("a"), []byte("b")}))
	require.NoError(t, store.AppendList(ctx, "conv-1", "events", [][]byte{[]byte("c")}))

	got, err := store.LoadList(ctx, "conv-1", "events")
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, "a", string(got[0]))
	assert.Equal(t, "b", string(got[1]))
	assert.Equal(t, "c", string(got[2]))
}

func TestArenaStateStore_AppendList_DoesNotMutateInput(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	original := []byte("original-bytes")
	require.NoError(t, store.AppendList(ctx, "conv-1", "events", [][]byte{original}))

	for i := range original {
		original[i] = 'x'
	}

	got, err := store.LoadList(ctx, "conv-1", "events")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "original-bytes", string(got[0]))
}

func TestArenaStateStore_LoadList_DistinguishesEmptyFromMissing(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	// Conversation exists, list does not — (nil, nil).
	require.NoError(t, store.AppendList(ctx, "conv-1", "other", [][]byte{[]byte("x")}))
	got, err := store.LoadList(ctx, "conv-1", "missing-list")
	require.NoError(t, err)
	assert.Nil(t, got)

	// Conversation doesn't exist — ErrNotFound.
	_, err = store.LoadList(ctx, "missing", "events")
	assert.ErrorIs(t, err, runtimestore.ErrNotFound)
}

func TestArenaStateStore_ListLen_Tracks(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()
	require.NoError(t, store.AppendList(ctx, "conv-1", "warm", [][]byte{[]byte("x")}))

	n, err := store.ListLen(ctx, "conv-1", "events")
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	require.NoError(t, store.AppendList(ctx, "conv-1", "events", [][]byte{[]byte("a"), []byte("b")}))
	n, err = store.ListLen(ctx, "conv-1", "events")
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	_, err = store.ListLen(ctx, "missing", "events")
	assert.ErrorIs(t, err, runtimestore.ErrNotFound)
}

func TestArenaStateStore_AppendList_Concurrent(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	const goroutines = 16
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

func TestArenaStateStore_DeleteRemovesLists(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	require.NoError(t, store.AppendList(ctx, "conv-1", "events", [][]byte{[]byte("x")}))

	require.NoError(t, store.Delete(ctx, "conv-1"))

	_, err := store.LoadList(ctx, "conv-1", "events")
	assert.ErrorIs(t, err, runtimestore.ErrNotFound)
}
