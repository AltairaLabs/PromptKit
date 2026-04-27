package stages

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	runtimeStatestore "github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper to create a test element with a message
func newTestMessageElement(role, content string) stage.StreamElement {
	msg := &types.Message{
		Role:    role,
		Content: content,
	}
	return stage.NewMessageElement(msg)
}

// Helper to run a stage and collect output
func runStage(t *testing.T, s stage.Stage, inputs []stage.StreamElement) []stage.StreamElement {
	t.Helper()

	input := make(chan stage.StreamElement, len(inputs))
	for _, elem := range inputs {
		input <- elem
	}
	close(input)

	output := make(chan stage.StreamElement, 100)
	ctx := context.Background()

	err := s.Process(ctx, input, output)
	require.NoError(t, err)

	var results []stage.StreamElement
	for elem := range output {
		results = append(results, elem)
	}
	return results
}

// =============================================================================
// HistoryInjectionStage Tests
// =============================================================================

func TestHistoryInjectionStage_EmitsHistoryFirst(t *testing.T) {
	history := []types.Message{
		{Role: "user", Content: "History message 1"},
		{Role: "assistant", Content: "History response 1"},
	}

	s := NewHistoryInjectionStage(history)

	// Input element (current message)
	inputs := []stage.StreamElement{
		newTestMessageElement("user", "Current message"),
	}

	results := runStage(t, s, inputs)

	// Should have history (2) + current (1) = 3 elements
	require.Len(t, results, 3)

	// First two should be history
	assert.Equal(t, "History message 1", results[0].Message.Content)
	assert.Equal(t, "History response 1", results[1].Message.Content)

	// Last should be current
	assert.Equal(t, "Current message", results[2].Message.Content)
}

func TestHistoryInjectionStage_EmptyHistory(t *testing.T) {
	s := NewHistoryInjectionStage(nil)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "Message"),
	}

	results := runStage(t, s, inputs)

	// Should only have the input element
	require.Len(t, results, 1)
	assert.Equal(t, "Message", results[0].Message.Content)
}

func TestHistoryInjectionStage_MultipleElements(t *testing.T) {
	history := []types.Message{
		{Role: "user", Content: "History"},
	}

	s := NewHistoryInjectionStage(history)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "Current 1"),
		newTestMessageElement("user", "Current 2"),
	}

	results := runStage(t, s, inputs)

	// Should have history (1) + current (2) = 3 elements
	require.Len(t, results, 3)
	assert.Equal(t, "History", results[0].Message.Content)
	assert.Equal(t, "Current 1", results[1].Message.Content)
	assert.Equal(t, "Current 2", results[2].Message.Content)
}

// =============================================================================
// TurnIndexStage Tests
// =============================================================================

func TestTurnIndexStage_CountsTurns(t *testing.T) {
	turnState := stage.NewTurnState()
	s := NewTurnIndexStageWithTurnState(turnState)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "User 1"),
		newTestMessageElement("assistant", "Assistant 1"),
		newTestMessageElement("user", "User 2"),
		newTestMessageElement("assistant", "Assistant 2"),
	}

	results := runStage(t, s, inputs)

	require.Len(t, results, 4)

	// Counters land on TurnState.ProviderRequestMetadata.
	m := turnState.ProviderRequestMetadata
	assert.Equal(t, 2, m["arena_user_completed_turns"])
	assert.Equal(t, 3, m["arena_user_next_turn"])
	assert.Equal(t, 2, m["arena_assistant_completed_turns"])
	assert.Equal(t, 3, m["arena_assistant_next_turn"])
}

func TestTurnIndexStage_EmptyInput(t *testing.T) {
	s := NewTurnIndexStage()

	results := runStage(t, s, nil)

	require.Len(t, results, 0)
}

func TestTurnIndexStage_OnlyUserMessages(t *testing.T) {
	turnState := stage.NewTurnState()
	s := NewTurnIndexStageWithTurnState(turnState)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "User 1"),
		newTestMessageElement("user", "User 2"),
	}

	results := runStage(t, s, inputs)

	require.Len(t, results, 2)
	assert.Equal(t, 2, turnState.ProviderRequestMetadata["arena_user_completed_turns"])
	assert.Equal(t, 0, turnState.ProviderRequestMetadata["arena_assistant_completed_turns"])
}

func TestTurnIndexStage_DoesNotOverwriteExisting(t *testing.T) {
	turnState := stage.NewTurnState()
	turnState.ProviderRequestMetadata = map[string]interface{}{
		"arena_user_completed_turns": 100, // Pre-existing value
	}
	s := NewTurnIndexStageWithTurnState(turnState)

	results := runStage(t, s, []stage.StreamElement{newTestMessageElement("user", "Test")})

	require.Len(t, results, 1)
	// Should NOT overwrite the existing value
	assert.Equal(t, 100, turnState.ProviderRequestMetadata["arena_user_completed_turns"])
}

