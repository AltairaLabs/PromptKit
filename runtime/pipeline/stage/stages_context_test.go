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

// =============================================================================
// Default Message Limit and Large Conversation Warning Tests
// =============================================================================

func TestContextAssemblyStage_DefaultMaxMessages(t *testing.T) {
	// Verify that DefaultMaxMessages and DefaultWarningThreshold are set
	assert.Equal(t, 200, DefaultMaxMessages)
	assert.Equal(t, 100, DefaultWarningThreshold)
}

func TestContextAssemblyStage_DefaultConfigValues(t *testing.T) {
	config := &ContextAssemblyConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store:          statestore.NewMemoryStore(),
			ConversationID: "test",
		},
	}
	s := NewContextAssemblyStage(config)
	assert.Equal(t, DefaultMaxMessages, s.config.MaxMessages)
	assert.Equal(t, DefaultWarningThreshold, s.config.WarningThreshold)
}

func TestContextAssemblyStage_CustomConfigValues(t *testing.T) {
	config := &ContextAssemblyConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store:          statestore.NewMemoryStore(),
			ConversationID: "test",
		},
		MaxMessages:      50,
		WarningThreshold: 25,
	}
	s := NewContextAssemblyStage(config)
	assert.Equal(t, 50, s.config.MaxMessages)
	assert.Equal(t, 25, s.config.WarningThreshold)
}

func TestContextAssemblyStage_TruncatesWhenExceedsDefaultLimit(t *testing.T) {
	store := newMinimalStore()
	ctx := context.Background()

	// Create 80 messages with MaxMessages=50 to test truncation
	// (keeps output under 100-element channel buffer in runTestStage)
	msgs := make([]types.Message, 80)
	for i := 0; i < 80; i++ {
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
		ID:       "conv-truncate",
		Messages: msgs,
	})
	require.NoError(t, err)

	config := &ContextAssemblyConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store:          store,
			ConversationID: "conv-truncate",
		},
		MaxMessages:      50,
		HasContextWindow: false, // No explicit context window
		HasTokenBudget:   false,
	}

	s := NewContextAssemblyStage(config)

	inputs := []StreamElement{
		newTestMsgElement("user", "Current message"),
	}

	results := runTestStage(t, s, inputs)

	// Should have 50 history messages + 1 current = 51
	require.Len(t, results, 51)

	// The first history message should be Message 30 (80 - 50 = 30 truncated)
	assert.Equal(t, "Message 30", results[0].Message.Content)

	// The last history message should be Message 79
	assert.Equal(t, "Message 79", results[49].Message.Content)

	// Current message
	assert.Equal(t, "Current message", results[50].Message.Content)
}

func TestContextAssemblyStage_PreservesSystemMessagesOnTruncation(t *testing.T) {
	store := newMinimalStore()
	ctx := context.Background()

	// Create messages: 2 system + 80 conversation with MaxMessages=50
	msgs := make([]types.Message, 0, 82)
	msgs = append(msgs, types.Message{Role: "system", Content: "System prompt 1"})
	msgs = append(msgs, types.Message{Role: "system", Content: "System prompt 2"})
	for i := 0; i < 80; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs = append(msgs, types.Message{
			Role:    role,
			Content: fmt.Sprintf("Message %d", i),
		})
	}

	err := store.Save(ctx, &statestore.ConversationState{
		ID:       "conv-sys-trunc",
		Messages: msgs,
	})
	require.NoError(t, err)

	config := &ContextAssemblyConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store:          store,
			ConversationID: "conv-sys-trunc",
		},
		MaxMessages:      50,
		HasContextWindow: false,
		HasTokenBudget:   false,
	}

	s := NewContextAssemblyStage(config)

	inputs := []StreamElement{
		newTestMsgElement("user", "Current message"),
	}

	results := runTestStage(t, s, inputs)

	// Should have 50 history messages (2 system + 48 conversation) + 1 current = 51
	require.Len(t, results, 51)

	// First two should be system messages
	assert.Equal(t, "system", results[0].Message.Role)
	assert.Equal(t, "System prompt 1", results[0].Message.Content)
	assert.Equal(t, "system", results[1].Message.Role)
	assert.Equal(t, "System prompt 2", results[1].Message.Content)

	// Next should be conversation messages starting from Message 32
	// (80 - 48 = 32 conversation messages truncated)
	assert.Equal(t, "Message 32", results[2].Message.Content)

	// Last history message should be Message 79
	assert.Equal(t, "Message 79", results[49].Message.Content)
}

