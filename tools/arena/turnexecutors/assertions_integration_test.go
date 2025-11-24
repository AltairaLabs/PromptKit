package turnexecutors

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	runtimestore "github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestPipelineExecutor_AssertionsPass(t *testing.T) {
	// Setup mock provider
	mockProvider := new(MockProvider)
	mockProvider.On("ID").Return("test-provider")
	mockProvider.On("ShouldIncludeRawOutput").Return(false).Maybe()
	mockProvider.On("SupportsStreaming").Return(false).Maybe()

	// Mock response that matches assertion (contains "search")
	costBreakdown := types.CostInfo{
		InputTokens:   10,
		OutputTokens:  5,
		InputCostUSD:  0.0005,
		OutputCostUSD: 0.0005,
		TotalCost:     0.001,
	}
	response := providers.PredictionResponse{
		Content:  "I will search for that information.", // Contains "search"
		CostInfo: &costBreakdown,
	}
	mockProvider.On("Predict", mock.Anything, mock.Anything).Return(response, nil)

	toolRegistry := tools.NewRegistry()
	store := statestore.NewArenaStateStore()
	storeConfig := &StateStoreConfig{
		Store:  store,
		UserID: "test-user",
	}

	scenario := &config.Scenario{
		TaskType: "test",
	}

	executor := NewPipelineExecutor(toolRegistry, nil)

	// Pre-initialize state store (required for ArenaStateStore)
	storeIface := storeConfig.Store
	rtStore, ok := storeIface.(runtimestore.Store)
	require.True(t, ok, "state store must implement runtimestore.Store")
	initState := &runtimestore.ConversationState{
		ID:       "test-conv",
		UserID:   "test-user",
		Messages: []types.Message{},
		Metadata: map[string]interface{}{},
	}
	saveErr := rtStore.Save(context.Background(), initState)
	require.NoError(t, saveErr)

	// Create request with assertions
	req := TurnRequest{
		Provider:         mockProvider,
		Scenario:         scenario,
		Temperature:      0.7,
		MaxTokens:        100,
		PromptRegistry:   nil, // Tests use nil
		TaskType:         "test",
		StateStoreConfig: storeConfig,
		ConversationID:   "test-conv",
		Assertions: []assertions.AssertionConfig{
			{
				Type: "content_includes",
				Params: map[string]interface{}{
					"patterns": []string{"search"},
				},
				Message: "Expected content to include search patterns",
			},
		},
	}

	userMsg := types.Message{
		Role:    "user",
		Content: "Search for something",
	}

	// Execute - should pass
	err := executor.Execute(context.Background(), req, userMsg)
	if err != nil {
		t.Fatalf("Expected no error with passing assertions, got: %v", err)
	}

	// Verify assertion results were attached to message
	arenaState, err := store.GetArenaState(context.Background(), "test-conv")
	if err != nil {
		t.Fatalf("Failed to get arena state: %v", err)
	}

	if len(arenaState.Messages) < 2 {
		t.Fatalf("Expected at least 2 messages (user + assistant), got %d", len(arenaState.Messages))
	}

	assistantMsg := arenaState.Messages[len(arenaState.Messages)-1]
	if assistantMsg.Role != "assistant" {
		t.Fatalf("Expected last message to be assistant, got %s", assistantMsg.Role)
	}

	if assistantMsg.Meta == nil || assistantMsg.Meta["assertions"] == nil {
		t.Fatal("Expected assertions to be attached to assistant message meta")
	}
}