// =============================================================================
// StripToolMessagesStage Tests
// =============================================================================

func TestStripToolMessagesStage_RemovesToolMessages(t *testing.T) {
	s := NewStripToolMessagesStage()

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "User message"),
		newTestMessageElement("assistant", "Assistant message"),
		newTestMessageElement("tool", "Tool result"),
		newTestMessageElement("user", "Another user message"),
	}

	results := runStage(t, s, inputs)

	// Should have 3 elements (tool message stripped)
	require.Len(t, results, 3)
	assert.Equal(t, "user", results[0].Message.Role)
	assert.Equal(t, "assistant", results[1].Message.Role)
	assert.Equal(t, "user", results[2].Message.Role)
}

func TestStripToolMessagesStage_CaseInsensitive(t *testing.T) {
	s := NewStripToolMessagesStage()

	inputs := []stage.StreamElement{
		newTestMessageElement("TOOL", "Tool result 1"),
		newTestMessageElement("Tool", "Tool result 2"),
		newTestMessageElement("user", "User message"),
	}

	results := runStage(t, s, inputs)

	require.Len(t, results, 1)
	assert.Equal(t, "user", results[0].Message.Role)
}

func TestStripToolMessagesStage_EmptyInput(t *testing.T) {
	s := NewStripToolMessagesStage()

	results := runStage(t, s, nil)

	require.Len(t, results, 0)
}

// =============================================================================
// MockScenarioContextStage Tests
// =============================================================================

func TestMockScenarioContextStage_AddsContext(t *testing.T) {
	scenario := &config.Scenario{
		ID: "test-scenario",
	}

	turnState := stage.NewTurnState()
	s := NewMockScenarioContextStageWithTurnState(scenario, turnState)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "Test"),
	}

	results := runStage(t, s, inputs)

	require.Len(t, results, 1)
	assert.Equal(t, "test-scenario", turnState.ProviderRequestMetadata["mock_scenario_id"])
	assert.NotNil(t, turnState.ProviderRequestMetadata["mock_turn_number"])
}

func TestMockScenarioContextStage_NilScenario(t *testing.T) {
	turnState := stage.NewTurnState()
	s := NewMockScenarioContextStageWithTurnState(nil, turnState)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "Test"),
	}

	results := runStage(t, s, inputs)

	require.Len(t, results, 1)
	// Should forward without writing scenario metadata.
	assert.Nil(t, turnState.ProviderRequestMetadata["mock_scenario_id"])
}

func TestMockScenarioContextStage_EmptyScenarioID(t *testing.T) {
	scenario := &config.Scenario{
		ID: "", // Empty ID
	}

	turnState := stage.NewTurnState()
	s := NewMockScenarioContextStageWithTurnState(scenario, turnState)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "Test"),
	}

	results := runStage(t, s, inputs)

	require.Len(t, results, 1)
	// Should forward without writing scenario metadata when ID is empty.
	assert.Nil(t, turnState.ProviderRequestMetadata["mock_scenario_id"])
}

func TestMockScenarioContextStage_TurnNumberFromTurnState(t *testing.T) {
	scenario := &config.Scenario{
		ID: "test-scenario",
	}

	turnState := stage.NewTurnState()
	turnState.ProviderRequestMetadata = map[string]interface{}{
		"arena_user_completed_turns": 5,
	}
	s := NewMockScenarioContextStageWithTurnState(scenario, turnState)

	elem := newTestMessageElement("user", "Test")

	results := runStage(t, s, []stage.StreamElement{elem})

	require.Len(t, results, 1)
	// Should use the turn number from TurnState's existing metadata.
	assert.Equal(t, 5, turnState.ProviderRequestMetadata["mock_turn_number"])
}

func TestMockScenarioContextStage_TurnNumberFromAssistantCount(t *testing.T) {
	scenario := &config.Scenario{
		ID: "test-scenario",
	}

	turnState := stage.NewTurnState()
	s := NewMockScenarioContextStageWithTurnState(scenario, turnState)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "User 1"),
		newTestMessageElement("assistant", "Assistant 1"),
		newTestMessageElement("assistant", "Assistant 2"),
		newTestMessageElement("user", "User 2"),
	}

	results := runStage(t, s, inputs)

	require.Len(t, results, 4)
	// Turn number should be assistant count + 1 = 3
	assert.Equal(t, 3, turnState.ProviderRequestMetadata["mock_turn_number"])
}

