package statestore

import (
	"context"
	"fmt"
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

func TestRedisStore_Fork(t *testing.T) {
	store, _ := setupRedisStore(t)
	ctx := context.Background()

	// Create original state
	original := &ConversationState{
		ID:           "conv-123",
		UserID:       "user-alice",
		SystemPrompt: "You are a helpful assistant",
		Messages: []types.Message{
			{Role: "user", Content: "Hello", Timestamp: time.Now()},
			{Role: "assistant", Content: "Hi there!", Timestamp: time.Now()},
		},
		TokenCount: 100,
		Metadata:   map[string]interface{}{"key": "value"},
	}

	// Save original
	err := store.Save(ctx, original)
	require.NoError(t, err)

	// Fork the conversation
	err = store.Fork(ctx, "conv-123", "conv-123-fork")
	require.NoError(t, err)

	// Load forked state
	forked, err := store.Load(ctx, "conv-123-fork")
	require.NoError(t, err)

	// Verify fork has new ID
	assert.Equal(t, "conv-123-fork", forked.ID)

	// Verify other fields are copied
	assert.Equal(t, original.UserID, forked.UserID)
	assert.Equal(t, original.SystemPrompt, forked.SystemPrompt)
	assert.Equal(t, original.TokenCount, forked.TokenCount)
	assert.Equal(t, len(original.Messages), len(forked.Messages))

	// Verify messages are copied
	for i := range original.Messages {
		assert.Equal(t, original.Messages[i].Role, forked.Messages[i].Role)
		assert.Equal(t, original.Messages[i].Content, forked.Messages[i].Content)
	}

	// Verify modifying fork doesn't affect original
	forked.Messages = append(forked.Messages, types.Message{
		Role:    "user",
		Content: "New message in fork",
	})
	err = store.Save(ctx, forked)
	require.NoError(t, err)

	// Load original again
	reloadedOriginal, err := store.Load(ctx, "conv-123")
	require.NoError(t, err)

	// Original should be unchanged
	assert.Equal(t, 2, len(reloadedOriginal.Messages))
	assert.NotEqual(t, len(reloadedOriginal.Messages), len(forked.Messages))
}

func TestRedisStore_ForkNotFound(t *testing.T) {
	store, _ := setupRedisStore(t)
	ctx := context.Background()

	err := store.Fork(ctx, "nonexistent", "fork-id")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRedisStore_ForkInvalidIDs(t *testing.T) {
	store, _ := setupRedisStore(t)
	ctx := context.Background()

	// Empty source ID
	err := store.Fork(ctx, "", "fork-id")
	assert.ErrorIs(t, err, ErrInvalidID)

	// Empty new ID
	err = store.Fork(ctx, "conv-123", "")
	assert.ErrorIs(t, err, ErrInvalidID)
}

func TestRedisStore_LoadRecentMessages(t *testing.T) {
	t.Run("fallback to monolithic key", func(t *testing.T) {
		store, _ := setupRedisStore(t)
		ctx := context.Background()

		// Save state with messages using the monolithic Save (no list key)
		state := &ConversationState{
			ID:     "conv-mono",
			UserID: "user-alice",
			Messages: []types.Message{
				{Role: "user", Content: "msg1"},
				{Role: "assistant", Content: "msg2"},
				{Role: "user", Content: "msg3"},
				{Role: "assistant", Content: "msg4"},
			},
		}
		err := store.Save(ctx, state)
		require.NoError(t, err)

		// Load last 2 messages — should fall back to monolithic key
		msgs, err := store.LoadRecentMessages(ctx, "conv-mono", 2)
		require.NoError(t, err)
		assert.Len(t, msgs, 2)
		assert.Equal(t, "msg3", msgs[0].Content)
		assert.Equal(t, "msg4", msgs[1].Content)
	})

	t.Run("with list format", func(t *testing.T) {
		store, _ := setupRedisStore(t)
		ctx := context.Background()

		// Append messages directly to list format
		messages := []types.Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi"},
			{Role: "user", Content: "how are you"},
			{Role: "assistant", Content: "I am fine"},
		}
		err := store.AppendMessages(ctx, "conv-list", messages)
		require.NoError(t, err)

		// Load last 2 messages from list
		msgs, err := store.LoadRecentMessages(ctx, "conv-list", 2)
		require.NoError(t, err)
		assert.Len(t, msgs, 2)
		assert.Equal(t, "how are you", msgs[0].Content)
		assert.Equal(t, "I am fine", msgs[1].Content)
	})

	t.Run("N greater than total", func(t *testing.T) {
		store, _ := setupRedisStore(t)
		ctx := context.Background()

		state := &ConversationState{
			ID:     "conv-few",
			UserID: "user-alice",
			Messages: []types.Message{
				{Role: "user", Content: "only one"},
			},
		}
		err := store.Save(ctx, state)
		require.NoError(t, err)

		// Request more messages than exist (monolithic fallback)
		msgs, err := store.LoadRecentMessages(ctx, "conv-few", 100)
		require.NoError(t, err)
		assert.Len(t, msgs, 1)
		assert.Equal(t, "only one", msgs[0].Content)
	})

	t.Run("N greater than total with list format", func(t *testing.T) {
		store, _ := setupRedisStore(t)
		ctx := context.Background()

		messages := []types.Message{
			{Role: "user", Content: "first"},
			{Role: "assistant", Content: "second"},
		}
		err := store.AppendMessages(ctx, "conv-few-list", messages)
		require.NoError(t, err)

		msgs, err := store.LoadRecentMessages(ctx, "conv-few-list", 100)
		require.NoError(t, err)
		assert.Len(t, msgs, 2)
		assert.Equal(t, "first", msgs[0].Content)
		assert.Equal(t, "second", msgs[1].Content)
	})

	t.Run("empty conversation", func(t *testing.T) {
		store, _ := setupRedisStore(t)
		ctx := context.Background()

		// No messages list and no monolithic key — should return ErrNotFound
		_, err := store.LoadRecentMessages(ctx, "conv-nonexistent", 5)
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("invalid ID", func(t *testing.T) {
		store, _ := setupRedisStore(t)
		ctx := context.Background()

		_, err := store.LoadRecentMessages(ctx, "", 5)
		assert.ErrorIs(t, err, ErrInvalidID)
	})
}

func TestRedisStore_MessageCount(t *testing.T) {
	t.Run("monolithic key fallback", func(t *testing.T) {
		store, _ := setupRedisStore(t)
		ctx := context.Background()

		state := &ConversationState{
			ID:     "conv-count-mono",
			UserID: "user-alice",
			Messages: []types.Message{
				{Role: "user", Content: "msg1"},
				{Role: "assistant", Content: "msg2"},
				{Role: "user", Content: "msg3"},
			},
		}
		err := store.Save(ctx, state)
		require.NoError(t, err)

		count, err := store.MessageCount(ctx, "conv-count-mono")
		require.NoError(t, err)
		assert.Equal(t, 3, count)
	})

	t.Run("list format", func(t *testing.T) {
		store, _ := setupRedisStore(t)
		ctx := context.Background()

		messages := []types.Message{
			{Role: "user", Content: "a"},
			{Role: "assistant", Content: "b"},
			{Role: "user", Content: "c"},
			{Role: "assistant", Content: "d"},
			{Role: "user", Content: "e"},
		}
		err := store.AppendMessages(ctx, "conv-count-list", messages)
		require.NoError(t, err)

		count, err := store.MessageCount(ctx, "conv-count-list")
		require.NoError(t, err)
		assert.Equal(t, 5, count)
	})

	t.Run("not found", func(t *testing.T) {
		store, _ := setupRedisStore(t)
		ctx := context.Background()

		_, err := store.MessageCount(ctx, "conv-nonexistent")
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("invalid ID", func(t *testing.T) {
		store, _ := setupRedisStore(t)
		ctx := context.Background()

		_, err := store.MessageCount(ctx, "")
		assert.ErrorIs(t, err, ErrInvalidID)
	})
}

func TestRedisStore_AppendMessages(t *testing.T) {
	t.Run("append to new conversation", func(t *testing.T) {
		store, _ := setupRedisStore(t)
		ctx := context.Background()

		messages := []types.Message{
			{Role: "user", Content: "first message"},
			{Role: "assistant", Content: "first reply"},
		}
		err := store.AppendMessages(ctx, "conv-new", messages)
		require.NoError(t, err)

		// Verify messages were stored
		msgs, err := store.LoadRecentMessages(ctx, "conv-new", 10)
		require.NoError(t, err)
		assert.Len(t, msgs, 2)
		assert.Equal(t, "first message", msgs[0].Content)
		assert.Equal(t, "first reply", msgs[1].Content)

		// Append more messages
		moreMessages := []types.Message{
			{Role: "user", Content: "second message"},
			{Role: "assistant", Content: "second reply"},
		}
		err = store.AppendMessages(ctx, "conv-new", moreMessages)
		require.NoError(t, err)

		// Verify all messages
		msgs, err = store.LoadRecentMessages(ctx, "conv-new", 10)
		require.NoError(t, err)
		assert.Len(t, msgs, 4)
		assert.Equal(t, "second reply", msgs[3].Content)
	})

	t.Run("migration from monolithic key", func(t *testing.T) {
		store, _ := setupRedisStore(t)
		ctx := context.Background()

		// Save a conversation using monolithic format
		state := &ConversationState{
			ID:     "conv-migrate",
			UserID: "user-alice",
			Messages: []types.Message{
				{Role: "user", Content: "old msg 1"},
				{Role: "assistant", Content: "old msg 2"},
			},
		}
		err := store.Save(ctx, state)
		require.NoError(t, err)

		// Append new messages — should trigger migration
		newMessages := []types.Message{
			{Role: "user", Content: "new msg 3"},
		}
		err = store.AppendMessages(ctx, "conv-migrate", newMessages)
		require.NoError(t, err)

		// Verify all messages are present (migrated + appended)
		msgs, err := store.LoadRecentMessages(ctx, "conv-migrate", 10)
		require.NoError(t, err)
		assert.Len(t, msgs, 3)
		assert.Equal(t, "old msg 1", msgs[0].Content)
		assert.Equal(t, "old msg 2", msgs[1].Content)
		assert.Equal(t, "new msg 3", msgs[2].Content)
	})

	t.Run("invalid ID", func(t *testing.T) {
		store, _ := setupRedisStore(t)
		ctx := context.Background()

		err := store.AppendMessages(ctx, "", []types.Message{{Role: "user", Content: "test"}})
		assert.ErrorIs(t, err, ErrInvalidID)
	})
}

func TestRedisStore_LoadSummaries(t *testing.T) {
	t.Run("load summaries from list", func(t *testing.T) {
		store, _ := setupRedisStore(t)
		ctx := context.Background()

		// Save summaries using SaveSummary
		s1 := Summary{StartTurn: 0, EndTurn: 5, Content: "Summary of turns 0-5", TokenCount: 30}
		s2 := Summary{StartTurn: 6, EndTurn: 10, Content: "Summary of turns 6-10", TokenCount: 25}

		err := store.SaveSummary(ctx, "conv-sum", s1)
		require.NoError(t, err)
		err = store.SaveSummary(ctx, "conv-sum", s2)
		require.NoError(t, err)

		// Load summaries
		summaries, err := store.LoadSummaries(ctx, "conv-sum")
		require.NoError(t, err)
		assert.Len(t, summaries, 2)
		assert.Equal(t, "Summary of turns 0-5", summaries[0].Content)
		assert.Equal(t, 6, summaries[1].StartTurn)
		assert.Equal(t, 25, summaries[1].TokenCount)
	})

	t.Run("no summaries returns nil", func(t *testing.T) {
		store, _ := setupRedisStore(t)
		ctx := context.Background()

		// No summaries list and no monolithic key
		summaries, err := store.LoadSummaries(ctx, "conv-no-sum")
		require.NoError(t, err)
		assert.Nil(t, summaries)
	})

	t.Run("fallback to monolithic key", func(t *testing.T) {
		store, _ := setupRedisStore(t)
		ctx := context.Background()

		// Save state with summaries in monolithic format
		state := &ConversationState{
			ID:     "conv-mono-sum",
			UserID: "user-alice",
			Summaries: []Summary{
				{StartTurn: 0, EndTurn: 3, Content: "Monolithic summary", TokenCount: 20},
			},
		}
		err := store.Save(ctx, state)
		require.NoError(t, err)

		// Load summaries — should fall back to monolithic key
		summaries, err := store.LoadSummaries(ctx, "conv-mono-sum")
		require.NoError(t, err)
		assert.Len(t, summaries, 1)
		assert.Equal(t, "Monolithic summary", summaries[0].Content)
	})

	t.Run("invalid ID", func(t *testing.T) {
		store, _ := setupRedisStore(t)
		ctx := context.Background()

		_, err := store.LoadSummaries(ctx, "")
		assert.ErrorIs(t, err, ErrInvalidID)
	})
}

func TestRedisStore_SaveSummary(t *testing.T) {
	t.Run("save and retrieve summary", func(t *testing.T) {
		store, _ := setupRedisStore(t)
		ctx := context.Background()

		summary := Summary{
			StartTurn:  0,
			EndTurn:    10,
			Content:    "This is a test summary",
			TokenCount: 42,
			CreatedAt:  time.Now(),
		}

		err := store.SaveSummary(ctx, "conv-save-sum", summary)
		require.NoError(t, err)

		// Verify by loading
		summaries, err := store.LoadSummaries(ctx, "conv-save-sum")
		require.NoError(t, err)
		assert.Len(t, summaries, 1)
		assert.Equal(t, "This is a test summary", summaries[0].Content)
		assert.Equal(t, 42, summaries[0].TokenCount)
		assert.Equal(t, 0, summaries[0].StartTurn)
		assert.Equal(t, 10, summaries[0].EndTurn)
	})

	t.Run("save multiple summaries preserves order", func(t *testing.T) {
		store, _ := setupRedisStore(t)
		ctx := context.Background()

		for i := 0; i < 3; i++ {
			s := Summary{
				StartTurn:  i * 10,
				EndTurn:    (i + 1) * 10,
				Content:    fmt.Sprintf("summary %d", i),
				TokenCount: 10 + i,
			}
			err := store.SaveSummary(ctx, "conv-multi-sum", s)
			require.NoError(t, err)
		}

		summaries, err := store.LoadSummaries(ctx, "conv-multi-sum")
		require.NoError(t, err)
		assert.Len(t, summaries, 3)
		for i := 0; i < 3; i++ {
			assert.Equal(t, fmt.Sprintf("summary %d", i), summaries[i].Content)
			assert.Equal(t, i*10, summaries[i].StartTurn)
		}
	})

	t.Run("invalid ID", func(t *testing.T) {
		store, _ := setupRedisStore(t)
		ctx := context.Background()

		err := store.SaveSummary(ctx, "", Summary{Content: "test"})
		assert.ErrorIs(t, err, ErrInvalidID)
	})
}

func TestRedisStore_MigrateSummaries(t *testing.T) {
	t.Run("summaries migrated during AppendMessages", func(t *testing.T) {
		store, _ := setupRedisStore(t)
		ctx := context.Background()

		// Save state with summaries in monolithic format
		state := &ConversationState{
			ID:     "conv-migrate-sum",
			UserID: "user-alice",
			Messages: []types.Message{
				{Role: "user", Content: "old msg 1"},
				{Role: "assistant", Content: "old msg 2"},
			},
			Summaries: []Summary{
				{StartTurn: 0, EndTurn: 5, Content: "First summary", TokenCount: 30},
				{StartTurn: 6, EndTurn: 10, Content: "Second summary", TokenCount: 25},
			},
		}
		err := store.Save(ctx, state)
		require.NoError(t, err)

		// AppendMessages triggers migration (ensureListFormat -> migrateToListFormat -> migrateSummaries)
		newMessages := []types.Message{
			{Role: "user", Content: "new msg 3"},
		}
		err = store.AppendMessages(ctx, "conv-migrate-sum", newMessages)
		require.NoError(t, err)

		// Verify summaries were migrated to list format
		summaries, err := store.LoadSummaries(ctx, "conv-migrate-sum")
		require.NoError(t, err)
		assert.Len(t, summaries, 2)
		assert.Equal(t, "First summary", summaries[0].Content)
		assert.Equal(t, 30, summaries[0].TokenCount)
		assert.Equal(t, "Second summary", summaries[1].Content)
		assert.Equal(t, 25, summaries[1].TokenCount)

		// Verify messages were also migrated and appended
		msgs, err := store.LoadRecentMessages(ctx, "conv-migrate-sum", 10)
		require.NoError(t, err)
		assert.Len(t, msgs, 3)
		assert.Equal(t, "old msg 1", msgs[0].Content)
		assert.Equal(t, "new msg 3", msgs[2].Content)
	})

	t.Run("empty summaries skipped during migration", func(t *testing.T) {
		store, mr := setupRedisStore(t)
		ctx := context.Background()

		// Save state with messages but NO summaries in monolithic format
		state := &ConversationState{
			ID:     "conv-migrate-no-sum",
			UserID: "user-alice",
			Messages: []types.Message{
				{Role: "user", Content: "msg1"},
			},
		}
		err := store.Save(ctx, state)
		require.NoError(t, err)

		// AppendMessages triggers migration
		err = store.AppendMessages(ctx, "conv-migrate-no-sum", []types.Message{
			{Role: "assistant", Content: "msg2"},
		})
		require.NoError(t, err)

		// Verify no summaries key was created (migrateSummaries returns early for empty slice)
		sumKey := store.summariesKey("conv-migrate-no-sum")
		exists := mr.Exists(sumKey)
		assert.False(t, exists, "summaries key should not exist when no summaries to migrate")
	})

	t.Run("summaries migrated with TTL", func(t *testing.T) {
		store, mr := setupRedisStore(t, WithTTL(5*time.Minute))
		ctx := context.Background()

		// Save state with summaries in monolithic format
		state := &ConversationState{
			ID:     "conv-migrate-sum-ttl",
			UserID: "user-alice",
			Messages: []types.Message{
				{Role: "user", Content: "msg1"},
			},
			Summaries: []Summary{
				{StartTurn: 0, EndTurn: 5, Content: "Summary with TTL", TokenCount: 20},
			},
		}
		err := store.Save(ctx, state)
		require.NoError(t, err)

		// AppendMessages triggers migration
		err = store.AppendMessages(ctx, "conv-migrate-sum-ttl", []types.Message{
			{Role: "assistant", Content: "msg2"},
		})
		require.NoError(t, err)

		// Verify summaries key has TTL set
		sumKey := store.summariesKey("conv-migrate-sum-ttl")
		ttl := mr.TTL(sumKey)
		assert.True(t, ttl > 0, "summaries key should have TTL set")

		// Fast-forward past TTL to verify expiration works
		mr.FastForward(6 * time.Minute)
		exists := mr.Exists(sumKey)
		assert.False(t, exists, "summaries key should expire after TTL")
	})
}

func TestRedisStore_AppendMessages_WithTTL(t *testing.T) {
	t.Run("messages key gets TTL", func(t *testing.T) {
		store, mr := setupRedisStore(t, WithTTL(10*time.Minute))
		ctx := context.Background()

		messages := []types.Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi"},
		}
		err := store.AppendMessages(ctx, "conv-ttl", messages)
		require.NoError(t, err)

		// Verify messages key has TTL
		msgKey := store.messagesKey("conv-ttl")
		ttl := mr.TTL(msgKey)
		assert.True(t, ttl > 0, "messages key should have TTL set")

		// Verify meta key has TTL
		metaKey := store.metaKey("conv-ttl")
		metaTTL := mr.TTL(metaKey)
		assert.True(t, metaTTL > 0, "meta key should have TTL set")
	})

	t.Run("no TTL when store has zero TTL", func(t *testing.T) {
		store, mr := setupRedisStore(t, WithTTL(0))
		ctx := context.Background()

		messages := []types.Message{
			{Role: "user", Content: "hello"},
		}
		err := store.AppendMessages(ctx, "conv-no-ttl", messages)
		require.NoError(t, err)

		// Verify messages key has no TTL (0 means no expiration)
		msgKey := store.messagesKey("conv-no-ttl")
		ttl := mr.TTL(msgKey)
		assert.Equal(t, time.Duration(0), ttl, "messages key should have no TTL")
	})

	t.Run("TTL expires messages", func(t *testing.T) {
		store, mr := setupRedisStore(t, WithTTL(5*time.Minute))
		ctx := context.Background()

		messages := []types.Message{
			{Role: "user", Content: "ephemeral"},
		}
		err := store.AppendMessages(ctx, "conv-expire", messages)
		require.NoError(t, err)

		// Verify messages exist
		msgs, err := store.LoadRecentMessages(ctx, "conv-expire", 10)
		require.NoError(t, err)
		assert.Len(t, msgs, 1)

		// Fast-forward past TTL
		mr.FastForward(6 * time.Minute)

		// Messages key should be expired; LoadRecentMessages falls through to monolithic (also missing)
		_, err = store.LoadRecentMessages(ctx, "conv-expire", 10)
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("migration from monolithic with TTL sets expiry on messages key", func(t *testing.T) {
		store, mr := setupRedisStore(t, WithTTL(10*time.Minute))
		ctx := context.Background()

		// Save monolithic state
		state := &ConversationState{
			ID:     "conv-migrate-ttl",
			UserID: "user-alice",
			Messages: []types.Message{
				{Role: "user", Content: "old msg"},
			},
		}
		err := store.Save(ctx, state)
		require.NoError(t, err)

		// AppendMessages triggers migration
		err = store.AppendMessages(ctx, "conv-migrate-ttl", []types.Message{
			{Role: "assistant", Content: "new msg"},
		})
		require.NoError(t, err)

		// Verify messages key has TTL after migration
		msgKey := store.messagesKey("conv-migrate-ttl")
		ttl := mr.TTL(msgKey)
		assert.True(t, ttl > 0, "migrated messages key should have TTL set")
	})
}

func TestRedisStore_LoadSummaries_FromMonolithic(t *testing.T) {
	t.Run("loads summaries from monolithic state with multiple summaries", func(t *testing.T) {
		store, _ := setupRedisStore(t)
		ctx := context.Background()

		// Save state with multiple summaries in monolithic format
		state := &ConversationState{
			ID:     "conv-mono-multi-sum",
			UserID: "user-alice",
			Summaries: []Summary{
				{StartTurn: 0, EndTurn: 5, Content: "First chunk", TokenCount: 15},
				{StartTurn: 6, EndTurn: 10, Content: "Second chunk", TokenCount: 20},
				{StartTurn: 11, EndTurn: 15, Content: "Third chunk", TokenCount: 18},
			},
		}
		err := store.Save(ctx, state)
		require.NoError(t, err)

		// Load summaries — should fall back to monolithic key
		summaries, err := store.LoadSummaries(ctx, "conv-mono-multi-sum")
		require.NoError(t, err)
		assert.Len(t, summaries, 3)
		assert.Equal(t, "First chunk", summaries[0].Content)
		assert.Equal(t, 15, summaries[0].TokenCount)
		assert.Equal(t, "Second chunk", summaries[1].Content)
		assert.Equal(t, "Third chunk", summaries[2].Content)
		assert.Equal(t, 11, summaries[2].StartTurn)
		assert.Equal(t, 15, summaries[2].EndTurn)
	})

	t.Run("monolithic state with no summaries returns empty", func(t *testing.T) {
		store, _ := setupRedisStore(t)
		ctx := context.Background()

		// Save state with NO summaries
		state := &ConversationState{
			ID:     "conv-mono-no-sum",
			UserID: "user-alice",
			Messages: []types.Message{
				{Role: "user", Content: "hello"},
			},
		}
		err := store.Save(ctx, state)
		require.NoError(t, err)

		// Load summaries — monolithic key exists but has no summaries
		summaries, err := store.LoadSummaries(ctx, "conv-mono-no-sum")
		require.NoError(t, err)
		assert.Empty(t, summaries)
	})

	t.Run("monolithic summaries with timestamps preserved", func(t *testing.T) {
		store, _ := setupRedisStore(t)
		ctx := context.Background()

		createdAt := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
		state := &ConversationState{
			ID:     "conv-mono-sum-ts",
			UserID: "user-alice",
			Summaries: []Summary{
				{
					StartTurn:  0,
					EndTurn:    5,
					Content:    "Summary with timestamp",
					TokenCount: 25,
					CreatedAt:  createdAt,
				},
			},
		}
		err := store.Save(ctx, state)
		require.NoError(t, err)

		summaries, err := store.LoadSummaries(ctx, "conv-mono-sum-ts")
		require.NoError(t, err)
		assert.Len(t, summaries, 1)
		assert.Equal(t, createdAt, summaries[0].CreatedAt)
	})
}

func TestRedisStore_SaveSummary_WithTTL(t *testing.T) {
	t.Run("summary key gets TTL", func(t *testing.T) {
		store, mr := setupRedisStore(t, WithTTL(10*time.Minute))
		ctx := context.Background()

		summary := Summary{
			StartTurn:  0,
			EndTurn:    5,
			Content:    "TTL summary",
			TokenCount: 20,
		}
		err := store.SaveSummary(ctx, "conv-sum-ttl", summary)
		require.NoError(t, err)

		// Verify summaries key has TTL
		sumKey := store.summariesKey("conv-sum-ttl")
		ttl := mr.TTL(sumKey)
		assert.True(t, ttl > 0, "summaries key should have TTL set")
	})

	t.Run("no TTL when store has zero TTL", func(t *testing.T) {
		store, mr := setupRedisStore(t, WithTTL(0))
		ctx := context.Background()

		summary := Summary{
			StartTurn:  0,
			EndTurn:    5,
			Content:    "No TTL summary",
			TokenCount: 15,
		}
		err := store.SaveSummary(ctx, "conv-sum-no-ttl", summary)
		require.NoError(t, err)

		// Verify no TTL on summaries key
		sumKey := store.summariesKey("conv-sum-no-ttl")
		ttl := mr.TTL(sumKey)
		assert.Equal(t, time.Duration(0), ttl, "summaries key should have no TTL")
	})

	t.Run("TTL expires summaries", func(t *testing.T) {
		store, mr := setupRedisStore(t, WithTTL(5*time.Minute))
		ctx := context.Background()

		summary := Summary{
			StartTurn:  0,
			EndTurn:    5,
			Content:    "Ephemeral summary",
			TokenCount: 10,
		}
		err := store.SaveSummary(ctx, "conv-sum-expire", summary)
		require.NoError(t, err)

		// Verify summary exists
		summaries, err := store.LoadSummaries(ctx, "conv-sum-expire")
		require.NoError(t, err)
		assert.Len(t, summaries, 1)

		// Fast-forward past TTL
		mr.FastForward(6 * time.Minute)

		// Summary key should be expired; LoadSummaries falls to monolithic (also missing) -> nil
		summaries, err = store.LoadSummaries(ctx, "conv-sum-expire")
		require.NoError(t, err)
		assert.Nil(t, summaries)
	})

	t.Run("multiple saves update TTL", func(t *testing.T) {
		store, mr := setupRedisStore(t, WithTTL(10*time.Minute))
		ctx := context.Background()

		s1 := Summary{StartTurn: 0, EndTurn: 5, Content: "First", TokenCount: 10}
		err := store.SaveSummary(ctx, "conv-sum-multi-ttl", s1)
		require.NoError(t, err)

		// Fast-forward 7 minutes (within TTL)
		mr.FastForward(7 * time.Minute)

		// Save another summary — this should refresh the TTL
		s2 := Summary{StartTurn: 6, EndTurn: 10, Content: "Second", TokenCount: 12}
		err = store.SaveSummary(ctx, "conv-sum-multi-ttl", s2)
		require.NoError(t, err)

		// Fast-forward 7 more minutes (14 total — past original TTL, but within refreshed TTL)
		mr.FastForward(7 * time.Minute)

		// Summaries should still exist because the second SaveSummary refreshed the TTL
		summaries, err := store.LoadSummaries(ctx, "conv-sum-multi-ttl")
		require.NoError(t, err)
		assert.Len(t, summaries, 2)
		assert.Equal(t, "First", summaries[0].Content)
		assert.Equal(t, "Second", summaries[1].Content)
	})
}