func TestPipelineExecutor_AssertionsFail(t *testing.T) {
	// Setup mock provider
	mockProvider := new(MockProvider)
	mockProvider.On("ID").Return("test-provider")
	mockProvider.On("ShouldIncludeRawOutput").Return(false).Maybe()
	mockProvider.On("SupportsStreaming").Return(false).Maybe()

	// Mock response without tool calls
	costBreakdown := types.CostInfo{
		InputTokens:   10,
		OutputTokens:  5,
		InputCostUSD:  0.0005,
		OutputCostUSD: 0.0005,
		TotalCost:     0.001,
	}
	response := providers.PredictionResponse{
		Content:  "I will not use any tools.",
		CostInfo: &costBreakdown,
	}
	mockProvider.On("Predict", mock.Anything, mock.Anything).Return(response, nil)

	toolRegistry := tools.NewRegistry()
	store := statestore.NewArenaStateStore()
	storeConfig := &StateStoreConfig{
		Store:  store,
		UserID: "test-user",
	}

	scenario := &config.Scenario{
		TaskType: "test",
	}

	executor := NewPipelineExecutor(toolRegistry, nil)

	// Pre-initialize state store (required for ArenaStateStore)
	storeIface := storeConfig.Store
	rtStore, ok := storeIface.(runtimestore.Store)
	require.True(t, ok, "state store must implement runtimestore.Store")
	initState := &runtimestore.ConversationState{
		ID:       "test-conv-fail",
		UserID:   "test-user",
		Messages: []types.Message{},
		Metadata: map[string]interface{}{},
	}
	saveErr := rtStore.Save(context.Background(), initState)
	require.NoError(t, saveErr)

	// Create request with assertions that will fail
	req := TurnRequest{
		Provider:         mockProvider,
		Scenario:         scenario,
		Temperature:      0.7,
		MaxTokens:        100,
		PromptRegistry:   nil,
		TaskType:         "test",
		StateStoreConfig: storeConfig,
		ConversationID:   "test-conv-fail",
		Assertions: []assertions.AssertionConfig{
			{
				Type: "content_includes",
				Params: map[string]interface{}{
					"patterns": []string{"missing_word"}, // Word not in response
				},
				Message: "Expected content to include missing word",
			},
		},
	}

	userMsg := types.Message{
		Role:    "user",
		Content: "Search for something",
	}

	// Execute - should fail with assertion error
	err := executor.Execute(context.Background(), req, userMsg)
	if err == nil {
		t.Fatal("Expected error for failed assertion, got nil")
	}

	// Error message should mention the assertion failure
	errMsg := err.Error()
	if errMsg == "" {
		t.Fatal("Expected non-empty error message")
	}
}

func TestPipelineExecutor_NoAssertions(t *testing.T) {
	// Setup mock provider
	mockProvider := new(MockProvider)
	mockProvider.On("ID").Return("test-provider")
	mockProvider.On("ShouldIncludeRawOutput").Return(false).Maybe()
	mockProvider.On("SupportsStreaming").Return(false).Maybe()

	// Mock response
	costBreakdown := types.CostInfo{
		InputTokens:   10,
		OutputTokens:  5,
		InputCostUSD:  0.0005,
		OutputCostUSD: 0.0005,
		TotalCost:     0.001,
	}
	response := providers.PredictionResponse{
		Content:  "Test response",
		CostInfo: &costBreakdown,
	}
	mockProvider.On("Predict", mock.Anything, mock.Anything).Return(response, nil)

	toolRegistry := tools.NewRegistry()
	store := statestore.NewArenaStateStore()
	storeConfig := &StateStoreConfig{
		Store:  store,
		UserID: "test-user",
	}

	scenario := &config.Scenario{
		TaskType: "test",
	}

	executor := NewPipelineExecutor(toolRegistry, nil)

	// Pre-initialize state store (required for ArenaStateStore)
	storeIface := storeConfig.Store
	rtStore, ok := storeIface.(runtimestore.Store)
	require.True(t, ok, "state store must implement runtimestore.Store")
	initState := &runtimestore.ConversationState{
		ID:       "test-conv-no-assertions",
		UserID:   "test-user",
		Messages: []types.Message{},
		Metadata: map[string]interface{}{},
	}
	saveErr := rtStore.Save(context.Background(), initState)
	require.NoError(t, saveErr)

	// Create request without assertions
	req := TurnRequest{
		Provider:         mockProvider,
		Scenario:         scenario,
		Temperature:      0.7,
		MaxTokens:        100,
		PromptRegistry:   nil,
		TaskType:         "test",
		StateStoreConfig: storeConfig,
		ConversationID:   "test-conv-no-assertions",
		Assertions:       nil, // No assertions
	}

	userMsg := types.Message{
		Role:    "user",
		Content: "Test message",
	}

	// Execute - should succeed
	err := executor.Execute(context.Background(), req, userMsg)
	if err != nil {
		t.Fatalf("Expected no error without assertions, got: %v", err)
	}

	// Verify message was saved but no assertion meta
	arenaState, err := store.GetArenaState(context.Background(), "test-conv-no-assertions")
	if err != nil {
		t.Fatalf("Failed to get arena state: %v", err)
	}

	if len(arenaState.Messages) < 2 {
		t.Fatal("Expected at least 2 messages (user + assistant)")
	}

	assistantMsg := arenaState.Messages[len(arenaState.Messages)-1]
	if assistantMsg.Role != "assistant" {
		t.Fatalf("Expected last message to be assistant, got %s", assistantMsg.Role)
	}

	// Should not have assertions in meta when none configured
	if assistantMsg.Meta != nil && assistantMsg.Meta["assertions"] != nil {
		t.Error("Did not expect assertions in meta when none configured")
	}
}