func TestMockScenarioContextStage_TurnNumberFromUserCount(t *testing.T) {
	scenario := &config.Scenario{
		ID: "test-scenario",
	}

	turnState := stage.NewTurnState()
	s := NewMockScenarioContextStageWithTurnState(scenario, turnState)

	// Only user messages, no assistant messages
	inputs := []stage.StreamElement{
		newTestMessageElement("user", "User 1"),
		newTestMessageElement("user", "User 2"),
	}

	results := runStage(t, s, inputs)

	require.Len(t, results, 2)
	// Turn number should be user count = 2
	assert.Equal(t, 2, turnState.ProviderRequestMetadata["mock_turn_number"])
}

// =============================================================================
// SelfPlayUserTurnContextStage Tests
// =============================================================================

func TestSelfPlayUserTurnContextStage_AddsContext(t *testing.T) {
	scenario := &config.Scenario{
		ID: "selfplay-scenario",
	}

	turnState := stage.NewTurnState()
	s := NewSelfPlayUserTurnContextStageWithTurnState(scenario, "", turnState)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "User 1"),
		newTestMessageElement("assistant", "Assistant 1"),
	}

	results := runStage(t, s, inputs)

	require.Len(t, results, 2)

	// Coordination data lives in TurnState's provider request metadata.
	m := turnState.ProviderRequestMetadata
	assert.Equal(t, "selfplay-scenario", m["mock_scenario_id"])
	assert.Equal(t, 1, m["arena_user_completed_turns"])
	assert.Equal(t, 2, m["arena_user_next_turn"])
	assert.Equal(t, "self_play_user", m["arena_role"])
}

func TestSelfPlayUserTurnContextStage_NilScenario(t *testing.T) {
	turnState := stage.NewTurnState()
	s := NewSelfPlayUserTurnContextStageWithTurnState(nil, "", turnState)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "Test"),
	}

	results := runStage(t, s, inputs)

	require.Len(t, results, 1)
	// Should not write scenario-specific metadata.
	assert.Nil(t, turnState.ProviderRequestMetadata["mock_scenario_id"])
}

func TestSelfPlayUserTurnContextStage_PersonaIDRoutedToProviderMetadata(t *testing.T) {
	scenario := &config.Scenario{ID: "test-scenario"}
	turnState := stage.NewTurnState()
	s := NewSelfPlayUserTurnContextStageWithTurnState(scenario, "curious-customer", turnState)

	results := runStage(t, s, []stage.StreamElement{newTestMessageElement("user", "hi")})
	require.Len(t, results, 1)
	assert.Equal(t, "curious-customer", turnState.ProviderRequestMetadata["mock_persona_id"])
}

// =============================================================================
// ScenarioContextExtractionStage Tests
// =============================================================================

func TestScenarioContextExtractionStage_WritesToTurnStateVariables(t *testing.T) {
	scenario := &config.Scenario{
		ID:          "test-scenario",
		Description: "Test scenario description",
		TaskType:    "chat",
		ContextMetadata: &config.ContextMetadata{
			Domain:   "technology",
			UserRole: "developer",
		},
	}

	turnState := stage.NewTurnState()
	s := NewScenarioContextExtractionStageWithTurnState(scenario, turnState)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "Hello"),
	}

	results := runStage(t, s, inputs)
	require.Len(t, results, 1)

	assert.Equal(t, "technology", turnState.Variables["domain"])
	assert.Equal(t, "developer", turnState.Variables["user_context"])
	assert.Equal(t, "developer", turnState.Variables["user_role"])
	assert.Contains(t, turnState.Variables["context_slot"], "Test scenario description")
}

func TestScenarioContextExtractionStage_EmptyScenarioWritesEmptyValues(t *testing.T) {
	turnState := stage.NewTurnState()
	s := NewScenarioContextExtractionStageWithTurnState(&config.Scenario{}, turnState)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "Test"),
	}

	results := runStage(t, s, inputs)
	require.Len(t, results, 1)

	// Should have empty string values for missing context.
	assert.Equal(t, "", turnState.Variables["domain"])
}

func TestScenarioContextExtractionStage_DoesNotOverwriteExisting(t *testing.T) {
	scenario := &config.Scenario{
		ContextMetadata: &config.ContextMetadata{
			Domain: "new_domain",
		},
	}

	turnState := stage.NewTurnState()
	turnState.Variables = map[string]string{
		"domain": "existing_domain", // Pre-existing value
	}
	s := NewScenarioContextExtractionStageWithTurnState(scenario, turnState)

	results := runStage(t, s, []stage.StreamElement{newTestMessageElement("user", "Test")})
	require.Len(t, results, 1)

	// Should NOT overwrite existing domain in TurnState.
	assert.Equal(t, "existing_domain", turnState.Variables["domain"])
}

