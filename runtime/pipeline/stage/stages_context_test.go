package stage

import (
	"context"
	"fmt"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// ContextAssemblyStage Tests
// =============================================================================

func TestContextAssemblyStage_WithMessageReader(t *testing.T) {
	store := statestore.NewMemoryStore()
	ctx := context.Background()

	// Pre-populate 30 messages
	msgs := make([]types.Message, 30)
	for i := 0; i < 30; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs[i] = types.Message{
			Role:    role,
			Content: fmt.Sprintf("Message %d", i),
		}
	}
	err := store.Save(ctx, &statestore.ConversationState{
		ID:       "conv-123",
		Messages: msgs,
	})
	require.NoError(t, err)

	config := &ContextAssemblyConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store:          store,
			ConversationID: "conv-123",
		},
		RecentMessages: 20,
	}

	s := NewContextAssemblyStage(config)

	inputs := []StreamElement{
		newTestMsgElement("user", "Current message"),
	}

	results := runTestStage(t, s, inputs)

	// Should have 20 history messages + 1 current = 21
	require.Len(t, results, 21)

	// First 20 should be from history (messages 10-29)
	for i := 0; i < 20; i++ {
		assert.Equal(t, fmt.Sprintf("Message %d", i+10), results[i].Message.Content)
		assert.Equal(t, "statestore", results[i].Message.Source)
		assert.True(t, results[i].Metadata["from_history"].(bool))
	}

	// Last should be current message
	assert.Equal(t, "Current message", results[20].Message.Content)
}

func TestContextAssemblyStage_WithSummaries(t *testing.T) {
	store := statestore.NewMemoryStore()
	ctx := context.Background()

	// Pre-populate with messages
	err := store.Save(ctx, &statestore.ConversationState{
		ID: "conv-123",
		Messages: []types.Message{
			{Role: "user", Content: "Recent 1"},
			{Role: "assistant", Content: "Recent 2"},
		},
	})
	require.NoError(t, err)

	// Add summaries
	err = store.SaveSummary(ctx, "conv-123", statestore.Summary{
		StartTurn: 0,
		EndTurn:   10,
		Content:   "Summary of early conversation",
	})
	require.NoError(t, err)

	err = store.SaveSummary(ctx, "conv-123", statestore.Summary{
		StartTurn: 10,
		EndTurn:   20,
		Content:   "Summary of middle conversation",
	})
	require.NoError(t, err)

	config := &ContextAssemblyConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store:          store,
			ConversationID: "conv-123",
		},
		RecentMessages: 20,
	}

	s := NewContextAssemblyStage(config)

	inputs := []StreamElement{
		newTestMsgElement("user", "Current message"),
	}

	results := runTestStage(t, s, inputs)

	// Should have 2 summaries + 2 history messages + 1 current = 5
	require.Len(t, results, 5)

	// First two should be summary system messages
	assert.Equal(t, "system", results[0].Message.Role)
	assert.Contains(t, results[0].Message.Content, "Summary of early conversation")
	assert.Contains(t, results[0].Message.Content, "turns 0-10")
	assert.Equal(t, "statestore", results[0].Message.Source)

	assert.Equal(t, "system", results[1].Message.Role)
	assert.Contains(t, results[1].Message.Content, "Summary of middle conversation")
	assert.Contains(t, results[1].Message.Content, "turns 10-20")

	// Next two should be recent messages from history
	assert.Equal(t, "Recent 1", results[2].Message.Content)
	assert.Equal(t, "Recent 2", results[3].Message.Content)

	// Last should be current message
	assert.Equal(t, "Current message", results[4].Message.Content)
}

// minimalStore implements only statestore.Store (Load/Save/Fork), not MessageReader.
type minimalStore struct {
	states map[string]*statestore.ConversationState
}

func newMinimalStore() *minimalStore {
	return &minimalStore{
		states: make(map[string]*statestore.ConversationState),
	}
}

