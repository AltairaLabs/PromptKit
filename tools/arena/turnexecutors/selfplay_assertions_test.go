package turnexecutors

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	runtimestore "github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
	"github.com/AltairaLabs/PromptKit/tools/arena/selfplay"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestSelfPlayExecutor_WithAssertions_Pass verifies that assertions work correctly in self-play mode
func TestSelfPlayExecutor_WithAssertions_Pass(t *testing.T) {
	// Setup mock provider for assistant responses
	mockProvider := new(MockProvider)
	mockProvider.On("ID").Return("test-provider")
	mockProvider.On("ShouldIncludeRawOutput").Return(false).Maybe()
	mockProvider.On("SupportsStreaming").Return(false).Maybe()

	// Mock response that matches assertion (contains "renewable")
	costBreakdown := types.CostInfo{
		InputTokens:   10,
		OutputTokens:  5,
		InputCostUSD:  0.0005,
		OutputCostUSD: 0.0005,
		TotalCost:     0.001,
	}
	response := providers.PredictionResponse{
		Content:  "Renewable energy sources like solar and wind are essential for sustainability.",
		CostInfo: &costBreakdown,
	}
	mockProvider.On("Predict", mock.Anything, mock.Anything).Return(response, nil)

	// Setup mock self-play content provider - reuse from streaming_test.go pattern
	mockContentGen := &selfPlayMockContentGenerator{
		message: types.Message{
			Role:    "user",
			Content: "Tell me about renewable energy solutions.",
		},
	}
	mockContentProvider := &selfPlayMockProvider{
		contentGen: mockContentGen,
	}

	// Setup state store
	toolRegistry := tools.NewRegistry()
	store := statestore.NewArenaStateStore()
	storeConfig := &StateStoreConfig{
		Store:  store,
		UserID: "test-user",
	}

	scenario := &config.Scenario{
		TaskType: "test",
	}

	// Create executors
	pipelineExecutor := NewPipelineExecutor(toolRegistry, nil)
	selfPlayExecutor := NewSelfPlayExecutor(pipelineExecutor, mockContentProvider)

	// Pre-initialize state store
	storeIface := storeConfig.Store
	rtStore, ok := storeIface.(runtimestore.Store)
	require.True(t, ok, "state store must implement runtimestore.Store")
	initState := &runtimestore.ConversationState{
		ID:       "test-selfplay-conv",
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
		PromptRegistry:   nil,
		TaskType:         "test",
		StateStoreConfig: storeConfig,
		ConversationID:   "test-selfplay-conv",
		SelfPlayRole:     "gemini-user",
		SelfPlayPersona:  "curious-learner",
		Assertions: []assertions.AssertionConfig{
			{
				Type: "content_includes",
				Params: map[string]interface{}{
					"patterns": []string{"renewable"},
				},
				Message: "Expected content to include renewable energy topics",
			},
		},
	}

	// Execute - should pass
	err := selfPlayExecutor.ExecuteTurn(context.Background(), req)
	if err != nil {
		t.Fatalf("Expected no error with passing assertions in self-play, got: %v", err)
	}

	// Verify assertion results were attached to assistant message
	arenaState, err := store.GetArenaState(context.Background(), "test-selfplay-conv")
	if err != nil {
		t.Fatalf("Failed to get arena state: %v", err)
	}

	// Should have 3 messages: system + user (self-play generated) + assistant
	if len(arenaState.Messages) < 3 {
		t.Fatalf("Expected at least 3 messages (system + user + assistant), got %d", len(arenaState.Messages))
	}

	// Check that first message is system
	if arenaState.Messages[0].Role != "system" {
		t.Fatalf("Expected first message to be system, got %s", arenaState.Messages[0].Role)
	}

	// Check that second message is the self-play generated user message
	userMsg := arenaState.Messages[1]
	if userMsg.Role != "user" {
		t.Fatalf("Expected second message to be user, got %s", userMsg.Role)
	}
	if userMsg.Content != "Tell me about renewable energy solutions." {
		t.Fatalf("Unexpected user message content: %s", userMsg.Content)
	}

	// Check that user message has self-play metadata
	if userMsg.Meta == nil || userMsg.Meta["raw_response"] == nil {
		t.Fatal("Expected self-play metadata on user message")
	}

	// Check assistant message has assertions
	assistantMsg := arenaState.Messages[2]
	if assistantMsg.Role != "assistant" {
		t.Fatalf("Expected second message to be assistant, got %s", assistantMsg.Role)
	}

	if assistantMsg.Meta == nil || assistantMsg.Meta["assertions"] == nil {
		t.Fatal("Expected assertions to be attached to assistant message meta")
	}

	mockProvider.AssertExpectations(t)
}