func TestBuildContextSlot_WithDescription(t *testing.T) {
	scenario := &config.Scenario{
		Description: "A test scenario",
		TaskType:    "chat",
	}

	result := buildContextSlot(scenario, nil)

	assert.Equal(t, "A test scenario", result)
}

func TestBuildContextSlot_WithUserMessage(t *testing.T) {
	scenario := &config.Scenario{
		TaskType: "chat",
	}
	messages := []types.Message{
		{Role: "user", Content: "I want to book a flight"},
	}

	result := buildContextSlot(scenario, messages)

	assert.Contains(t, result, "User wants to: I want to book a flight")
}

func TestBuildContextSlot_TruncatesLongContent(t *testing.T) {
	scenario := &config.Scenario{
		TaskType: "chat",
	}
	longContent := ""
	for i := 0; i < 200; i++ {
		longContent += "x"
	}
	messages := []types.Message{
		{Role: "user", Content: longContent},
	}

	result := buildContextSlot(scenario, messages)

	// Should truncate to 150 chars + "..."
	assert.Contains(t, result, "...")
	assert.LessOrEqual(t, len(result), 200)
}

func TestBuildContextSlot_FallbackToTaskType(t *testing.T) {
	scenario := &config.Scenario{
		TaskType: "support",
	}

	result := buildContextSlot(scenario, nil)

	assert.Equal(t, "support conversation", result)
}

// =============================================================================
// Helper Function Tests
// =============================================================================

func TestDeepCloneMessages(t *testing.T) {
	original := []types.Message{
		{
			Role:    "user",
			Content: "Test",
			Meta: map[string]interface{}{
				"key": "value",
			},
			ToolCalls: []types.MessageToolCall{
				{ID: "call1", Name: "tool1"},
			},
		},
	}

	cloned := deepCloneMessages(original)

	require.Len(t, cloned, 1)
	assert.Equal(t, "user", cloned[0].Role)
	assert.Equal(t, "Test", cloned[0].Content)

	// Modify original should not affect clone
	original[0].Content = "Modified"
	assert.Equal(t, "Test", cloned[0].Content)

	// Meta should be deep copied
	original[0].Meta["key"] = "modified"
	assert.Equal(t, "value", cloned[0].Meta["key"])
}

func TestDeepCloneMessages_Nil(t *testing.T) {
	result := deepCloneMessages(nil)
	assert.Nil(t, result)
}

func TestDeepCloneMessages_WithToolResult(t *testing.T) {
	original := []types.Message{
		{
			Role: "tool",
			ToolResult: func() *types.MessageToolResult {
				r := types.NewTextToolResult("call1", "tool1", "result")
				return &r
			}(),
		},
	}

	cloned := deepCloneMessages(original)

	require.Len(t, cloned, 1)
	require.NotNil(t, cloned[0].ToolResult)
	assert.Equal(t, "call1", cloned[0].ToolResult.ID)

	// Modify original should not affect clone
	original[0].ToolResult.Parts = []types.ContentPart{types.NewTextPart("modified")}
	assert.Equal(t, "result", cloned[0].ToolResult.GetTextContent())
}

func TestDeepCloneMessages_WithCostInfo(t *testing.T) {
	original := []types.Message{
		{
			Role: "assistant",
			CostInfo: &types.CostInfo{
				TotalCost: 0.01,
			},
		},
	}

	cloned := deepCloneMessages(original)

	require.Len(t, cloned, 1)
	require.NotNil(t, cloned[0].CostInfo)
	assert.Equal(t, 0.01, cloned[0].CostInfo.TotalCost)
}

func TestDeepCopyMap(t *testing.T) {
	original := map[string]interface{}{
		"key1": "value1",
		"key2": 42,
	}

	copied := shallowCopyMap(original)

	assert.Equal(t, "value1", copied["key1"])
	assert.Equal(t, 42, copied["key2"])

	// Modify original should not affect copy
	original["key1"] = "modified"
	assert.Equal(t, "value1", copied["key1"])
}

func TestDeepCopyMap_Nil(t *testing.T) {
	result := shallowCopyMap(nil)
	assert.Nil(t, result)
}

// =============================================================================
// ArenaStateStoreSaveStage Tests (basic - no actual store interaction)
// =============================================================================

func TestArenaStateStoreSaveStage_NilConfig(t *testing.T) {
	s := NewArenaStateStoreSaveStage(nil)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "Test"),
	}

	results := runStage(t, s, inputs)

	// Should forward elements when no config
	require.Len(t, results, 1)
	assert.Equal(t, "Test", results[0].Message.Content)
}

