package statestore

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryStore_DefaultNoTTL(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	state := &ConversationState{
		ID:     "conv-1",
		UserID: "user-alice",
	}
	err := store.Save(ctx, state)
	require.NoError(t, err)

	// Without TTL, entries never expire
	loaded, err := store.Load(ctx, "conv-1")
	require.NoError(t, err)
	assert.Equal(t, "conv-1", loaded.ID)
}

func TestMemoryStore_TTLExpiry(t *testing.T) {
	store := NewMemoryStore(WithMemoryTTL(50 * time.Millisecond))
	ctx := context.Background()

	state := &ConversationState{
		ID:     "conv-ttl",
		UserID: "user-alice",
		Messages: []types.Message{
			{Role: "user", Content: "Hello", Timestamp: time.Now()},
		},
	}
	err := store.Save(ctx, state)
	require.NoError(t, err)

	// Immediately accessible
	loaded, err := store.Load(ctx, "conv-ttl")
	require.NoError(t, err)
	assert.Equal(t, "conv-ttl", loaded.ID)

	// Wait for TTL to expire
	time.Sleep(80 * time.Millisecond)

	// Should now return ErrNotFound
	_, err = store.Load(ctx, "conv-ttl")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMemoryStore_TTLAccessRefreshes(t *testing.T) {
	store := NewMemoryStore(WithMemoryTTL(80 * time.Millisecond))
	ctx := context.Background()

	state := &ConversationState{
		ID:     "conv-refresh",
		UserID: "user-alice",
	}
	err := store.Save(ctx, state)
	require.NoError(t, err)

	// Access at 40ms — should refresh the TTL
	time.Sleep(40 * time.Millisecond)
	_, err = store.Load(ctx, "conv-refresh")
	require.NoError(t, err)

	// Access at 80ms total (40ms after refresh) — should still be alive
	time.Sleep(40 * time.Millisecond)
	_, err = store.Load(ctx, "conv-refresh")
	require.NoError(t, err)

	// Wait beyond TTL without access
	time.Sleep(100 * time.Millisecond)
	_, err = store.Load(ctx, "conv-refresh")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMemoryStore_TTLListFiltersExpired(t *testing.T) {
	store := NewMemoryStore(WithMemoryTTL(50 * time.Millisecond))
	ctx := context.Background()

	// Save two conversations
	for _, id := range []string{"conv-a", "conv-b"} {
		err := store.Save(ctx, &ConversationState{
			ID:     id,
			UserID: "user-alice",
		})
		require.NoError(t, err)
	}

	ids, err := store.List(ctx, ListOptions{UserID: "user-alice"})
	require.NoError(t, err)
	assert.Len(t, ids, 2)

	// Wait for TTL to expire
	time.Sleep(80 * time.Millisecond)

	ids, err = store.List(ctx, ListOptions{UserID: "user-alice"})
	require.NoError(t, err)
	assert.Len(t, ids, 0)

	// Also verify List without user filter
	ids, err = store.List(ctx, ListOptions{})
	require.NoError(t, err)
	assert.Len(t, ids, 0)
}

func TestMemoryStore_TTLForkExpiredSource(t *testing.T) {
	store := NewMemoryStore(WithMemoryTTL(50 * time.Millisecond))
	ctx := context.Background()

	err := store.Save(ctx, &ConversationState{
		ID:     "conv-src",
		UserID: "user-alice",
	})
	require.NoError(t, err)

	time.Sleep(80 * time.Millisecond)

	err = store.Fork(ctx, "conv-src", "conv-fork")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMemoryStore_TTLMessageCountExpired(t *testing.T) {
	store := NewMemoryStore(WithMemoryTTL(50 * time.Millisecond))
	ctx := context.Background()

	err := store.Save(ctx, &ConversationState{
		ID: "conv-mc",
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
	})
	require.NoError(t, err)

	time.Sleep(80 * time.Millisecond)

	_, err = store.MessageCount(ctx, "conv-mc")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMemoryStore_TTLLoadRecentMessagesExpired(t *testing.T) {
	store := NewMemoryStore(WithMemoryTTL(50 * time.Millisecond))
	ctx := context.Background()

	err := store.Save(ctx, &ConversationState{
		ID: "conv-lrm",
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
	})
	require.NoError(t, err)

	time.Sleep(80 * time.Millisecond)

	_, err = store.LoadRecentMessages(ctx, "conv-lrm", 1)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMemoryStore_TTLAppendMessagesExpired(t *testing.T) {
	store := NewMemoryStore(WithMemoryTTL(50 * time.Millisecond))
	ctx := context.Background()

	err := store.Save(ctx, &ConversationState{
		ID:     "conv-am",
		UserID: "user-alice",
		Messages: []types.Message{
			{Role: "user", Content: "Original"},
		},
	})
	require.NoError(t, err)

	time.Sleep(80 * time.Millisecond)

	// Appending to an expired entry creates a new state
	err = store.AppendMessages(ctx, "conv-am", []types.Message{
		{Role: "user", Content: "New message"},
	})
	require.NoError(t, err)

	loaded, err := store.Load(ctx, "conv-am")
	require.NoError(t, err)
	assert.Len(t, loaded.Messages, 1)
	assert.Equal(t, "New message", loaded.Messages[0].Content)
}

func TestMemoryStore_TTLLoadSummariesExpired(t *testing.T) {
	store := NewMemoryStore(WithMemoryTTL(50 * time.Millisecond))
	ctx := context.Background()

	err := store.Save(ctx, &ConversationState{
		ID: "conv-ls",
		Summaries: []Summary{
			{Content: "A summary"},
		},
	})
	require.NoError(t, err)

	time.Sleep(80 * time.Millisecond)

	summaries, err := store.LoadSummaries(ctx, "conv-ls")
	require.NoError(t, err)
	assert.Nil(t, summaries)
}

func TestMemoryStore_TTLSaveSummaryExpired(t *testing.T) {
	store := NewMemoryStore(WithMemoryTTL(50 * time.Millisecond))
	ctx := context.Background()

	err := store.Save(ctx, &ConversationState{ID: "conv-ss"})
	require.NoError(t, err)

	time.Sleep(80 * time.Millisecond)

	err = store.SaveSummary(ctx, "conv-ss", Summary{Content: "Late summary"})
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMemoryStore_MaxEntries(t *testing.T) {
	store := NewMemoryStore(WithMemoryMaxEntries(3))
	ctx := context.Background()

	// Save 3 entries
	for i := 0; i < 3; i++ {
		err := store.Save(ctx, &ConversationState{
			ID:     "conv-" + string(rune('a'+i)),
			UserID: "user-alice",
		})
		require.NoError(t, err)
		time.Sleep(5 * time.Millisecond) // ensure distinct LastAccessedAt
	}

	assert.Equal(t, 3, store.Len())

	// Saving a 4th should evict the LRU (conv-a)
	err := store.Save(ctx, &ConversationState{
		ID:     "conv-d",
		UserID: "user-alice",
	})
	require.NoError(t, err)
	assert.Equal(t, 3, store.Len())

	// conv-a should be evicted
	_, err = store.Load(ctx, "conv-a")
	assert.ErrorIs(t, err, ErrNotFound)

	// conv-b, conv-c, conv-d should still exist
	for _, id := range []string{"conv-b", "conv-c", "conv-d"} {
		_, err = store.Load(ctx, id)
		require.NoError(t, err, "expected %s to exist", id)
	}
}

func TestMemoryStore_MaxEntriesUpdateDoesNotEvict(t *testing.T) {
	store := NewMemoryStore(WithMemoryMaxEntries(2))
	ctx := context.Background()

	err := store.Save(ctx, &ConversationState{ID: "conv-1"})
	require.NoError(t, err)
	err = store.Save(ctx, &ConversationState{ID: "conv-2"})
	require.NoError(t, err)

	// Updating existing entry should not trigger eviction
	err = store.Save(ctx, &ConversationState{ID: "conv-1", TokenCount: 99})
	require.NoError(t, err)
	assert.Equal(t, 2, store.Len())

	loaded, err := store.Load(ctx, "conv-1")
	require.NoError(t, err)
	assert.Equal(t, 99, loaded.TokenCount)
}

func TestMemoryStore_BackgroundEviction(t *testing.T) {
	store := NewMemoryStore(
		WithMemoryTTL(50*time.Millisecond),
		WithMemoryEvictionInterval(30*time.Millisecond),
	)
	defer store.Close()
	ctx := context.Background()

	err := store.Save(ctx, &ConversationState{ID: "conv-bg"})
	require.NoError(t, err)
	assert.Equal(t, 1, store.Len())

	// Wait for TTL + eviction interval to pass
	time.Sleep(120 * time.Millisecond)

	// Background goroutine should have evicted the entry
	assert.Equal(t, 0, store.Len())
}

func TestMemoryStore_CloseIdempotent(t *testing.T) {
	store := NewMemoryStore(
		WithMemoryTTL(1*time.Second),
		WithMemoryEvictionInterval(100*time.Millisecond),
	)
	// Calling Close multiple times should not panic
	store.Close()
	store.Close()
}

func TestMemoryStore_CloseWithoutBackgroundEviction(t *testing.T) {
	store := NewMemoryStore()
	// Close on a store without background eviction should be safe
	store.Close()
}

func TestMemoryStore_TTLConcurrentAccess(t *testing.T) {
	store := NewMemoryStore(
		WithMemoryTTL(30*time.Millisecond),
		WithMemoryMaxEntries(50),
	)
	ctx := context.Background()

	const numGoroutines = 50
	const numOps = 20

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				convID := "conv-" + string(rune(id))

				_ = store.Save(ctx, &ConversationState{
					ID:         convID,
					UserID:     "user-concurrent",
					TokenCount: j,
				})

				_, _ = store.Load(ctx, convID)
				_, _ = store.List(ctx, ListOptions{UserID: "user-concurrent"})
				_, _ = store.MessageCount(ctx, convID)

				if j%3 == 0 {
					_ = store.Delete(ctx, convID)
				}
			}
		}(i)
	}

	wg.Wait()
}

func TestMemoryStore_MaxEntriesWithZeroIsUnlimited(t *testing.T) {
	store := NewMemoryStore(WithMemoryMaxEntries(0))
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		err := store.Save(ctx, &ConversationState{
			ID: "conv-" + string(rune('a'+i)),
		})
		require.NoError(t, err)
	}
	assert.Equal(t, 10, store.Len())
}

func TestMemoryStore_MaxEntriesNegativeIsIgnored(t *testing.T) {
	store := NewMemoryStore(WithMemoryMaxEntries(-5))
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		err := store.Save(ctx, &ConversationState{
			ID: "conv-" + string(rune('a'+i)),
		})
		require.NoError(t, err)
	}
	assert.Equal(t, 5, store.Len())
}

func TestMemoryStore_TTLWithMaxEntries(t *testing.T) {
	store := NewMemoryStore(
		WithMemoryTTL(50*time.Millisecond),
		WithMemoryMaxEntries(2),
	)
	ctx := context.Background()

	// Fill the store
	err := store.Save(ctx, &ConversationState{ID: "conv-1"})
	require.NoError(t, err)
	time.Sleep(5 * time.Millisecond)
	err = store.Save(ctx, &ConversationState{ID: "conv-2"})
	require.NoError(t, err)

	// Wait for TTL to expire
	time.Sleep(80 * time.Millisecond)

	// Saving a new entry should succeed (expired entries evicted by LRU path)
	err = store.Save(ctx, &ConversationState{ID: "conv-3"})
	require.NoError(t, err)

	// The expired ones should return not found
	_, err = store.Load(ctx, "conv-1")
	assert.ErrorIs(t, err, ErrNotFound)

	// The new one should be loadable
	_, err = store.Load(ctx, "conv-3")
	require.NoError(t, err)
}

func TestMemoryStore_UserIndexSetBehavior(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Save the same conversation twice — user index should not duplicate
	state := &ConversationState{
		ID:     "conv-1",
		UserID: "user-alice",
	}
	err := store.Save(ctx, state)
	require.NoError(t, err)
	err = store.Save(ctx, state)
	require.NoError(t, err)

	ids, err := store.List(ctx, ListOptions{UserID: "user-alice"})
	require.NoError(t, err)
	assert.Len(t, ids, 1)
}

func TestMemoryStore_EvictExpiredLocked(t *testing.T) {
	store := NewMemoryStore(WithMemoryTTL(50 * time.Millisecond))
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		err := store.Save(ctx, &ConversationState{
			ID:     "conv-" + string(rune('a'+i)),
			UserID: "user-alice",
		})
		require.NoError(t, err)
	}
	assert.Equal(t, 5, store.Len())

	time.Sleep(80 * time.Millisecond)

	// Trigger eviction manually via internal method
	store.mu.Lock()
	store.evictExpiredLocked()
	store.mu.Unlock()

	assert.Equal(t, 0, store.Len())
}

func TestMemoryStore_ForkMaxEntries(t *testing.T) {
	store := NewMemoryStore(WithMemoryMaxEntries(2))
	ctx := context.Background()

	err := store.Save(ctx, &ConversationState{ID: "conv-1"})
	require.NoError(t, err)
	time.Sleep(5 * time.Millisecond)
	err = store.Save(ctx, &ConversationState{ID: "conv-2"})
	require.NoError(t, err)

	// Fork when at max capacity should evict LRU (conv-1)
	err = store.Fork(ctx, "conv-2", "conv-3")
	require.NoError(t, err)

	assert.Equal(t, 2, store.Len())
	_, err = store.Load(ctx, "conv-1")
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = store.Load(ctx, "conv-3")
	require.NoError(t, err)
}