// TestSelfPlayExecutor_WithAssertions_Fail verifies that assertion failures are properly caught in self-play
func TestSelfPlayExecutor_WithAssertions_Fail(t *testing.T) {
	// Setup mock provider for assistant responses
	mockProvider := new(MockProvider)
	mockProvider.On("ID").Return("test-provider")
	mockProvider.On("ShouldIncludeRawOutput").Return(false).Maybe()
	mockProvider.On("SupportsStreaming").Return(false).Maybe()

	// Mock response that DOESN'T match assertion (missing "solar")
	costBreakdown := types.CostInfo{
		InputTokens:   10,
		OutputTokens:  5,
		InputCostUSD:  0.0005,
		OutputCostUSD: 0.0005,
		TotalCost:     0.001,
	}
	response := providers.PredictionResponse{
		Content:  "Wind energy is a good renewable source.",
		CostInfo: &costBreakdown,
	}
	mockProvider.On("Predict", mock.Anything, mock.Anything).Return(response, nil)

	// Setup mock self-play content provider
	mockContentGen := &selfPlayMockContentGenerator{
		message: types.Message{
			Role:    "user",
			Content: "Tell me about solar power.",
		},
	}
	mockContentProvider := &selfPlayMockProvider{
		contentGen: mockContentGen,
	}

	toolRegistry := tools.NewRegistry()
	store := statestore.NewArenaStateStore()
	storeConfig := &StateStoreConfig{
		Store:  store,
		UserID: "test-user",
	}

	scenario := &config.Scenario{
		TaskType: "test",
	}

	pipelineExecutor := NewPipelineExecutor(toolRegistry, nil)
	selfPlayExecutor := NewSelfPlayExecutor(pipelineExecutor, mockContentProvider)

	// Pre-initialize state store
	storeIface := storeConfig.Store
	rtStore, ok := storeIface.(runtimestore.Store)
	require.True(t, ok)
	initState := &runtimestore.ConversationState{
		ID:       "test-selfplay-fail",
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
		ConversationID:   "test-selfplay-fail",
		SelfPlayRole:     "gemini-user",
		SelfPlayPersona:  "",
		Assertions: []assertions.AssertionConfig{
			{
				Type: "content_includes",
				Params: map[string]interface{}{
					"patterns": []string{"solar"}, // Word not in response
				},
				Message: "Expected content to include solar energy",
			},
		},
	}

	// Execute - should fail with assertion error
	err := selfPlayExecutor.ExecuteTurn(context.Background(), req)
	if err == nil {
		t.Fatal("Expected error for failed assertion in self-play, got nil")
	}

	// Error message should mention the assertion failure
	errMsg := err.Error()
	if errMsg == "" {
		t.Fatal("Expected non-empty error message")
	}

	mockProvider.AssertExpectations(t)
}

// Mock types for self-play assertions testing (prevent redeclaration)

type selfPlayMockProvider struct {
	contentGen *selfPlayMockContentGenerator
}

func (m *selfPlayMockProvider) GetContentGenerator(role, persona string) (selfplay.Generator, error) {
	return m.contentGen, nil
}

type selfPlayMockContentGenerator struct {
	message types.Message
}

func (m *selfPlayMockContentGenerator) NextUserTurn(ctx context.Context, history []types.Message, scenarioID string, opts *selfplay.GeneratorOptions) (*pipeline.ExecutionResult, error) {
	return &pipeline.ExecutionResult{
		Response: &pipeline.Response{
			Content: m.message.Content,
			Role:    m.message.Role,
		},
		CostInfo: types.CostInfo{}, // Empty cost info for mock
		Metadata: map[string]interface{}{
			"persona": "mock-persona",
			"role":    "self-play-user",
		},
	}, nil
}
