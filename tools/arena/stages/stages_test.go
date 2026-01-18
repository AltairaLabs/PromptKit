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

	// History elements should have source metadata
	assert.Equal(t, "history_injection", results[0].Metadata["source"])
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
// MetadataInjectionStage Tests
// =============================================================================

func TestMetadataInjectionStage_InjectsMetadata(t *testing.T) {
	metadata := map[string]interface{}{
		"key1": "value1",
		"key2": 42,
	}

	s := NewMetadataInjectionStage(metadata)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "Test"),
	}

	results := runStage(t, s, inputs)

	require.Len(t, results, 1)
	assert.Equal(t, "value1", results[0].Metadata["key1"])
	assert.Equal(t, 42, results[0].Metadata["key2"])
}

func TestMetadataInjectionStage_EmptyMetadata(t *testing.T) {
	s := NewMetadataInjectionStage(nil)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "Test"),
	}

	results := runStage(t, s, inputs)

	require.Len(t, results, 1)
	// Should still work with empty metadata
	assert.Equal(t, "Test", results[0].Message.Content)
}

func TestMetadataInjectionStage_PreservesExistingMetadata(t *testing.T) {
	metadata := map[string]interface{}{
		"new_key": "new_value",
	}

	s := NewMetadataInjectionStage(metadata)

	elem := newTestMessageElement("user", "Test")
	elem.Metadata = map[string]interface{}{
		"existing_key": "existing_value",
	}

	results := runStage(t, s, []stage.StreamElement{elem})

	require.Len(t, results, 1)
	assert.Equal(t, "existing_value", results[0].Metadata["existing_key"])
	assert.Equal(t, "new_value", results[0].Metadata["new_key"])
}

// =============================================================================
// VariableInjectionStage Tests
// =============================================================================

func TestVariableInjectionStage_InjectsVariables(t *testing.T) {
	variables := map[string]string{
		"name":   "John",
		"domain": "tech",
	}

	s := NewVariableInjectionStage(variables)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "Test"),
	}

	results := runStage(t, s, inputs)

	require.Len(t, results, 1)
	assert.Equal(t, variables, results[0].Metadata["variables"])
}

func TestVariableInjectionStage_NilVariables(t *testing.T) {
	s := NewVariableInjectionStage(nil)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "Test"),
	}

	results := runStage(t, s, inputs)

	require.Len(t, results, 1)
	// Should have nil variables in metadata
	assert.Nil(t, results[0].Metadata["variables"])
}

// =============================================================================
// TurnIndexStage Tests
// =============================================================================

func TestTurnIndexStage_CountsTurns(t *testing.T) {
	s := NewTurnIndexStage()

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "User 1"),
		newTestMessageElement("assistant", "Assistant 1"),
		newTestMessageElement("user", "User 2"),
		newTestMessageElement("assistant", "Assistant 2"),
	}

	results := runStage(t, s, inputs)

	require.Len(t, results, 4)

	// All elements should have the same turn counts
	for _, elem := range results {
		assert.Equal(t, 2, elem.Metadata["arena_user_completed_turns"])
		assert.Equal(t, 3, elem.Metadata["arena_user_next_turn"])
		assert.Equal(t, 2, elem.Metadata["arena_assistant_completed_turns"])
		assert.Equal(t, 3, elem.Metadata["arena_assistant_next_turn"])
	}
}

func TestTurnIndexStage_EmptyInput(t *testing.T) {
	s := NewTurnIndexStage()

	results := runStage(t, s, nil)

	require.Len(t, results, 0)
}

func TestTurnIndexStage_OnlyUserMessages(t *testing.T) {
	s := NewTurnIndexStage()

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "User 1"),
		newTestMessageElement("user", "User 2"),
	}

	results := runStage(t, s, inputs)

	require.Len(t, results, 2)
	assert.Equal(t, 2, results[0].Metadata["arena_user_completed_turns"])
	assert.Equal(t, 0, results[0].Metadata["arena_assistant_completed_turns"])
}

func TestTurnIndexStage_DoesNotOverwriteExisting(t *testing.T) {
	s := NewTurnIndexStage()

	elem := newTestMessageElement("user", "Test")
	elem.Metadata = map[string]interface{}{
		"arena_user_completed_turns": 100, // Pre-existing value
	}

	results := runStage(t, s, []stage.StreamElement{elem})

	require.Len(t, results, 1)
	// Should NOT overwrite the existing value
	assert.Equal(t, 100, results[0].Metadata["arena_user_completed_turns"])
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

	s := NewMockScenarioContextStage(scenario)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "Test"),
	}

	results := runStage(t, s, inputs)

	require.Len(t, results, 1)
	assert.Equal(t, "test-scenario", results[0].Metadata["mock_scenario_id"])
	assert.NotNil(t, results[0].Metadata["mock_turn_number"])
}

func TestMockScenarioContextStage_NilScenario(t *testing.T) {
	s := NewMockScenarioContextStage(nil)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "Test"),
	}

	results := runStage(t, s, inputs)

	require.Len(t, results, 1)
	// Should forward without scenario metadata
	assert.Nil(t, results[0].Metadata["mock_scenario_id"])
}