func (s *minimalStore) Load(_ context.Context, id string) (*statestore.ConversationState, error) {
	state, exists := s.states[id]
	if !exists {
		return nil, statestore.ErrNotFound
	}
	return state, nil
}

func (s *minimalStore) Save(_ context.Context, state *statestore.ConversationState) error {
	s.states[state.ID] = state
	return nil
}

func (s *minimalStore) Fork(_ context.Context, sourceID, newID string) error {
	state, exists := s.states[sourceID]
	if !exists {
		return statestore.ErrNotFound
	}
	copied := *state
	copied.ID = newID
	s.states[newID] = &copied
	return nil
}

func TestContextAssemblyStage_FallbackToFullLoad(t *testing.T) {
	store := newMinimalStore()
	ctx := context.Background()

	// Pre-populate with messages
	err := store.Save(ctx, &statestore.ConversationState{
		ID: "conv-123",
		Messages: []types.Message{
			{Role: "user", Content: "History 1"},
			{Role: "assistant", Content: "History 2"},
			{Role: "user", Content: "History 3"},
		},
	})
	require.NoError(t, err)

	config := &ContextAssemblyConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store:          store,
			ConversationID: "conv-123",
		},
		RecentMessages: 2, // This is ignored in fallback path — all messages loaded
	}

	s := NewContextAssemblyStage(config)

	inputs := []StreamElement{
		newTestMsgElement("user", "Current message"),
	}

	results := runTestStage(t, s, inputs)

	// Fallback loads all messages: 3 history + 1 current = 4
	require.Len(t, results, 4)

	// All three history messages should be present
	assert.Equal(t, "History 1", results[0].Message.Content)
	assert.Equal(t, "statestore", results[0].Message.Source)
	assert.Equal(t, "History 2", results[1].Message.Content)
	assert.Equal(t, "History 3", results[2].Message.Content)

	// Current message
	assert.Equal(t, "Current message", results[3].Message.Content)
}

func TestContextAssemblyStage_EmptyConversation(t *testing.T) {
	store := statestore.NewMemoryStore()

	config := &ContextAssemblyConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store:          store,
			ConversationID: "nonexistent",
		},
		RecentMessages: 20,
	}

	s := NewContextAssemblyStage(config)

	inputs := []StreamElement{
		newTestMsgElement("user", "Current message"),
	}

	results := runTestStage(t, s, inputs)

	// Only the current message should be present (no history)
	require.Len(t, results, 1)
	assert.Equal(t, "Current message", results[0].Message.Content)
}

func TestContextAssemblyStage_NilConfig(t *testing.T) {
	s := NewContextAssemblyStage(nil)

	inputs := []StreamElement{
		newTestMsgElement("user", "Current message"),
	}

	results := runTestStage(t, s, inputs)

	// Should pass through without modification
	require.Len(t, results, 1)
	assert.Equal(t, "Current message", results[0].Message.Content)
}

// =============================================================================
// mockMessageIndex for semantic retrieval tests
// =============================================================================

type mockMessageIndex struct {
	results []statestore.IndexResult
	err     error
}

func (m *mockMessageIndex) Index(_ context.Context, _ string, _ int, _ types.Message) error {
	return nil
}

func (m *mockMessageIndex) Search(_ context.Context, _, _ string, _ int) ([]statestore.IndexResult, error) {
	return m.results, m.err
}

func (m *mockMessageIndex) Delete(_ context.Context, _ string) error {
	return nil
}

// =============================================================================
// Additional ContextAssemblyStage Tests for Coverage
// =============================================================================

