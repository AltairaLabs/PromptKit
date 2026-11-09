package statestore

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMemoryStore_ConcurrentAccess runs save/load/list/delete on one shared
// store from many goroutines released on a barrier. Run under -race. Each
// goroutine owns one conversation, so every Load must see its own ID, and the
// final Save each makes must be what remains.
func TestMemoryStore_ConcurrentAccess(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	const numGoroutines = 100
	const numOps = 10

	var wg sync.WaitGroup
	var wrongID atomic.Int32
	start := make(chan struct{})
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			<-start
			convID := fmt.Sprintf("conv-%03d", id)
			for j := 0; j < numOps; j++ {
				_ = store.Save(ctx, &ConversationState{ID: convID, UserID: "user-concurrent", TokenCount: j})
				if got, err := store.Load(ctx, convID); err == nil && got.ID != convID {
					wrongID.Add(1)
				}
				_, _ = store.List(ctx, ListOptions{UserID: "user-concurrent"})
				if j%3 == 0 {
					_ = store.Delete(ctx, convID)
				}
			}
			_ = store.Save(ctx, &ConversationState{ID: convID, UserID: "user-concurrent", TokenCount: numOps})
		}(i)
	}
	close(start)
	wg.Wait()

	assert.Zero(t, wrongID.Load(), "a Load returned another conversation's state")
	ids, err := store.List(ctx, ListOptions{UserID: "user-concurrent", Limit: numGoroutines * 2})
	require.NoError(t, err)
	sort.Strings(ids)
	want := make([]string, numGoroutines)
	for i := range want {
		want[i] = fmt.Sprintf("conv-%03d", i)
	}
	assert.Equal(t, want, ids)
	for _, id := range want {
		got, err := store.Load(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, numOps, got.TokenCount, id)
	}
}

// TestMemoryStore_TTLConcurrentAccess pins that the max-entries bound holds
// when many goroutines save at once.
func TestMemoryStore_TTLConcurrentAccess(t *testing.T) {
	const maxEntries = 50
	store := NewMemoryStore(WithMemoryTTL(30*time.Millisecond), WithMemoryMaxEntries(maxEntries))
	defer store.Close()
	ctx := context.Background()

	const numGoroutines = 100
	const numOps = 20
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			<-start
			convID := fmt.Sprintf("conv-%03d", id)
			for j := 0; j < numOps; j++ {
				_ = store.Save(ctx, &ConversationState{ID: convID, UserID: "user-concurrent", TokenCount: j})
				_, _ = store.Load(ctx, convID)
				_, _ = store.List(ctx, ListOptions{UserID: "user-concurrent"})
				_, _ = store.MessageCount(ctx, convID)
				if j%3 == 0 {
					_ = store.Delete(ctx, convID)
				}
			}
		}(i)
	}
	close(start)
	wg.Wait()

	assert.LessOrEqual(t, store.Len(), maxEntries)
}

// TestMemoryStore_CloseIdempotent pins that Close stops background eviction
// and may be called more than once.
func TestMemoryStore_CloseIdempotent(t *testing.T) {
	store := NewMemoryStore(
		WithMemoryTTL(10*time.Millisecond),
		WithMemoryEvictionInterval(5*time.Millisecond),
	)
	ctx := context.Background()
	require.NoError(t, store.Save(ctx, &ConversationState{ID: "conv-close"}))

	require.NotPanics(t, func() {
		store.Close()
		store.Close()
	})

	// With the eviction loop stopped, the expired entry is still held...
	time.Sleep(40 * time.Millisecond)
	assert.Equal(t, 1, store.Len())
	// ...and access still evicts it lazily.
	_, err := store.Load(ctx, "conv-close")
	assert.ErrorIs(t, err, ErrNotFound)
	assert.Equal(t, 0, store.Len())
}

// TestMemoryStore_CloseWithoutBackgroundEviction pins that Close on a store
// with no eviction loop is safe and leaves the store usable.
func TestMemoryStore_CloseWithoutBackgroundEviction(t *testing.T) {
	store := NewMemoryStore()
	require.NotPanics(t, store.Close)

	ctx := context.Background()
	require.NoError(t, store.Save(ctx, &ConversationState{ID: "conv-after", TokenCount: 3}))
	got, err := store.Load(ctx, "conv-after")
	require.NoError(t, err)
	assert.Equal(t, 3, got.TokenCount)
}