func TestMockScenarioContextStage_EmptyScenarioID(t *testing.T) {
	scenario := &config.Scenario{
		ID: "", // Empty ID
	}

	s := NewMockScenarioContextStage(scenario)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "Test"),
	}

	results := runStage(t, s, inputs)

	require.Len(t, results, 1)
	// Should forward without scenario metadata when ID is empty
	assert.Nil(t, results[0].Metadata["mock_scenario_id"])
}

func TestMockScenarioContextStage_TurnNumberFromMetadata(t *testing.T) {
	scenario := &config.Scenario{
		ID: "test-scenario",
	}

	s := NewMockScenarioContextStage(scenario)

	elem := newTestMessageElement("user", "Test")
	elem.Metadata = map[string]interface{}{
		"arena_user_completed_turns": 5,
	}

	results := runStage(t, s, []stage.StreamElement{elem})

	require.Len(t, results, 1)
	// Should use the turn number from metadata
	assert.Equal(t, 5, results[0].Metadata["mock_turn_number"])
}

func TestMockScenarioContextStage_TurnNumberFromAssistantCount(t *testing.T) {
	scenario := &config.Scenario{
		ID: "test-scenario",
	}

	s := NewMockScenarioContextStage(scenario)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "User 1"),
		newTestMessageElement("assistant", "Assistant 1"),
		newTestMessageElement("assistant", "Assistant 2"),
		newTestMessageElement("user", "User 2"),
	}

	results := runStage(t, s, inputs)

	require.Len(t, results, 4)
	// Turn number should be assistant count + 1 = 3
	assert.Equal(t, 3, results[0].Metadata["mock_turn_number"])
}

func TestMockScenarioContextStage_TurnNumberFromUserCount(t *testing.T) {
	scenario := &config.Scenario{
		ID: "test-scenario",
	}

	s := NewMockScenarioContextStage(scenario)

	// Only user messages, no assistant messages
	inputs := []stage.StreamElement{
		newTestMessageElement("user", "User 1"),
		newTestMessageElement("user", "User 2"),
	}

	results := runStage(t, s, inputs)

	require.Len(t, results, 2)
	// Turn number should be user count = 2
	assert.Equal(t, 2, results[0].Metadata["mock_turn_number"])
}

// =============================================================================
// SelfPlayUserTurnContextStage Tests
// =============================================================================

func TestSelfPlayUserTurnContextStage_AddsContext(t *testing.T) {
	scenario := &config.Scenario{
		ID: "selfplay-scenario",
	}

	s := NewSelfPlayUserTurnContextStage(scenario)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "User 1"),
		newTestMessageElement("assistant", "Assistant 1"),
	}

	results := runStage(t, s, inputs)

	require.Len(t, results, 2)

	// Check metadata on first element
	assert.Equal(t, "selfplay-scenario", results[0].Metadata["mock_scenario_id"])
	assert.Equal(t, 1, results[0].Metadata["arena_user_completed_turns"])
	assert.Equal(t, 2, results[0].Metadata["arena_user_next_turn"])
	assert.Equal(t, "self_play_user", results[0].Metadata["arena_role"])
}

func TestSelfPlayUserTurnContextStage_NilScenario(t *testing.T) {
	s := NewSelfPlayUserTurnContextStage(nil)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "Test"),
	}

	results := runStage(t, s, inputs)

	require.Len(t, results, 1)
	// Should not add scenario-specific metadata
	assert.Nil(t, results[0].Metadata["mock_scenario_id"])
}

func TestSelfPlayUserTurnContextStage_UsesExistingTurnCount(t *testing.T) {
	scenario := &config.Scenario{
		ID: "test-scenario",
	}

	s := NewSelfPlayUserTurnContextStage(scenario)

	elem := newTestMessageElement("user", "Test")
	elem.Metadata = map[string]interface{}{
		"arena_user_completed_turns": 10, // Pre-existing count
	}

	results := runStage(t, s, []stage.StreamElement{elem})

	require.Len(t, results, 1)
	// Should use the higher existing count
	assert.Equal(t, 10, results[0].Metadata["arena_user_completed_turns"])
	assert.Equal(t, 11, results[0].Metadata["arena_user_next_turn"])
}

// =============================================================================
// ScenarioContextExtractionStage Tests
// =============================================================================

func TestScenarioContextExtractionStage_ExtractsContext(t *testing.T) {
	scenario := &config.Scenario{
		ID:          "test-scenario",
		Description: "Test scenario description",
		TaskType:    "chat",
		ContextMetadata: &config.ContextMetadata{
			Domain:   "technology",
			UserRole: "developer",
		},
	}

	s := NewScenarioContextExtractionStage(scenario)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "Hello"),
	}

	results := runStage(t, s, inputs)

	require.Len(t, results, 1)
	assert.Equal(t, "technology", results[0].Metadata["domain"])
	assert.Equal(t, "developer", results[0].Metadata["user_context"])
	assert.Equal(t, "developer", results[0].Metadata["user_role"])
	assert.Contains(t, results[0].Metadata["context_slot"], "Test scenario description")
}