func TestContextAssemblyStage_WithSemanticRetrieval(t *testing.T) {
	store := statestore.NewMemoryStore()
	ctx := context.Background()

	// Pre-populate with messages including a user message as the last one
	err := store.Save(ctx, &statestore.ConversationState{
		ID: "conv-sr",
		Messages: []types.Message{
			{Role: "user", Content: "Tell me about dogs"},
			{Role: "assistant", Content: "Dogs are great pets."},
			{Role: "user", Content: "What about cats?"},
			{Role: "assistant", Content: "Cats are independent."},
		},
	})
	require.NoError(t, err)

	// Add a summary so we can verify ordering: summaries -> retrieved -> hot window
	err = store.SaveSummary(ctx, "conv-sr", statestore.Summary{
		StartTurn: 0,
		EndTurn:   5,
		Content:   "Early discussion about pets",
	})
	require.NoError(t, err)

	// Configure mock index that returns a semantically retrieved message
	idx := &mockMessageIndex{
		results: []statestore.IndexResult{
			{
				TurnIndex: 42, // some older turn not in hot window
				Message: types.Message{
					Role:    "user",
					Content: "I had a cat named Whiskers",
				},
				Score: 0.85,
			},
		},
	}

	config := &ContextAssemblyConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store:          store,
			ConversationID: "conv-sr",
		},
		RecentMessages: 20,
		MessageIndex:   idx,
		RetrievalTopK:  3,
	}

	s := NewContextAssemblyStage(config)

	inputs := []StreamElement{
		newTestMsgElement("user", "Current question"),
	}

	results := runTestStage(t, s, inputs)

	// Expected order: 1 summary + 1 retrieved + 4 hot window + 1 current = 7
	require.Len(t, results, 7)

	// First: summary
	assert.Equal(t, "system", results[0].Message.Role)
	assert.Contains(t, results[0].Message.Content, "Early discussion about pets")

	// Second: retrieved message with source="retrieved"
	assert.Equal(t, "I had a cat named Whiskers", results[1].Message.Content)
	// Note: emitHistory overwrites Source to "statestore", but the retrieved message
	// gets Source="retrieved" set in retrieveRelevant before emitHistory runs
	assert.Equal(t, "statestore", results[1].Message.Source) // emitHistory sets this

	// Messages 2-5: hot window
	assert.Equal(t, "Tell me about dogs", results[2].Message.Content)
	assert.Equal(t, "Dogs are great pets.", results[3].Message.Content)
	assert.Equal(t, "What about cats?", results[4].Message.Content)
	assert.Equal(t, "Cats are independent.", results[5].Message.Content)

	// Last: current input
	assert.Equal(t, "Current question", results[6].Message.Content)
}

func TestContextAssemblyStage_SemanticRetrieval_SearchError(t *testing.T) {
	store := statestore.NewMemoryStore()
	ctx := context.Background()

	// Need at least one user message in recent for retrieval to attempt search
	err := store.Save(ctx, &statestore.ConversationState{
		ID: "conv-err",
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
	})
	require.NoError(t, err)

	// Mock index returns an error
	idx := &mockMessageIndex{
		err: fmt.Errorf("embedding service unavailable"),
	}

	config := &ContextAssemblyConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store:          store,
			ConversationID: "conv-err",
		},
		RecentMessages: 20,
		MessageIndex:   idx,
		RetrievalTopK:  3,
	}

	s := NewContextAssemblyStage(config)

	inputs := []StreamElement{
		newTestMsgElement("user", "Current"),
	}

	// Should succeed despite search error (non-fatal)
	results := runTestStage(t, s, inputs)

	// 1 hot window message + 1 current = 2
	require.Len(t, results, 2)
	assert.Equal(t, "Hello", results[0].Message.Content)
	assert.Equal(t, "Current", results[1].Message.Content)
}

func TestContextAssemblyStage_SemanticRetrieval_NoUserMessage(t *testing.T) {
	store := statestore.NewMemoryStore()
	ctx := context.Background()

	// Only assistant messages — no user message to build query from
	err := store.Save(ctx, &statestore.ConversationState{
		ID: "conv-nouser",
		Messages: []types.Message{
			{Role: "assistant", Content: "Welcome!"},
		},
	})
	require.NoError(t, err)

	idx := &mockMessageIndex{
		results: []statestore.IndexResult{
			{TurnIndex: 1, Message: types.Message{Role: "user", Content: "Old"}, Score: 0.9},
		},
	}

	config := &ContextAssemblyConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store:          store,
			ConversationID: "conv-nouser",
		},
		RecentMessages: 20,
		MessageIndex:   idx,
		RetrievalTopK:  3,
	}

	s := NewContextAssemblyStage(config)

	inputs := []StreamElement{
		newTestMsgElement("user", "Current"),
	}

	results := runTestStage(t, s, inputs)

	// No retrieved messages since no user message in recent history for query
	// 1 hot window + 1 current = 2
	require.Len(t, results, 2)
	assert.Equal(t, "Welcome!", results[0].Message.Content)
	assert.Equal(t, "Current", results[1].Message.Content)
}