func TestArenaStateStoreSaveStage_WithArenaStore(t *testing.T) {
	store := statestore.NewArenaStateStore()

	cfg := &pipeline.StateStoreConfig{
		Store:          store,
		ConversationID: "test-conv-1",
		UserID:         "test-user",
	}

	s := NewArenaStateStoreSaveStage(cfg)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "Hello"),
		newTestMessageElement("assistant", "Hi there!"),
	}

	results := runStage(t, s, inputs)

	// Should forward all elements
	require.Len(t, results, 2)

	// Verify state was saved
	ctx := context.Background()
	state, err := store.Load(ctx, "test-conv-1")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Len(t, state.Messages, 2)
}

func TestArenaStateStoreSaveStage_WithSystemPrompt(t *testing.T) {
	store := statestore.NewArenaStateStore()

	cfg := &pipeline.StateStoreConfig{
		Store:          store,
		ConversationID: "test-conv-2",
	}

	turnState := stage.NewTurnState()
	turnState.SystemPrompt = "You are a helpful assistant"
	s := NewArenaStateStoreSaveStageWithTurnState(cfg, turnState)

	elem := newTestMessageElement("user", "Hello")

	results := runStage(t, s, []stage.StreamElement{elem})

	require.Len(t, results, 1)

	// Verify state includes system prompt as first message
	ctx := context.Background()
	state, err := store.Load(ctx, "test-conv-2")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Len(t, state.Messages, 2) // system + user
	assert.Equal(t, "system", state.Messages[0].Role)
	assert.Equal(t, "You are a helpful assistant", state.Messages[0].Content)
}

func TestArenaStateStoreSaveStage_WithCostInfo(t *testing.T) {
	store := statestore.NewArenaStateStore()

	cfg := &pipeline.StateStoreConfig{
		Store:          store,
		ConversationID: "test-conv-3",
	}

	s := NewArenaStateStoreSaveStage(cfg)

	elem := newTestMessageElement("assistant", "Response")
	elem.Message.CostInfo = &types.CostInfo{
		TotalCost:    0.01,
		InputTokens:  100,
		OutputTokens: 50,
	}

	results := runStage(t, s, []stage.StreamElement{elem})

	require.Len(t, results, 1)

	// Verify cost info was saved to metadata
	ctx := context.Background()
	state, err := store.Load(ctx, "test-conv-3")
	require.NoError(t, err)
	assert.Equal(t, 0.01, state.Metadata["total_cost_usd"])
	assert.Equal(t, 150, state.Metadata["total_tokens"])
}

func TestArenaStateStoreSaveStage_InvalidStoreType(t *testing.T) {
	// Use a non-ArenaStateStore
	store := runtimeStatestore.NewMemoryStore()

	cfg := &pipeline.StateStoreConfig{
		Store:          store,
		ConversationID: "test-conv",
	}

	s := NewArenaStateStoreSaveStage(cfg)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "Test"),
	}

	input := make(chan stage.StreamElement, len(inputs))
	for _, elem := range inputs {
		input <- elem
	}
	close(input)

	output := make(chan stage.StreamElement, 100)
	ctx := context.Background()

	err := s.Process(ctx, input, output)

	// Should error with invalid store type
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid store type")
}

func TestArenaStateStoreSaveStage_UpdateExistingState(t *testing.T) {
	store := statestore.NewArenaStateStore()
	ctx := context.Background()

	// Pre-populate state
	_ = store.Save(ctx, &runtimeStatestore.ConversationState{
		ID: "test-conv-5",
		Messages: []types.Message{
			{Role: "user", Content: "Old message"},
		},
	})

	cfg := &pipeline.StateStoreConfig{
		Store:          store,
		ConversationID: "test-conv-5",
	}

	s := NewArenaStateStoreSaveStage(cfg)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "New message"),
	}

	_ = runStage(t, s, inputs)

	// Verify state was updated
	state, err := store.Load(ctx, "test-conv-5")
	require.NoError(t, err)
	assert.Len(t, state.Messages, 1)
	assert.Equal(t, "New message", state.Messages[0].Content)
}

func TestArenaStateStoreSaveStage_NilStore(t *testing.T) {
	cfg := &pipeline.StateStoreConfig{
		Store:          nil,
		ConversationID: "test-conv",
	}

	s := NewArenaStateStoreSaveStage(cfg)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "Test"),
	}

	results := runStage(t, s, inputs)

	// Should just forward elements when store is nil
	require.Len(t, results, 1)
}

func TestCreateSystemMessage(t *testing.T) {
	timestamp := time.Now()
	msg := createSystemMessage("You are helpful", timestamp)

	assert.Equal(t, "system", msg.Role)
	assert.Equal(t, "You are helpful", msg.Content)
	assert.Equal(t, timestamp, msg.Timestamp)
	require.Len(t, msg.Parts, 1)
	assert.Equal(t, "text", msg.Parts[0].Type)
}