func TestContextAssemblyStage_NoTruncationWhenWithinLimit(t *testing.T) {
	store := newMinimalStore()
	ctx := context.Background()

	// Create 50 messages (well within DefaultMaxMessages)
	msgs := make([]types.Message, 50)
	for i := 0; i < 50; i++ {
		msgs[i] = types.Message{
			Role:    "user",
			Content: fmt.Sprintf("Message %d", i),
		}
	}

	err := store.Save(ctx, &statestore.ConversationState{
		ID:       "conv-no-trunc",
		Messages: msgs,
	})
	require.NoError(t, err)

	config := &ContextAssemblyConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store:          store,
			ConversationID: "conv-no-trunc",
		},
		HasContextWindow: false,
	}

	s := NewContextAssemblyStage(config)

	inputs := []StreamElement{
		newTestMsgElement("user", "Current message"),
	}

	results := runTestStage(t, s, inputs)

	// Should have all 50 history messages + 1 current = 51
	require.Len(t, results, 51)
	assert.Equal(t, "Message 0", results[0].Message.Content)
}

func TestContextAssemblyStage_NoTruncationWhenContextWindowSet(t *testing.T) {
	store := newMinimalStore()
	ctx := context.Background()

	// Create 90 messages with MaxMessages=50 but HasContextWindow=true
	msgs := make([]types.Message, 90)
	for i := 0; i < 90; i++ {
		msgs[i] = types.Message{
			Role:    "user",
			Content: fmt.Sprintf("Message %d", i),
		}
	}

	err := store.Save(ctx, &statestore.ConversationState{
		ID:       "conv-cw-set",
		Messages: msgs,
	})
	require.NoError(t, err)

	config := &ContextAssemblyConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store:          store,
			ConversationID: "conv-cw-set",
		},
		MaxMessages:      50,
		HasContextWindow: true, // Explicit context window set — skip truncation
	}

	s := NewContextAssemblyStage(config)

	inputs := []StreamElement{
		newTestMsgElement("user", "Current message"),
	}

	results := runTestStage(t, s, inputs)

	// Should have all 90 history messages + 1 current = 91 (no truncation)
	require.Len(t, results, 91)
}

func TestContextAssemblyStage_CustomMaxMessages(t *testing.T) {
	store := newMinimalStore()
	ctx := context.Background()

	// Create 100 messages
	msgs := make([]types.Message, 100)
	for i := 0; i < 100; i++ {
		msgs[i] = types.Message{
			Role:    "user",
			Content: fmt.Sprintf("Message %d", i),
		}
	}

	err := store.Save(ctx, &statestore.ConversationState{
		ID:       "conv-custom-max",
		Messages: msgs,
	})
	require.NoError(t, err)

	config := &ContextAssemblyConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store:          store,
			ConversationID: "conv-custom-max",
		},
		MaxMessages:      50,
		HasContextWindow: false,
	}

	s := NewContextAssemblyStage(config)

	inputs := []StreamElement{
		newTestMsgElement("user", "Current message"),
	}

	results := runTestStage(t, s, inputs)

	// Should have 50 history messages + 1 current = 51
	require.Len(t, results, 51)

	// First history message should be Message 50 (100 - 50 = 50 truncated)
	assert.Equal(t, "Message 50", results[0].Message.Content)
}

func TestContextAssemblyStage_WarnLargeConversation_NilConfig(t *testing.T) {
	// Ensure warnLargeConversation doesn't panic with nil config
	s := NewContextAssemblyStage(nil)
	// This should not panic
	s.warnLargeConversation(make([]types.Message, 200))
}

func TestContextAssemblyStage_ApplyDefaultMessageLimit_NilConfig(t *testing.T) {
	// Ensure applyDefaultMessageLimit returns messages unchanged with nil config
	s := NewContextAssemblyStage(nil)
	msgs := make([]types.Message, 5)
	result := s.applyDefaultMessageLimit(msgs)
	assert.Len(t, result, 5)
}