func TestScenarioContextExtractionStage_NilScenario(t *testing.T) {
	s := NewScenarioContextExtractionStage(&config.Scenario{})

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "Test"),
	}

	results := runStage(t, s, inputs)

	require.Len(t, results, 1)
	// Should still have empty string values
	assert.Equal(t, "", results[0].Metadata["domain"])
}

func TestScenarioContextExtractionStage_VariablesSubMap(t *testing.T) {
	scenario := &config.Scenario{
		ID:       "test",
		TaskType: "chat",
		ContextMetadata: &config.ContextMetadata{
			Domain: "finance",
		},
	}

	s := NewScenarioContextExtractionStage(scenario)

	inputs := []stage.StreamElement{
		newTestMessageElement("user", "Test"),
	}

	results := runStage(t, s, inputs)

	require.Len(t, results, 1)

	// Check variables sub-map
	vars, ok := results[0].Metadata["variables"].(map[string]string)
	require.True(t, ok)
	assert.Equal(t, "finance", vars["domain"])
}

func TestScenarioContextExtractionStage_DoesNotOverwriteExisting(t *testing.T) {
	scenario := &config.Scenario{
		ContextMetadata: &config.ContextMetadata{
			Domain: "new_domain",
		},
	}

	s := NewScenarioContextExtractionStage(scenario)

	elem := newTestMessageElement("user", "Test")
	elem.Metadata = map[string]interface{}{
		"domain": "existing_domain", // Pre-existing value
	}

	results := runStage(t, s, []stage.StreamElement{elem})

	require.Len(t, results, 1)
	// Should NOT overwrite existing domain
	assert.Equal(t, "existing_domain", results[0].Metadata["domain"])
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
			ToolResult: &types.MessageToolResult{
				ID:      "call1",
				Name:    "tool1",
				Content: "result",
			},
		},
	}

	cloned := deepCloneMessages(original)

	require.Len(t, cloned, 1)
	require.NotNil(t, cloned[0].ToolResult)
	assert.Equal(t, "call1", cloned[0].ToolResult.ID)

	// Modify original should not affect clone
	original[0].ToolResult.Content = "modified"
	assert.Equal(t, "result", cloned[0].ToolResult.Content)
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

	copied := deepCopyMap(original)

	assert.Equal(t, "value1", copied["key1"])
	assert.Equal(t, 42, copied["key2"])

	// Modify original should not affect copy
	original["key1"] = "modified"
	assert.Equal(t, "value1", copied["key1"])
}

func TestDeepCopyMap_Nil(t *testing.T) {
	result := deepCopyMap(nil)
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

	s := NewArenaStateStoreSaveStage(cfg)

	elem := newTestMessageElement("user", "Hello")
	elem.Metadata = map[string]interface{}{
		"system_prompt": "You are a helpful assistant",
	}

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
	elem.Metadata = map[string]interface{}{
		"cost_info": &types.CostInfo{
			TotalCost:    0.01,
			InputTokens:  100,
			OutputTokens: 50,
		},
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

func TestArenaStateStoreSaveStage_WithExecutionTrace(t *testing.T) {
	store := statestore.NewArenaStateStore()

	cfg := &pipeline.StateStoreConfig{
		Store:          store,
		ConversationID: "test-conv-4",
	}

	s := NewArenaStateStoreSaveStage(cfg)

	elem := newTestMessageElement("assistant", "Response")
	elem.Metadata = map[string]interface{}{
		"execution_trace": &pipeline.ExecutionTrace{
			StartedAt: time.Now(),
			LLMCalls:  []pipeline.LLMCall{},
		},
	}

	results := runStage(t, s, []stage.StreamElement{elem})

	require.Len(t, results, 1)

	// Verify state was saved
	ctx := context.Background()
	state, err := store.Load(ctx, "test-conv-4")
	require.NoError(t, err)
	require.NotNil(t, state)
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
	assistantElem.Metadata = map[string]interface{}{
		"input_transcription": transcribedText,
	}

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
	assistant1Elem.Metadata = map[string]interface{}{
		"input_transcription": transcript1,
	}

	transcript2 := " That sounds great what about pricing"
	assistant2 := &types.Message{
		Role:    "assistant",
		Content: "Our pricing is...",
	}
	assistant2Elem := stage.NewMessageElement(assistant2)
	assistant2Elem.Metadata = map[string]interface{}{
		"input_transcription": transcript2,
	}

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
	assistant1Elem.Metadata = map[string]interface{}{
		"input_transcription":   transcript1,
		"transcription_turn_id": turnID1,
	}

	transcript2 := " What services do you offer"
	assistant2 := &types.Message{
		Role:    "assistant",
		Content: "We offer many services...",
	}
	assistant2Elem := stage.NewMessageElement(assistant2)
	assistant2Elem.Metadata = map[string]interface{}{
		"input_transcription":   transcript2,
		"transcription_turn_id": turnID2,
	}

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