func TestPrependSystemMessage_NoExisting(t *testing.T) {
	messages := []types.Message{
		{Role: "user", Content: "Hello"},
	}

	result := prependSystemMessage(messages, "System prompt")

	require.Len(t, result, 2)
	assert.Equal(t, "system", result[0].Role)
	assert.Equal(t, "System prompt", result[0].Content)
	assert.Equal(t, "user", result[1].Role)
}

func TestPrependSystemMessage_AlreadyExists(t *testing.T) {
	messages := []types.Message{
		{Role: "system", Content: "Existing system"},
		{Role: "user", Content: "Hello"},
	}

	result := prependSystemMessage(messages, "New system prompt")

	// Should NOT prepend when system message already exists
	require.Len(t, result, 2)
	assert.Equal(t, "system", result[0].Role)
	assert.Equal(t, "Existing system", result[0].Content) // Original preserved
}

func TestPrependSystemMessage_EmptyMessages(t *testing.T) {
	result := prependSystemMessage(nil, "System prompt")

	require.Len(t, result, 1)
	assert.Equal(t, "system", result[0].Role)
}

// =============================================================================
// Selfplay Metadata Preservation Tests (TDD for duplex mode)
// =============================================================================

// TestArenaStateStoreSaveStage_PreservesMessageMeta verifies that Message.Meta
// is preserved when saving through the stage. This is critical for selfplay
// metadata (self_play, persona, selfplay_turn_index) to appear in output.
func TestArenaStateStoreSaveStage_PreservesMessageMeta(t *testing.T) {
	store := statestore.NewArenaStateStore()

	cfg := &pipeline.StateStoreConfig{
		Store:          store,
		ConversationID: "test-selfplay-meta",
	}

	s := NewArenaStateStoreSaveStage(cfg)

	// Create a user message with Meta set (like selfplay does)
	userMsg := &types.Message{
		Role:    "user",
		Content: "Generated selfplay text",
		Meta: map[string]interface{}{
			"self_play":           true,
			"persona":             "curious-customer",
			"selfplay_turn_index": 1,
		},
	}
	userElem := stage.NewMessageElement(userMsg)

	results := runStage(t, s, []stage.StreamElement{userElem})

	// Should forward all elements
	require.Len(t, results, 1)

	// Verify state was saved
	ctx := context.Background()
	state, err := store.Load(ctx, "test-selfplay-meta")
	require.NoError(t, err)
	require.NotNil(t, state)
	require.Len(t, state.Messages, 1)

	// CRITICAL: Verify Meta is preserved on the saved message
	savedMsg := state.Messages[0]
	require.NotNil(t, savedMsg.Meta, "Message.Meta should be preserved after save")
	assert.Equal(t, true, savedMsg.Meta["self_play"], "self_play should be true")
	assert.Equal(t, "curious-customer", savedMsg.Meta["persona"], "persona should be preserved")
	assert.Equal(t, 1, savedMsg.Meta["selfplay_turn_index"], "selfplay_turn_index should be preserved")
}

// TestArenaStateStoreSaveStage_PreservesOriginalTextForSelfplay verifies that
// when input transcription is applied to a selfplay message, the original
// LLM-generated text is preserved in meta.original_text.
func TestArenaStateStoreSaveStage_PreservesOriginalTextForSelfplay(t *testing.T) {
	store := statestore.NewArenaStateStore()

	cfg := &pipeline.StateStoreConfig{
		Store:          store,
		ConversationID: "test-selfplay-transcription",
	}

	s := NewArenaStateStoreSaveStage(cfg)

	// Create selfplay user message with original generated text
	originalText := "What services do you offer for enterprise customers?"
	userMsg := &types.Message{
		Role:    "user",
		Content: originalText,
		Meta: map[string]interface{}{
			"self_play":           true,
			"persona":             "curious-customer",
			"selfplay_turn_index": 1,
		},
	}

	// Create assistant response with input_transcription (simulating Gemini's transcription)
	// Note: Gemini often produces slightly different/truncated transcriptions
	transcribedText := " kind of services do you offer for enterprise" // Truncated version
	assistantMsg := &types.Message{
		Role:    "assistant",
		Content: "We offer several enterprise services...",
	}
	assistantElem := stage.NewMessageElement(assistantMsg)
	assistantElem.Meta.Transcription = &stage.Transcription{Text: transcribedText}

	results := runStage(t, s, []stage.StreamElement{
		stage.NewMessageElement(userMsg),
		assistantElem,
	})

	require.Len(t, results, 2)

	// Verify state was saved
	ctx := context.Background()
	state, err := store.Load(ctx, "test-selfplay-transcription")
	require.NoError(t, err)
	require.Len(t, state.Messages, 2)

	// Check that the user message has both the transcription AND original text
	savedUserMsg := state.Messages[0]

	// Content should be the transcription (what Gemini heard)
	assert.Equal(t, transcribedText, savedUserMsg.Content, "Content should be the transcription")

	// Original text should be preserved in meta
	require.NotNil(t, savedUserMsg.Meta, "Meta should exist")
	originalInMeta, ok := savedUserMsg.Meta["original_text"].(string)
	require.True(t, ok, "original_text should be in meta")
	assert.Equal(t, originalText, originalInMeta, "Original selfplay text should be preserved in meta")
}