func TestContextAssemblyStage_WithUserID(t *testing.T) {
	store := statestore.NewMemoryStore()
	ctx := context.Background()

	err := store.Save(ctx, &statestore.ConversationState{
		ID: "conv-uid",
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
	})
	require.NoError(t, err)

	config := &ContextAssemblyConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store:          store,
			ConversationID: "conv-uid",
			UserID:         "user-42",
		},
		RecentMessages: 20,
	}

	s := NewContextAssemblyStage(config)

	inputs := []StreamElement{
		newTestMsgElement("user", "Current message"),
	}

	results := runTestStage(t, s, inputs)

	// 1 history + 1 current = 2
	require.Len(t, results, 2)

	// The current message (forwarded via forwardWithMetadata) should have user_id metadata
	currentMsg := results[1]
	assert.Equal(t, "Current message", currentMsg.Message.Content)
	assert.Equal(t, "conv-uid", currentMsg.Metadata["conversation_id"])
	assert.Equal(t, "user-42", currentMsg.Metadata["user_id"])
}

func TestContextAssemblyStage_LoadFullHistory_NotFound(t *testing.T) {
	store := newMinimalStore()

	// Don't save anything — Load will return ErrNotFound

	config := &ContextAssemblyConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store:          store,
			ConversationID: "nonexistent",
		},
		RecentMessages: 20,
	}

	s := NewContextAssemblyStage(config)

	inputs := []StreamElement{
		newTestMsgElement("user", "Current message"),
	}

	results := runTestStage(t, s, inputs)

	// ErrNotFound is handled gracefully — only the current message comes through
	require.Len(t, results, 1)
	assert.Equal(t, "Current message", results[0].Message.Content)
}

func TestContextAssemblyStage_LoadFullHistory_NilState(t *testing.T) {
	// Custom minimal store that returns nil state without error
	store := &nilStateStore{}

	config := &ContextAssemblyConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store:          store,
			ConversationID: "conv-nil",
		},
		RecentMessages: 20,
	}

	s := NewContextAssemblyStage(config)

	inputs := []StreamElement{
		newTestMsgElement("user", "Current message"),
	}

	results := runTestStage(t, s, inputs)

	// nil state is handled gracefully — only the current message comes through
	require.Len(t, results, 1)
	assert.Equal(t, "Current message", results[0].Message.Content)
}

// nilStateStore returns nil state and nil error from Load (covers nil state path).
type nilStateStore struct{}

func (s *nilStateStore) Load(_ context.Context, _ string) (*statestore.ConversationState, error) {
	return nil, nil
}

func (s *nilStateStore) Save(_ context.Context, _ *statestore.ConversationState) error {
	return nil
}

func (s *nilStateStore) Fork(_ context.Context, _, _ string) error {
	return nil
}

func TestContextAssemblyStage_ForwardAll_ContextCancelled(t *testing.T) {
	// Test forwardAll with a cancelled context
	ctx, cancel := context.WithCancel(context.Background())

	s := NewContextAssemblyStage(nil) // nil config triggers forwardAll path

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Fill the output channel so the next send blocks
	output <- NewMessageElement(&types.Message{Role: "user", Content: "blocker"})

	// Send an input element
	input <- newTestMsgElement("user", "test")
	close(input)

	// Cancel context so the select in forwardAll takes ctx.Done path
	cancel()

	err := s.Process(ctx, input, output)
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}