func TestContextAssemblyStage_ApplyDefaultMessageLimit_HasContextWindow(t *testing.T) {
	config := &ContextAssemblyConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store:          statestore.NewMemoryStore(),
			ConversationID: "test",
		},
		HasContextWindow: true,
	}
	s := NewContextAssemblyStage(config)
	msgs := make([]types.Message, 300)
	result := s.applyDefaultMessageLimit(msgs)
	// Should not truncate when context window is set
	assert.Len(t, result, 300)
}

func TestContextAssemblyStage_WarnSuppressedWithTokenBudget(t *testing.T) {
	// When HasTokenBudget is true, warning should be suppressed.
	// We can't directly test logger output, but we verify the code path doesn't panic
	// and the messages pass through correctly.
	store := newMinimalStore()
	ctx := context.Background()

	msgs := make([]types.Message, 90)
	for i := 0; i < 90; i++ {
		msgs[i] = types.Message{Role: "user", Content: fmt.Sprintf("Msg %d", i)}
	}

	err := store.Save(ctx, &statestore.ConversationState{
		ID:       "conv-budget",
		Messages: msgs,
	})
	require.NoError(t, err)

	config := &ContextAssemblyConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store:          store,
			ConversationID: "conv-budget",
		},
		HasTokenBudget:   true,
		HasContextWindow: false,
	}

	s := NewContextAssemblyStage(config)

	inputs := []StreamElement{
		newTestMsgElement("user", "Current"),
	}

	results := runTestStage(t, s, inputs)
	// 90 history + 1 current = 91 (no truncation since within default 200 limit)
	require.Len(t, results, 91)
}

func TestContextAssemblyStage_WarningFires_AboveThreshold(t *testing.T) {
	// Verify warning fires when messages exceed threshold and
	// no token budget/context window. Use low threshold and MaxMessages
	// to keep output within channel buffer.
	store := newMinimalStore()
	ctx := context.Background()

	msgs := make([]types.Message, 20)
	for i := 0; i < 20; i++ {
		msgs[i] = types.Message{Role: "user", Content: fmt.Sprintf("Msg %d", i)}
	}

	err := store.Save(ctx, &statestore.ConversationState{
		ID:       "conv-warn",
		Messages: msgs,
	})
	require.NoError(t, err)

	config := &ContextAssemblyConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store:          store,
			ConversationID: "conv-warn",
		},
		WarningThreshold: 10,  // Low threshold — 20 msgs will trigger warning
		MaxMessages:      50,  // High enough to not truncate
		HasContextWindow: false,
		HasTokenBudget:   false,
	}

	s := NewContextAssemblyStage(config)

	inputs := []StreamElement{
		newTestMsgElement("user", "Current"),
	}

	// No panic, no truncation — just verifies code path runs
	results := runTestStage(t, s, inputs)
	require.Len(t, results, 21) // 20 history + 1 current
}

func TestContextAssemblyStage_TruncateAllSystemMessages(t *testing.T) {
	// Edge case: when system messages alone fill the limit
	config := &ContextAssemblyConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{
			Store:          statestore.NewMemoryStore(),
			ConversationID: "test",
		},
		MaxMessages:      2,
		HasContextWindow: false,
	}
	s := NewContextAssemblyStage(config)

	// 3 system messages + 5 conversation messages
	msgs := []types.Message{
		{Role: "system", Content: "sys1"},
		{Role: "system", Content: "sys2"},
		{Role: "system", Content: "sys3"},
		{Role: "user", Content: "user1"},
		{Role: "assistant", Content: "asst1"},
		{Role: "user", Content: "user2"},
		{Role: "assistant", Content: "asst2"},
		{Role: "user", Content: "user3"},
	}

	result := s.applyDefaultMessageLimit(msgs)

	// MaxMessages=2, 3 system messages -> maxConversation = -1 -> returns only system
	assert.Len(t, result, 3)
	assert.Equal(t, "sys1", result[0].Content)
	assert.Equal(t, "sys2", result[1].Content)
	assert.Equal(t, "sys3", result[2].Content)
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