// TestArenaStateStoreSaveStage_TranscriptionAppliedToCorrectTurn verifies that
// when messages arrive out of order due to race conditions (next turn's user message
// arrives before previous turn's assistant response), the transcription is still
// applied to the CORRECT user message (the one that precedes the assistant response).
func TestArenaStateStoreSaveStage_TranscriptionAppliedToCorrectTurn(t *testing.T) {
	store := statestore.NewArenaStateStore()

	cfg := &pipeline.StateStoreConfig{
		Store:          store,
		ConversationID: "test-transcription-race",
	}

	s := NewArenaStateStoreSaveStage(cfg)

	// Simulate a race condition where messages arrive in this order:
	// 1. User message 1 (selfplay turn 1)
	// 2. User message 2 (selfplay turn 2) - arrives BEFORE assistant 1!
	// 3. Assistant message 1 (with transcription for turn 1)
	// 4. Assistant message 2 (with transcription for turn 2)
	//
	// The transcription from assistant 1 should be applied to user 1, NOT user 2.

	user1Original := "Hello, what services do you offer?"
	user1 := &types.Message{
		Role:    "user",
		Content: user1Original,
		Meta: map[string]interface{}{
			"self_play":           true,
			"selfplay_turn_index": 1,
		},
	}

	user2Original := "That sounds great! What about pricing?"
	user2 := &types.Message{
		Role:    "user",
		Content: user2Original,
		Meta: map[string]interface{}{
			"self_play":           true,
			"selfplay_turn_index": 2,
		},
	}

	transcript1 := " Hello what services do you offer"
	assistant1 := &types.Message{
		Role:    "assistant",
		Content: "We offer many services...",
	}
	assistant1Elem := stage.NewMessageElement(assistant1)
	assistant1Elem.Meta.Transcription = &stage.Transcription{Text: transcript1}

	transcript2 := " That sounds great what about pricing"
	assistant2 := &types.Message{
		Role:    "assistant",
		Content: "Our pricing is...",
	}
	assistant2Elem := stage.NewMessageElement(assistant2)
	assistant2Elem.Meta.Transcription = &stage.Transcription{Text: transcript2}

	// Send messages in the "race condition" order
	results := runStage(t, s, []stage.StreamElement{
		stage.NewMessageElement(user1), // User 1 arrives first
		stage.NewMessageElement(user2), // User 2 arrives before assistant 1!
		assistant1Elem,                 // Assistant 1 with transcript for user 1
		assistant2Elem,                 // Assistant 2 with transcript for user 2
	})

	require.Len(t, results, 4)

	// Verify state
	ctx := context.Background()
	state, err := store.Load(ctx, "test-transcription-race")
	require.NoError(t, err)
	require.Len(t, state.Messages, 4)

	// User 1 should have transcript 1 (NOT transcript 2!)
	assert.Equal(t, transcript1, state.Messages[0].Content,
		"User 1 should have transcript 1, not transcript 2")
	assert.Equal(t, user1Original, state.Messages[0].Meta["original_text"],
		"User 1 should preserve its original text")

	// User 2 should have transcript 2
	assert.Equal(t, transcript2, state.Messages[1].Content,
		"User 2 should have transcript 2")
	assert.Equal(t, user2Original, state.Messages[1].Meta["original_text"],
		"User 2 should preserve its original text")
}

