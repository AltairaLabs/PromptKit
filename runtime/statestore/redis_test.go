package statestore

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupRedisStore creates a test Redis store with miniredis
func setupRedisStore(t *testing.T, opts ...RedisOption) (*RedisStore, *miniredis.Miniredis) {
	mr := miniredis.RunT(t)

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	store := NewRedisStore(client, opts...)
	return store, mr
}

func TestRedisStore_LoadNotFound(t *testing.T) {
	store, _ := setupRedisStore(t)
	ctx := context.Background()

	_, err := store.Load(ctx, "nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRedisStore_LoadInvalidID(t *testing.T) {
	store, _ := setupRedisStore(t)
	ctx := context.Background()

	_, err := store.Load(ctx, "")
	assert.ErrorIs(t, err, ErrInvalidID)
}

func TestRedisStore_SaveAndLoad(t *testing.T) {
	store, _ := setupRedisStore(t)
	ctx := context.Background()

	state := &ConversationState{
		ID:           "conv-123",
		UserID:       "user-alice",
		SystemPrompt: "You are a helpful assistant",
		Messages: []types.Message{
			{
				Role:      "user",
				Content:   "Hello",
				Timestamp: time.Now(),
			},
		},
		TokenCount: 1,
		Metadata:   map[string]interface{}{"test": "value"},
	}

	// Save
	err := store.Save(ctx, state)
	require.NoError(t, err)

	// Load
	loaded, err := store.Load(ctx, "conv-123")
	require.NoError(t, err)
	assert.Equal(t, "conv-123", loaded.ID)
	assert.Equal(t, "user-alice", loaded.UserID)
	assert.Equal(t, "You are a helpful assistant", loaded.SystemPrompt)
	assert.Len(t, loaded.Messages, 1)
	assert.Equal(t, "Hello", loaded.Messages[0].Content)
	assert.Equal(t, "value", loaded.Metadata["test"])
}

func TestRedisStore_SaveUpdatesExisting(t *testing.T) {
	store, _ := setupRedisStore(t)
	ctx := context.Background()

	// Save initial state
	state := &ConversationState{
		ID:         "conv-123",
		UserID:     "user-alice",
		TokenCount: 10,
	}
	err := store.Save(ctx, state)
	require.NoError(t, err)

	// Update state
	state.TokenCount = 20
	state.Messages = []types.Message{
		{Role: "user", Content: "Updated"},
	}
	err = store.Save(ctx, state)
	require.NoError(t, err)

	// Load and verify update
	loaded, err := store.Load(ctx, "conv-123")
	require.NoError(t, err)
	assert.Equal(t, 20, loaded.TokenCount)
	assert.Len(t, loaded.Messages, 1)
	assert.Equal(t, "Updated", loaded.Messages[0].Content)
}

func TestRedisStore_SaveInvalidState(t *testing.T) {
	store, _ := setupRedisStore(t)
	ctx := context.Background()

	err := store.Save(ctx, nil)
	assert.ErrorIs(t, err, ErrInvalidState)
}

func TestRedisStore_SaveInvalidID(t *testing.T) {
	store, _ := setupRedisStore(t)
	ctx := context.Background()

	state := &ConversationState{
		ID: "", // Empty ID
	}
	err := store.Save(ctx, state)
	assert.ErrorIs(t, err, ErrInvalidID)
}

func TestRedisStore_Delete(t *testing.T) {
	store, _ := setupRedisStore(t)
	ctx := context.Background()

	// Save a state
	state := &ConversationState{
		ID:     "conv-123",
		UserID: "user-alice",
	}
	err := store.Save(ctx, state)
	require.NoError(t, err)

	// Delete it
	err = store.Delete(ctx, "conv-123")
	require.NoError(t, err)

	// Verify it's gone
	_, err = store.Load(ctx, "conv-123")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRedisStore_DeleteNotFound(t *testing.T) {
	store, _ := setupRedisStore(t)
	ctx := context.Background()

	err := store.Delete(ctx, "nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRedisStore_DeleteInvalidID(t *testing.T) {
	store, _ := setupRedisStore(t)
	ctx := context.Background()

	err := store.Delete(ctx, "")
	assert.ErrorIs(t, err, ErrInvalidID)
}

func TestRedisStore_ListByUser(t *testing.T) {
	store, _ := setupRedisStore(t)
	ctx := context.Background()

	// Save conversations for different users
	for i := 1; i <= 3; i++ {
		state := &ConversationState{
			ID:     "alice-" + string(rune('0'+i)),
			UserID: "user-alice",
		}
		err := store.Save(ctx, state)
		require.NoError(t, err)
	}

	for i := 1; i <= 2; i++ {
		state := &ConversationState{
			ID:     "bob-" + string(rune('0'+i)),
			UserID: "user-bob",
		}
		err := store.Save(ctx, state)
		require.NoError(t, err)
	}

	// List Alice's conversations
	ids, err := store.List(ctx, ListOptions{UserID: "user-alice"})
	require.NoError(t, err)
	assert.Len(t, ids, 3)
	for _, id := range ids {
		assert.Contains(t, id, "alice")
	}

	// List Bob's conversations
	ids, err = store.List(ctx, ListOptions{UserID: "user-bob"})
	require.NoError(t, err)
	assert.Len(t, ids, 2)
	for _, id := range ids {
		assert.Contains(t, id, "bob")
	}

	// List nonexistent user
	ids, err = store.List(ctx, ListOptions{UserID: "user-charlie"})
	require.NoError(t, err)
	assert.Len(t, ids, 0)
}

func TestRedisStore_ListAll(t *testing.T) {
	store, _ := setupRedisStore(t)
	ctx := context.Background()

	// Save multiple conversations
	for i := 1; i <= 5; i++ {
		state := &ConversationState{
			ID:     "conv-" + string(rune('0'+i)),
			UserID: "user-alice",
		}
		err := store.Save(ctx, state)
		require.NoError(t, err)
	}

	// List all
	ids, err := store.List(ctx, ListOptions{})
	require.NoError(t, err)
	assert.Len(t, ids, 5)
}

func TestRedisStore_ListWithPagination(t *testing.T) {
	store, _ := setupRedisStore(t)
	ctx := context.Background()

	// Save 10 conversations
	for i := 0; i < 10; i++ {
		state := &ConversationState{
			ID:     "conv-" + string(rune('0'+i)),
			UserID: "user-alice",
		}
		err := store.Save(ctx, state)
		require.NoError(t, err)
	}

	// First page (limit 3)
	ids, err := store.List(ctx, ListOptions{
		UserID: "user-alice",
		Limit:  3,
		Offset: 0,
	})
	require.NoError(t, err)
	assert.Len(t, ids, 3)

	// Second page
	ids, err = store.List(ctx, ListOptions{
		UserID: "user-alice",
		Limit:  3,
		Offset: 3,
	})
	require.NoError(t, err)
	assert.Len(t, ids, 3)

	// Beyond last page
	ids, err = store.List(ctx, ListOptions{
		UserID: "user-alice",
		Limit:  3,
		Offset: 15,
	})
	require.NoError(t, err)
	assert.Len(t, ids, 0)
}

func TestRedisStore_ListSortByUpdatedAt(t *testing.T) {
	store, _ := setupRedisStore(t)
	ctx := context.Background()

	// Save conversations with staggered timing using actual delays
	state1 := &ConversationState{
		ID:     "conv-1",
		UserID: "user-alice",
	}
	err := store.Save(ctx, state1)
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)

	state2 := &ConversationState{
		ID:     "conv-2",
		UserID: "user-alice",
	}
	err = store.Save(ctx, state2)
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)

	state3 := &ConversationState{
		ID:     "conv-3",
		UserID: "user-alice",
	}
	err = store.Save(ctx, state3)
	require.NoError(t, err)

	// List sorted by updated_at descending (newest first)
	ids, err := store.List(ctx, ListOptions{
		UserID:    "user-alice",
		SortBy:    "updated_at",
		SortOrder: "desc",
	})
	require.NoError(t, err)
	assert.Equal(t, "conv-3", ids[0]) // Most recent
	assert.Equal(t, "conv-2", ids[1])
	assert.Equal(t, "conv-1", ids[2]) // Oldest

	// List sorted ascending
	ids, err = store.List(ctx, ListOptions{
		UserID:    "user-alice",
		SortBy:    "updated_at",
		SortOrder: "asc",
	})
	require.NoError(t, err)
	assert.Equal(t, "conv-1", ids[0]) // Oldest
	assert.Equal(t, "conv-2", ids[1])
	assert.Equal(t, "conv-3", ids[2]) // Most recent
}

func TestRedisStore_TTL(t *testing.T) {
	// Create store with short TTL for testing
	store, mr := setupRedisStore(t, WithTTL(100*time.Millisecond))
	ctx := context.Background()

	// Save a state
	state := &ConversationState{
		ID:     "conv-123",
		UserID: "user-alice",
	}
	err := store.Save(ctx, state)
	require.NoError(t, err)

	// Verify it exists
	_, err = store.Load(ctx, "conv-123")
	require.NoError(t, err)

	// Fast-forward time in miniredis
	mr.FastForward(200 * time.Millisecond)

	// Verify it's expired
	_, err = store.Load(ctx, "conv-123")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRedisStore_CustomPrefix(t *testing.T) {
	store, mr := setupRedisStore(t, WithPrefix("myapp"))
	ctx := context.Background()

	// Save a state
	state := &ConversationState{
		ID:     "conv-123",
		UserID: "user-alice",
	}
	err := store.Save(ctx, state)
	require.NoError(t, err)

	// Check Redis directly for key with custom prefix
	keys := mr.Keys()
	assert.Contains(t, keys, "myapp:conversation:conv-123")
	assert.Contains(t, keys, "myapp:user:user-alice:conversations")
}

func TestRedisStore_DeleteUpdatesUserIndex(t *testing.T) {
	store, _ := setupRedisStore(t)
	ctx := context.Background()

	// Save conversation
	state := &ConversationState{
		ID:     "conv-123",
		UserID: "user-alice",
	}
	err := store.Save(ctx, state)
	require.NoError(t, err)

	// Verify it's in user index
	ids, err := store.List(ctx, ListOptions{UserID: "user-alice"})
	require.NoError(t, err)
	assert.Len(t, ids, 1)

	// Delete conversation
	err = store.Delete(ctx, "conv-123")
	require.NoError(t, err)

	// Verify it's removed from user index
	ids, err = store.List(ctx, ListOptions{UserID: "user-alice"})
	require.NoError(t, err)
	assert.Len(t, ids, 0)
}

func TestRedisStore_DefaultLimit(t *testing.T) {
	store, _ := setupRedisStore(t)
	ctx := context.Background()

	// Save 150 conversations (more than default limit of 100)
	for i := 0; i < 150; i++ {
		state := &ConversationState{
			ID:     "conv-" + string(rune(i)),
			UserID: "user-alice",
		}
		err := store.Save(ctx, state)
		require.NoError(t, err)
	}

	// List without explicit limit (should default to 100)
	ids, err := store.List(ctx, ListOptions{UserID: "user-alice"})
	require.NoError(t, err)
	assert.Len(t, ids, 100)
}

func TestRedisStore_JSONSerialization(t *testing.T) {
	store, _ := setupRedisStore(t)
	ctx := context.Background()

	// Create a state with complex nested data
	state := &ConversationState{
		ID:     "conv-123",
		UserID: "user-alice",
		Messages: []types.Message{
			{
				Role:    "assistant",
				Content: "Response",
				ToolCalls: []types.MessageToolCall{
					{ID: "call-1", Name: "search", Args: []byte(`{"query":"test"}`)},
				},
			},
		},
		Summaries: []Summary{
			{StartTurn: 0, EndTurn: 10, Content: "Summary of conversation", TokenCount: 50},
		},
		Metadata: map[string]interface{}{
			"key1": "value1",
			"key2": map[string]interface{}{
				"nested": "data",
			},
		},
	}

	// Save and load
	err := store.Save(ctx, state)
	require.NoError(t, err)

	loaded, err := store.Load(ctx, "conv-123")
	require.NoError(t, err)

	// Verify complex data is preserved
	assert.Len(t, loaded.Messages, 1)
	assert.Len(t, loaded.Messages[0].ToolCalls, 1)
	assert.Equal(t, "search", loaded.Messages[0].ToolCalls[0].Name)
	assert.Len(t, loaded.Summaries, 1)
	assert.Equal(t, "Summary of conversation", loaded.Summaries[0].Content)
	assert.Equal(t, "value1", loaded.Metadata["key1"])
}
