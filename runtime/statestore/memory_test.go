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

func TestMemoryStore_LoadNotFound(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	_, err := store.Load(ctx, "nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMemoryStore_LoadInvalidID(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	_, err := store.Load(ctx, "")
	assert.ErrorIs(t, err, ErrInvalidID)
}

func TestMemoryStore_SaveAndLoad(t *testing.T) {
	store := NewMemoryStore()
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

func TestMemoryStore_SaveUpdatesExisting(t *testing.T) {
	store := NewMemoryStore()
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

func TestMemoryStore_SaveInvalidState(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	err := store.Save(ctx, nil)
	assert.ErrorIs(t, err, ErrInvalidState)
}

func TestMemoryStore_SaveInvalidID(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	state := &ConversationState{
		ID: "", // Empty ID
	}
	err := store.Save(ctx, state)
	assert.ErrorIs(t, err, ErrInvalidID)
}

func TestMemoryStore_Delete(t *testing.T) {
	store := NewMemoryStore()
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

func TestMemoryStore_DeleteNotFound(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	err := store.Delete(ctx, "nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMemoryStore_DeleteInvalidID(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	err := store.Delete(ctx, "")
	assert.ErrorIs(t, err, ErrInvalidID)
}

func TestMemoryStore_ListAll(t *testing.T) {
	store := NewMemoryStore()
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

func TestMemoryStore_ListByUser(t *testing.T) {
	store := NewMemoryStore()
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

func TestMemoryStore_ListWithPagination(t *testing.T) {
	store := NewMemoryStore()
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

	// Third page
	ids, err = store.List(ctx, ListOptions{
		UserID: "user-alice",
		Limit:  3,
		Offset: 6,
	})
	require.NoError(t, err)
	assert.Len(t, ids, 3)

	// Fourth page (only 1 remaining)
	ids, err = store.List(ctx, ListOptions{
		UserID: "user-alice",
		Limit:  3,
		Offset: 9,
	})
	require.NoError(t, err)
	assert.Len(t, ids, 1)

	// Beyond last page
	ids, err = store.List(ctx, ListOptions{
		UserID: "user-alice",
		Limit:  3,
		Offset: 15,
	})
	require.NoError(t, err)
	assert.Len(t, ids, 0)
}

func TestMemoryStore_ListSortByUpdatedAt(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Save conversations with staggered timing to establish order
	// Save in specific order with delays to ensure LastAccessedAt differs
	state1 := &ConversationState{
		ID:     "conv-1",
		UserID: "user-alice",
	}
	err := store.Save(ctx, state1)
	require.NoError(t, err)
	time.Sleep(10 * time.Millisecond)

	state2 := &ConversationState{
		ID:     "conv-2",
		UserID: "user-alice",
	}
	err = store.Save(ctx, state2)
	require.NoError(t, err)
	time.Sleep(10 * time.Millisecond)

	state3 := &ConversationState{
		ID:     "conv-3",
		UserID: "user-alice",
	}
	err = store.Save(ctx, state3)
	require.NoError(t, err)

	// List sorted by updated_at descending (newest first - default)
	ids, err := store.List(ctx, ListOptions{
		UserID:    "user-alice",
		SortBy:    "updated_at",
		SortOrder: "desc",
	})
	require.NoError(t, err)
	assert.Equal(t, "conv-3", ids[0]) // Most recent
	assert.Equal(t, "conv-2", ids[1])
	assert.Equal(t, "conv-1", ids[2]) // Oldest

	// List sorted ascending (oldest first)
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

func TestMemoryStore_ListSortByCreatedAt(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Save conversations with different creation times
	now := time.Now()
	states := []*ConversationState{
		{
			ID:     "conv-1",
			UserID: "user-alice",
			Messages: []types.Message{
				{Timestamp: now.Add(-3 * time.Hour)},
			},
		},
		{
			ID:     "conv-2",
			UserID: "user-alice",
			Messages: []types.Message{
				{Timestamp: now.Add(-1 * time.Hour)},
			},
		},
		{
			ID:     "conv-3",
			UserID: "user-alice",
			Messages: []types.Message{
				{Timestamp: now.Add(-2 * time.Hour)},
			},
		},
	}

	for _, state := range states {
		err := store.Save(ctx, state)
		require.NoError(t, err)
	}

	// List sorted by created_at descending
	ids, err := store.List(ctx, ListOptions{
		UserID:    "user-alice",
		SortBy:    "created_at",
		SortOrder: "desc",
	})
	require.NoError(t, err)
	assert.Equal(t, "conv-2", ids[0]) // Newest
	assert.Equal(t, "conv-3", ids[1])
	assert.Equal(t, "conv-1", ids[2]) // Oldest
}

func TestMemoryStore_DeepCopyPreventsExternalMutation(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Save state
	originalMetadata := map[string]interface{}{"key": "original"}
	state := &ConversationState{
		ID:       "conv-123",
		UserID:   "user-alice",
		Metadata: originalMetadata,
	}
	err := store.Save(ctx, state)
	require.NoError(t, err)

	// Load and mutate
	loaded, err := store.Load(ctx, "conv-123")
	require.NoError(t, err)
	loaded.Metadata["key"] = "mutated"

	// Load again and verify original value is preserved
	loaded2, err := store.Load(ctx, "conv-123")
	require.NoError(t, err)
	assert.Equal(t, "original", loaded2.Metadata["key"])
}

func TestMemoryStore_ConcurrentAccess(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Number of concurrent operations
	const numGoroutines = 100
	const numOpsPerGoroutine = 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Run concurrent save/load/delete operations
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			for j := 0; j < numOpsPerGoroutine; j++ {
				convID := "conv-" + string(rune('0'+id))

				// Save
				state := &ConversationState{
					ID:         convID,
					UserID:     "user-concurrent",
					TokenCount: j,
				}
				_ = store.Save(ctx, state)

				// Load
				_, _ = store.Load(ctx, convID)

				// List
				_, _ = store.List(ctx, ListOptions{UserID: "user-concurrent"})

				// Delete (occasionally)
				if j%3 == 0 {
					_ = store.Delete(ctx, convID)
				}
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// If we reach here without data races or panics, the test passes
	// (Run with -race flag to detect data races)
}

func TestMemoryStore_DeleteUpdatesUserIndex(t *testing.T) {
	store := NewMemoryStore()
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

func TestMemoryStore_DefaultLimit(t *testing.T) {
	store := NewMemoryStore()
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

func TestMemoryStore_DeepCloneMessageMeta(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Create state with message containing Meta field (simulating assertions)
	assertionResults := map[string]interface{}{
		"content_includes": map[string]interface{}{
			"passed":  true,
			"details": map[string]interface{}{"matched": true},
		},
		"content_matches": map[string]interface{}{
			"passed":  true,
			"details": map[string]interface{}{"pattern": ".*hello.*"},
		},
	}

	state := &ConversationState{
		ID:     "conv-123",
		UserID: "user-alice",
		Messages: []types.Message{
			{
				Role:      "user",
				Content:   "Hello",
				Timestamp: time.Now(),
			},
			{
				Role:      "assistant",
				Content:   "Hello! How can I help you?",
				Timestamp: time.Now(),
				Meta: map[string]interface{}{
					"assertions": assertionResults,
					"other_data": "some value",
				},
			},
		},
	}

	// Save the state
	err := store.Save(ctx, state)
	require.NoError(t, err)

	// Modify the original message Meta after saving
	state.Messages[1].Meta["assertions"] = map[string]interface{}{
		"modified": true,
	}
	state.Messages[1].Meta["new_key"] = "new value"

	// Load the state
	loaded, err := store.Load(ctx, "conv-123")
	require.NoError(t, err)

	// Verify the loaded state has the original Meta, not the modified version
	require.Len(t, loaded.Messages, 2)
	assistantMsg := loaded.Messages[1]

	// Check that assertions are preserved
	require.NotNil(t, assistantMsg.Meta)
	assertions, ok := assistantMsg.Meta["assertions"]
	require.True(t, ok, "assertions key should exist in Meta")

	assertionsMap, ok := assertions.(map[string]interface{})
	require.True(t, ok, "assertions should be a map")

	// Verify content_includes assertion
	contentIncludes, ok := assertionsMap["content_includes"]
	require.True(t, ok, "content_includes should exist")
	contentIncludesMap := contentIncludes.(map[string]interface{})
	assert.Equal(t, true, contentIncludesMap["passed"])

	// Verify content_matches assertion
	contentMatches, ok := assertionsMap["content_matches"]
	require.True(t, ok, "content_matches should exist")
	contentMatchesMap := contentMatches.(map[string]interface{})
	assert.Equal(t, true, contentMatchesMap["passed"])

	// Verify other_data is preserved
	assert.Equal(t, "some value", assistantMsg.Meta["other_data"])

	// Verify modified keys are NOT present
	_, hasModified := assertionsMap["modified"]
	assert.False(t, hasModified, "modified key should not exist")
	_, hasNewKey := assistantMsg.Meta["new_key"]
	assert.False(t, hasNewKey, "new_key should not exist")
}

func TestMemoryStore_DeepCloneNestedStructures(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Create complex nested structure in Meta
	state := &ConversationState{
		ID:     "conv-123",
		UserID: "user-alice",
		Messages: []types.Message{
			{
				Role:      "assistant",
				Content:   "Response with complex data",
				Timestamp: time.Now(),
				CostInfo: &types.CostInfo{
					InputTokens:   10,
					OutputTokens:  5,
					InputCostUSD:  0.0001,
					OutputCostUSD: 0.0002,
					TotalCost:     0.0003,
				},
				Validations: []types.ValidationResult{
					{
						ValidatorType: "banned_words",
						Passed:        true,
						Details: map[string]interface{}{
							"checked_words": []string{"word1", "word2"},
						},
						Timestamp: time.Now(),
					},
				},
				Meta: map[string]interface{}{
					"nested": map[string]interface{}{
						"level2": map[string]interface{}{
							"level3": "deep value",
						},
					},
					"array": []interface{}{"item1", "item2"},
				},
			},
		},
	}

	// Save
	err := store.Save(ctx, state)
	require.NoError(t, err)

	// Modify original after save
	state.Messages[0].CostInfo.InputTokens = 999
	state.Messages[0].Validations[0].Passed = false
	state.Messages[0].Meta["nested"].(map[string]interface{})["level2"].(map[string]interface{})["level3"] = "modified"
	state.Messages[0].Meta["array"].([]interface{})[0] = "modified"

	// Load and verify original values are preserved
	loaded, err := store.Load(ctx, "conv-123")
	require.NoError(t, err)

	msg := loaded.Messages[0]

	// Verify CostInfo preserved
	assert.Equal(t, 10, msg.CostInfo.InputTokens)
	assert.Equal(t, 0.0003, msg.CostInfo.TotalCost)

	// Verify Validations preserved
	assert.True(t, msg.Validations[0].Passed)
	assert.Equal(t, "banned_words", msg.Validations[0].ValidatorType)

	// Verify nested Meta preserved
	nested := msg.Meta["nested"].(map[string]interface{})
	level2 := nested["level2"].(map[string]interface{})
	assert.Equal(t, "deep value", level2["level3"])

	// Verify array preserved
	arr := msg.Meta["array"].([]interface{})
	assert.Equal(t, "item1", arr[0])
}

func TestMemoryStore_Fork(t *testing.T) {
	store := NewMemoryStore()
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

func TestMemoryStore_ForkNotFound(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	err := store.Fork(ctx, "nonexistent", "fork-id")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMemoryStore_ForkInvalidIDs(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Empty source ID
	err := store.Fork(ctx, "", "fork-id")
	assert.ErrorIs(t, err, ErrInvalidID)

	// Empty new ID
	err = store.Fork(ctx, "conv-123", "")
	assert.ErrorIs(t, err, ErrInvalidID)
}

func TestMemoryStore_LoadRecentMessages(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Save a conversation with 5 messages
	state := &ConversationState{
		ID:     "conv-recent",
		UserID: "user-alice",
		Messages: []types.Message{
			{Role: "user", Content: "msg-1", Timestamp: time.Now()},
			{Role: "assistant", Content: "msg-2", Timestamp: time.Now()},
			{Role: "user", Content: "msg-3", Timestamp: time.Now()},
			{Role: "assistant", Content: "msg-4", Timestamp: time.Now()},
			{Role: "user", Content: "msg-5", Timestamp: time.Now()},
		},
	}
	err := store.Save(ctx, state)
	require.NoError(t, err)

	// Load last 2 messages
	msgs, err := store.LoadRecentMessages(ctx, "conv-recent", 2)
	require.NoError(t, err)
	assert.Len(t, msgs, 2)
	assert.Equal(t, "msg-4", msgs[0].Content)
	assert.Equal(t, "msg-5", msgs[1].Content)

	// Load last 3 messages
	msgs, err = store.LoadRecentMessages(ctx, "conv-recent", 3)
	require.NoError(t, err)
	assert.Len(t, msgs, 3)
	assert.Equal(t, "msg-3", msgs[0].Content)
	assert.Equal(t, "msg-4", msgs[1].Content)
	assert.Equal(t, "msg-5", msgs[2].Content)

	// N greater than total messages — returns all
	msgs, err = store.LoadRecentMessages(ctx, "conv-recent", 100)
	require.NoError(t, err)
	assert.Len(t, msgs, 5)
	assert.Equal(t, "msg-1", msgs[0].Content)
	assert.Equal(t, "msg-5", msgs[4].Content)

	// Empty conversation (no messages)
	emptyState := &ConversationState{
		ID:     "conv-empty",
		UserID: "user-alice",
	}
	err = store.Save(ctx, emptyState)
	require.NoError(t, err)

	msgs, err = store.LoadRecentMessages(ctx, "conv-empty", 5)
	require.NoError(t, err)
	assert.Len(t, msgs, 0)

	// Invalid ID
	_, err = store.LoadRecentMessages(ctx, "", 5)
	assert.ErrorIs(t, err, ErrInvalidID)

	// Not found
	_, err = store.LoadRecentMessages(ctx, "nonexistent", 5)
	assert.ErrorIs(t, err, ErrNotFound)

	// Deep copy verification — mutating returned messages should not affect store
	msgs, err = store.LoadRecentMessages(ctx, "conv-recent", 1)
	require.NoError(t, err)
	msgs[0].Content = "mutated"

	msgs2, err := store.LoadRecentMessages(ctx, "conv-recent", 1)
	require.NoError(t, err)
	assert.Equal(t, "msg-5", msgs2[0].Content)
}

func TestMemoryStore_MessageCount(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Save a conversation with messages
	state := &ConversationState{
		ID:     "conv-count",
		UserID: "user-alice",
		Messages: []types.Message{
			{Role: "user", Content: "Hello", Timestamp: time.Now()},
			{Role: "assistant", Content: "Hi", Timestamp: time.Now()},
			{Role: "user", Content: "How are you?", Timestamp: time.Now()},
		},
	}
	err := store.Save(ctx, state)
	require.NoError(t, err)

	// Count messages
	count, err := store.MessageCount(ctx, "conv-count")
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	// Empty conversation
	emptyState := &ConversationState{
		ID:     "conv-empty-count",
		UserID: "user-alice",
	}
	err = store.Save(ctx, emptyState)
	require.NoError(t, err)

	count, err = store.MessageCount(ctx, "conv-empty-count")
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Not found
	_, err = store.MessageCount(ctx, "nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)

	// Invalid ID
	_, err = store.MessageCount(ctx, "")
	assert.ErrorIs(t, err, ErrInvalidID)
}

func TestMemoryStore_AppendMessages(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Save a conversation with initial messages
	state := &ConversationState{
		ID:     "conv-append",
		UserID: "user-alice",
		Messages: []types.Message{
			{Role: "user", Content: "Hello", Timestamp: time.Now()},
			{Role: "assistant", Content: "Hi there!", Timestamp: time.Now()},
		},
	}
	err := store.Save(ctx, state)
	require.NoError(t, err)

	// Append new messages to existing conversation
	newMsgs := []types.Message{
		{Role: "user", Content: "What is Go?", Timestamp: time.Now()},
		{Role: "assistant", Content: "Go is a programming language.", Timestamp: time.Now()},
	}
	err = store.AppendMessages(ctx, "conv-append", newMsgs)
	require.NoError(t, err)

	// Verify messages were appended
	loaded, err := store.Load(ctx, "conv-append")
	require.NoError(t, err)
	assert.Len(t, loaded.Messages, 4)
	assert.Equal(t, "Hello", loaded.Messages[0].Content)
	assert.Equal(t, "Hi there!", loaded.Messages[1].Content)
	assert.Equal(t, "What is Go?", loaded.Messages[2].Content)
	assert.Equal(t, "Go is a programming language.", loaded.Messages[3].Content)

	// Append to non-existent conversation — creates new state
	createMsgs := []types.Message{
		{Role: "user", Content: "First message", Timestamp: time.Now()},
	}
	err = store.AppendMessages(ctx, "conv-new", createMsgs)
	require.NoError(t, err)

	loaded, err = store.Load(ctx, "conv-new")
	require.NoError(t, err)
	assert.Equal(t, "conv-new", loaded.ID)
	assert.Len(t, loaded.Messages, 1)
	assert.Equal(t, "First message", loaded.Messages[0].Content)

	// Invalid ID
	err = store.AppendMessages(ctx, "", newMsgs)
	assert.ErrorIs(t, err, ErrInvalidID)

	// Deep copy verification — mutating original messages after append should not affect store
	mutableMsgs := []types.Message{
		{Role: "user", Content: "original", Timestamp: time.Now(), Meta: map[string]interface{}{"key": "value"}},
	}
	err = store.AppendMessages(ctx, "conv-deepcopy", mutableMsgs)
	require.NoError(t, err)

	// Mutate the original slice
	mutableMsgs[0].Content = "mutated"
	mutableMsgs[0].Meta["key"] = "mutated"

	// Verify stored version is unchanged
	loaded, err = store.Load(ctx, "conv-deepcopy")
	require.NoError(t, err)
	assert.Equal(t, "original", loaded.Messages[0].Content)
	assert.Equal(t, "value", loaded.Messages[0].Meta["key"])
}

func TestMemoryStore_LoadSummaries(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Save a conversation with summaries
	now := time.Now()
	state := &ConversationState{
		ID:     "conv-summaries",
		UserID: "user-alice",
		Messages: []types.Message{
			{Role: "user", Content: "Hello", Timestamp: now},
		},
		Summaries: []Summary{
			{
				StartTurn:  0,
				EndTurn:    5,
				Content:    "User discussed Go programming basics.",
				TokenCount: 20,
				CreatedAt:  now.Add(-1 * time.Hour),
			},
			{
				StartTurn:  6,
				EndTurn:    10,
				Content:    "User asked about concurrency patterns.",
				TokenCount: 25,
				CreatedAt:  now,
			},
		},
	}
	err := store.Save(ctx, state)
	require.NoError(t, err)

	// Load summaries
	summaries, err := store.LoadSummaries(ctx, "conv-summaries")
	require.NoError(t, err)
	assert.Len(t, summaries, 2)
	assert.Equal(t, "User discussed Go programming basics.", summaries[0].Content)
	assert.Equal(t, 0, summaries[0].StartTurn)
	assert.Equal(t, 5, summaries[0].EndTurn)
	assert.Equal(t, "User asked about concurrency patterns.", summaries[1].Content)
	assert.Equal(t, 25, summaries[1].TokenCount)

	// No summaries — conversation exists but has no summaries
	noSummaryState := &ConversationState{
		ID:     "conv-no-summaries",
		UserID: "user-alice",
		Messages: []types.Message{
			{Role: "user", Content: "Hello", Timestamp: now},
		},
	}
	err = store.Save(ctx, noSummaryState)
	require.NoError(t, err)

	summaries, err = store.LoadSummaries(ctx, "conv-no-summaries")
	require.NoError(t, err)
	assert.Nil(t, summaries)

	// Non-existent conversation — returns nil, nil
	summaries, err = store.LoadSummaries(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, summaries)

	// Invalid ID
	_, err = store.LoadSummaries(ctx, "")
	assert.ErrorIs(t, err, ErrInvalidID)

	// Deep copy verification — mutating returned summaries should not affect store
	summaries, err = store.LoadSummaries(ctx, "conv-summaries")
	require.NoError(t, err)
	summaries[0].Content = "mutated"

	summaries2, err := store.LoadSummaries(ctx, "conv-summaries")
	require.NoError(t, err)
	assert.Equal(t, "User discussed Go programming basics.", summaries2[0].Content)
}

func TestMemoryStore_SaveSummary(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Save a conversation
	now := time.Now()
	state := &ConversationState{
		ID:     "conv-save-summary",
		UserID: "user-alice",
		Messages: []types.Message{
			{Role: "user", Content: "Hello", Timestamp: now},
		},
	}
	err := store.Save(ctx, state)
	require.NoError(t, err)

	// Save first summary
	summary1 := Summary{
		StartTurn:  0,
		EndTurn:    5,
		Content:    "First summary of the conversation.",
		TokenCount: 15,
		CreatedAt:  now,
	}
	err = store.SaveSummary(ctx, "conv-save-summary", summary1)
	require.NoError(t, err)

	// Verify first summary was saved
	loaded, err := store.Load(ctx, "conv-save-summary")
	require.NoError(t, err)
	assert.Len(t, loaded.Summaries, 1)
	assert.Equal(t, "First summary of the conversation.", loaded.Summaries[0].Content)

	// Save second summary — should append
	summary2 := Summary{
		StartTurn:  6,
		EndTurn:    10,
		Content:    "Second summary of the conversation.",
		TokenCount: 18,
		CreatedAt:  now.Add(1 * time.Hour),
	}
	err = store.SaveSummary(ctx, "conv-save-summary", summary2)
	require.NoError(t, err)

	// Verify both summaries exist
	loaded, err = store.Load(ctx, "conv-save-summary")
	require.NoError(t, err)
	assert.Len(t, loaded.Summaries, 2)
	assert.Equal(t, "First summary of the conversation.", loaded.Summaries[0].Content)
	assert.Equal(t, "Second summary of the conversation.", loaded.Summaries[1].Content)

	// Not found
	err = store.SaveSummary(ctx, "nonexistent", summary1)
	assert.ErrorIs(t, err, ErrNotFound)

	// Invalid ID
	err = store.SaveSummary(ctx, "", summary1)
	assert.ErrorIs(t, err, ErrInvalidID)
}