// TestArenaStateStoreSaveStage_PreservesMetaAcrossMultipleMessages verifies
// that Meta is preserved for both user and assistant messages in a conversation.
func TestArenaStateStoreSaveStage_PreservesMetaAcrossMultipleMessages(t *testing.T) {
	store := statestore.NewArenaStateStore()

	cfg := &pipeline.StateStoreConfig{
		Store:          store,
		ConversationID: "test-multi-meta",
	}

	s := NewArenaStateStoreSaveStage(cfg)

	// Create user message with selfplay Meta
	userMsg := &types.Message{
		Role:    "user",
		Content: "First selfplay message",
		Meta: map[string]interface{}{
			"self_play":           true,
			"selfplay_turn_index": 1,
		},
	}

	// Create assistant message (no Meta expected from pipeline)
	assistantMsg := &types.Message{
		Role:    "assistant",
		Content: "Assistant response",
	}

	// Create second user message with different selfplay index
	userMsg2 := &types.Message{
		Role:    "user",
		Content: "Second selfplay message",
		Meta: map[string]interface{}{
			"self_play":           true,
			"selfplay_turn_index": 2,
		},
	}

	results := runStage(t, s, []stage.StreamElement{
		stage.NewMessageElement(userMsg),
		stage.NewMessageElement(assistantMsg),
		stage.NewMessageElement(userMsg2),
	})

	require.Len(t, results, 3)

	// Verify state was saved
	ctx := context.Background()
	state, err := store.Load(ctx, "test-multi-meta")
	require.NoError(t, err)
	require.Len(t, state.Messages, 3)

	// First user message should have Meta with selfplay_turn_index=1
	assert.NotNil(t, state.Messages[0].Meta, "First user message should have Meta")
	assert.Equal(t, 1, state.Messages[0].Meta["selfplay_turn_index"])

	// Assistant message should have nil Meta (or empty)
	// This is expected - assistants don't have selfplay metadata

	// Second user message should have Meta with selfplay_turn_index=2
	assert.NotNil(t, state.Messages[2].Meta, "Second user message should have Meta")
	assert.Equal(t, 2, state.Messages[2].Meta["selfplay_turn_index"])
}

// TestArenaStateStoreSaveStage_TranscriptionByTurnID verifies that
// transcriptions are matched to user messages by turn_id when available,
// providing reliable correlation regardless of message arrival order.
func TestArenaStateStoreSaveStage_TranscriptionByTurnID(t *testing.T) {
	store := statestore.NewArenaStateStore()

	cfg := &pipeline.StateStoreConfig{
		Store:          store,
		ConversationID: "test-turn-id",
	}

	s := NewArenaStateStoreSaveStage(cfg)

	// Create user messages with unique turn_ids
	turnID1 := "turn-uuid-001"
	turnID2 := "turn-uuid-002"

	user1Original := "Hello, how are you?"
	user1 := &types.Message{
		Role:    "user",
		Content: user1Original,
		Meta: map[string]interface{}{
			"turn_id":   turnID1,
			"self_play": true,
		},
	}

	user2Original := "What services do you offer?"
	user2 := &types.Message{
		Role:    "user",
		Content: user2Original,
		Meta: map[string]interface{}{
			"turn_id":   turnID2,
			"self_play": true,
		},
	}

	// Transcriptions arrive with their corresponding turn_ids
	transcript1 := " Hello how are you"
	assistant1 := &types.Message{
		Role:    "assistant",
		Content: "I'm doing well, thanks!",
	}
	assistant1Elem := stage.NewMessageElement(assistant1)
	assistant1Elem.Meta.Transcription = &stage.Transcription{Text: transcript1}
	tID1 := turnID1
	assistant1Elem.Meta.TurnID = &tID1

	transcript2 := " What services do you offer"
	assistant2 := &types.Message{
		Role:    "assistant",
		Content: "We offer many services...",
	}
	assistant2Elem := stage.NewMessageElement(assistant2)
	assistant2Elem.Meta.Transcription = &stage.Transcription{Text: transcript2}
	tID2 := turnID2
	assistant2Elem.Meta.TurnID = &tID2

	// Send messages in a mixed order (user2 arrives before assistant1)
	// With turn_id matching, transcriptions should still be correct
	results := runStage(t, s, []stage.StreamElement{
		stage.NewMessageElement(user1), // User 1 arrives
		stage.NewMessageElement(user2), // User 2 arrives before assistant1!
		assistant1Elem,                 // Assistant 1 with transcript for turn_id1
		assistant2Elem,                 // Assistant 2 with transcript for turn_id2
	})

	require.Len(t, results, 4)

	// Verify state
	ctx := context.Background()
	state, err := store.Load(ctx, "test-turn-id")
	require.NoError(t, err)
	require.Len(t, state.Messages, 4)

	// User 1 should have transcript 1 (matched by turn_id, not order)
	assert.Equal(t, transcript1, state.Messages[0].Content,
		"User 1 should have transcript 1 (matched by turn_id)")
	assert.Equal(t, user1Original, state.Messages[0].Meta["original_text"],
		"User 1 should preserve original text")

	// User 2 should have transcript 2 (matched by turn_id, not order)
	assert.Equal(t, transcript2, state.Messages[1].Content,
		"User 2 should have transcript 2 (matched by turn_id)")
	assert.Equal(t, user2Original, state.Messages[1].Meta["original_text"],
		"User 2 should preserve original text")
}
