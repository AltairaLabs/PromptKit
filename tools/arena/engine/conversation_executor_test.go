package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/selfplay"
	"github.com/AltairaLabs/PromptKit/tools/arena/turnexecutors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// createTestStateStore creates a MemoryStore for testing
func createTestStateStore() statestore.Store {
	return statestore.NewMemoryStore()
}

// MockTurnExecutor implements turnexecutors.TurnExecutor for testing
type MockTurnExecutor struct {
	executeFunc func(ctx context.Context, req turnexecutors.TurnRequest) error
	callCount   int
}

func (m *MockTurnExecutor) ExecuteTurn(ctx context.Context, req turnexecutors.TurnRequest) error {
	m.callCount++

	if m.executeFunc != nil {
		// If a custom execute func is provided, use it exclusively
		// (it's responsible for saving to StateStore if needed)
		return m.executeFunc(ctx, req)
	}

	// Default behavior: create mock messages with cost info and save to StateStore
	messages := []types.Message{
		{
			Role:    "user",
			Content: req.ScriptedContent, // Use the actual content from the request
		},
		{
			Role:    "assistant",
			Content: "test response",
			CostInfo: &types.CostInfo{
				InputTokens:   10,
				OutputTokens:  20,
				InputCostUSD:  0.0001,
				OutputCostUSD: 0.0002,
				TotalCost:     0.0003,
			},
		},
	} // Save messages to StateStore if configured
	if req.StateStoreConfig != nil && req.StateStoreConfig.Store != nil && req.ConversationID != "" {
		store, ok := req.StateStoreConfig.Store.(statestore.Store)
		if ok {
			// Load existing conversation
			state, loadErr := store.Load(ctx, req.ConversationID)
			if loadErr != nil && !errors.Is(loadErr, statestore.ErrNotFound) {
				return loadErr
			}
			if state == nil {
				state = &statestore.ConversationState{
					ID:       req.ConversationID,
					UserID:   req.StateStoreConfig.UserID,
					Messages: []types.Message{},
				}
			}

			// Append new messages
			state.Messages = append(state.Messages, messages...)

			// Save back
			if saveErr := store.Save(ctx, state); saveErr != nil {
				return saveErr
			}
		}
	}

	return nil
}

func (m *MockTurnExecutor) ExecuteTurnStream(ctx context.Context, req turnexecutors.TurnRequest) (<-chan turnexecutors.MessageStreamChunk, error) {
	// Fallback to non-streaming for existing tests
	err := m.ExecuteTurn(ctx, req)
	if err != nil {
		ch := make(chan turnexecutors.MessageStreamChunk, 1)
		ch <- turnexecutors.MessageStreamChunk{Error: err}
		close(ch)
		return ch, nil
	}

	ch := make(chan turnexecutors.MessageStreamChunk, 1)
	ch <- turnexecutors.MessageStreamChunk{
		Messages:     []types.Message{}, // Messages in StateStore
		FinishReason: strPtr("stop"),
	}
	close(ch)
	return ch, nil
}

// MockProvider implements providers.Provider for testing
type MockProvider struct {
	id string
}

func (m *MockProvider) ID() string {
	return m.id
}

func (m *MockProvider) Predict(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
	costBreakdown := types.CostInfo{
		InputTokens:   10,
		OutputTokens:  20,
		InputCostUSD:  0.0001,
		OutputCostUSD: 0.0002,
		TotalCost:     0.0003,
	}
	return providers.PredictionResponse{
		Content:  "mock response",
		CostInfo: &costBreakdown,
	}, nil
}

func (m *MockProvider) ShouldIncludeRawOutput() bool {
	return false
}

func (m *MockProvider) Close() error {
	return nil
}

func (m *MockProvider) PredictStream(ctx context.Context, req providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
	return nil, nil
}

func (m *MockProvider) SupportsStreaming() bool {
	return false
}

func (m *MockProvider) CalculateCost(inputTokens, outputTokens, cachedTokens int) types.CostInfo {
	inputCostPer1K := 0.01
	outputCostPer1K := 0.01
	cachedCostPer1K := 0.005

	inputCost := float64(inputTokens-cachedTokens) / 1000.0 * inputCostPer1K
	cachedCost := float64(cachedTokens) / 1000.0 * cachedCostPer1K
	outputCost := float64(outputTokens) / 1000.0 * outputCostPer1K

	return types.CostInfo{
		InputTokens:   inputTokens - cachedTokens,
		OutputTokens:  outputTokens,
		CachedTokens:  cachedTokens,
		InputCostUSD:  inputCost,
		OutputCostUSD: outputCost,
		CachedCostUSD: cachedCost,
		TotalCost:     inputCost + cachedCost + outputCost,
	}
}

func TestNewDefaultConversationExecutor(t *testing.T) {
	scriptedExecutor := &MockTurnExecutor{}
	selfPlayExecutor := &MockTurnExecutor{}
	registry := selfplay.NewRegistry(nil, map[string]string{}, map[string]*config.UserPersonaPack{}, []config.SelfPlayRoleGroup{})
	promptRegistry := createTestPromptRegistry(t)

	executor := NewDefaultConversationExecutor(
		scriptedExecutor,
		selfPlayExecutor,
		registry,
		promptRegistry,
	)

	if executor == nil {
		t.Fatal("Expected non-nil executor")
	}

	// Verify executor is of the expected type (already returns ConversationExecutor)
	// Type check is satisfied by the function signature
}

// createTestPromptRegistry creates a prompt registry with a basic test prompt config
func createTestPromptRegistry(t *testing.T) *prompt.Registry {
	t.Helper()

	// Create memory repository
	memRepo := memory.NewMemoryPromptRepository()

	// Create a minimal prompt config
	config := &prompt.PromptConfig{
		APIVersion: "promptkit.altairalabs.ai/v1alpha1",
		Kind:       "PromptConfig",
		Metadata: metav1.ObjectMeta{
			Name: "support",
		},
		Spec: prompt.PromptSpec{
			TaskType:       "support",
			Version:        "v1.0.0",
			Description:    "Test support prompt",
			SystemTemplate: "You are a helpful test assistant.",
			Variables:      []prompt.VariableMetadata{},
		},
	}

	// Register the prompt config
	memRepo.RegisterPrompt("support", config)

	// Create registry with memory repository
	registry := prompt.NewRegistryWithRepository(memRepo)

	return registry
}

func TestExecuteConversation_BasicScriptedScenario(t *testing.T) {
	// Use default mock behavior (no custom executeFunc)
	scriptedExecutor := &MockTurnExecutor{}

	executor := NewDefaultConversationExecutor(
		scriptedExecutor,
		nil,
		nil,
		createTestPromptRegistry(t),
	)

	scenario := &config.Scenario{
		ID:       "test",
		TaskType: "support",
		Turns: []config.TurnDefinition{
			{
				Role:    "user",
				Content: "Hello, how are you?",
			},
		},
	}

	req := ConversationRequest{
		Region:   "us",
		Scenario: scenario,
		Provider: &MockProvider{id: "test"},
		Config: &config.Config{
			Defaults: config.Defaults{
				Verbose: false,
			},
		},
		StateStoreConfig: &StateStoreConfig{
			Store:  createTestStateStore(),
			UserID: "test-user",
		},
		ConversationID: "test-conv-basic",
	}

	result := executor.ExecuteConversation(context.Background(), req)

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if result.Error != "" {
		t.Errorf("Expected success, got error: %s", result.Error)
	}

	// Should have user + assistant = 2 messages
	// (System message is internal to prompt assembly, not part of conversation history)
	if len(result.Messages) != 2 {
		t.Errorf("Expected 2 messages (user, assistant), got %d", len(result.Messages))
	}

	if result.Messages[0].Role != "user" {
		t.Error("Expected first message to be user message")
	}

	if result.Messages[1].Role != "assistant" {
		t.Error("Expected second message to be assistant message")
	}

	if result.Cost.TotalCost <= 0 {
		t.Error("Expected positive total cost")
	}

	if scriptedExecutor.callCount != 1 {
		t.Errorf("Expected 1 executor call, got %d", scriptedExecutor.callCount)
	}
}

func TestExecuteConversation_MultipleScriptedTurns(t *testing.T) {
	// Use default mock behavior
	scriptedExecutor := &MockTurnExecutor{}

	executor := NewDefaultConversationExecutor(
		scriptedExecutor,
		nil,
		nil,
		createTestPromptRegistry(t),
	)

	scenario := &config.Scenario{
		ID:       "test",
		TaskType: "support",
		Turns: []config.TurnDefinition{
			{Role: "user", Content: "First message"},
			{Role: "user", Content: "Second message"},
			{Role: "user", Content: "Third message"},
		},
	}

	req := ConversationRequest{
		Region:   "us",
		Scenario: scenario,
		Provider: &MockProvider{id: "test"},
		Config: &config.Config{
			Defaults: config.Defaults{
				Verbose: false,
			},
		},
		StateStoreConfig: &StateStoreConfig{
			Store:  createTestStateStore(),
			UserID: "test-user",
		},
		ConversationID: "test-conv-multi-turn",
	}

	result := executor.ExecuteConversation(context.Background(), req)

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if result.Failed {
		t.Errorf("Expected success, got error: %s", result.Error)
	}

	// Should have 3 * (user + assistant) = 6 messages
	expectedMessages := 3 * 2
	if len(result.Messages) != expectedMessages {
		t.Errorf("Expected %d messages, got %d", expectedMessages, len(result.Messages))
	}

	if scriptedExecutor.callCount != 3 {
		t.Errorf("Expected 3 executor calls, got %d", scriptedExecutor.callCount)
	}
}

func TestExecuteConversation_WithToolCalls(t *testing.T) {
	scriptedExecutor := &MockTurnExecutor{
		executeFunc: func(ctx context.Context, req turnexecutors.TurnRequest) error {
			// For this test, create messages with tool calls and save directly to StateStore
			messages := []types.Message{
				{
					Role:    "user",
					Content: req.ScriptedContent,
				},
				{
					Role:    "assistant",
					Content: "Let me check the weather",
					ToolCalls: []types.MessageToolCall{
						{
							ID:   "call_1",
							Name: "get_weather",
							Args: []byte(`{"city":"San Francisco"}`),
						},
					},
					CostInfo: &types.CostInfo{
						InputTokens:   10,
						OutputTokens:  20,
						InputCostUSD:  0.0001,
						OutputCostUSD: 0.0002,
						TotalCost:     0.0003,
					},
				},
			}

			// Save to StateStore
			if req.StateStoreConfig != nil && req.StateStoreConfig.Store != nil && req.ConversationID != "" {
				store, ok := req.StateStoreConfig.Store.(statestore.Store)
				if ok {
					state, loadErr := store.Load(ctx, req.ConversationID)
					if loadErr != nil && !errors.Is(loadErr, statestore.ErrNotFound) {
						return loadErr
					}
					if state == nil {
						state = &statestore.ConversationState{
							ID:       req.ConversationID,
							UserID:   req.StateStoreConfig.UserID,
							Messages: []types.Message{},
						}
					}
					state.Messages = append(state.Messages, messages...)
					if saveErr := store.Save(ctx, state); saveErr != nil {
						return saveErr
					}
				}
			}
			return nil
		},
	}

	executor := NewDefaultConversationExecutor(
		scriptedExecutor,
		nil,
		nil,
		createTestPromptRegistry(t),
	)

	scenario := &config.Scenario{
		ID:       "test",
		TaskType: "support",
		Turns: []config.TurnDefinition{
			{Role: "user", Content: "What's the weather?"},
		},
	}

	req := ConversationRequest{
		Region:   "us",
		Scenario: scenario,
		Provider: &MockProvider{id: "test"},
		Config: &config.Config{
			Defaults: config.Defaults{
				Verbose: false,
			},
		},
		StateStoreConfig: &StateStoreConfig{
			Store:  createTestStateStore(),
			UserID: "test-user",
		},
		ConversationID: "test-conv-tool-calls",
	}

	result := executor.ExecuteConversation(context.Background(), req)

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if result.Failed {
		t.Errorf("Expected success, got error: %s", result.Error)
	}

	// Should have user + assistant = 2 messages (tool calls simplified in flat message model)
	expectedMessages := 2
	if len(result.Messages) != expectedMessages {
		t.Errorf("Expected %d messages, got %d", expectedMessages, len(result.Messages))
	}

	// Check tool stats
	if result.ToolStats == nil || len(result.ToolStats.ByTool) == 0 {
		t.Error("Expected tool stats to be populated")
	}

	if count, ok := result.ToolStats.ByTool["get_weather"]; !ok || count != 1 {
		t.Errorf("Expected get_weather to be called 1 time, got %d", count)
	}
}

func TestExecuteConversation_ExecutorError(t *testing.T) {
	scriptedExecutor := &MockTurnExecutor{
		executeFunc: func(ctx context.Context, req turnexecutors.TurnRequest) error {
			return errors.New("executor failed")
		},
	}

	executor := NewDefaultConversationExecutor(
		scriptedExecutor,
		nil,
		nil,
		createTestPromptRegistry(t),
	)

	scenario := &config.Scenario{
		ID:       "test",
		TaskType: "support",
		Turns: []config.TurnDefinition{
			{Role: "user", Content: "Hello"},
		},
	}

	req := ConversationRequest{
		Region:   "us",
		Scenario: scenario,
		Provider: &MockProvider{id: "test"},
		Config: &config.Config{
			Defaults: config.Defaults{
				Verbose: false,
			},
		},
	}

	result := executor.ExecuteConversation(context.Background(), req)

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if !result.Failed {
		t.Error("Expected failed result")
	}

	if result.Error == "" {
		t.Error("Expected error message")
	}
}

func TestExecuteConversation_ValidationFailurePreservesMessages(t *testing.T) {
	// Create a StateStore to capture messages
	store := createTestStateStore()

	// Create a mock executor that saves messages before returning error
	scriptedExecutor := &MockTurnExecutor{
		executeFunc: func(ctx context.Context, req turnexecutors.TurnRequest) error {
			// Simulate normal execution - save messages to StateStore
			messages := []types.Message{
				{
					Role:    "user",
					Content: "Test message with banned word: guarantee",
				},
				{
					Role:    "assistant",
					Content: "I guarantee this will trigger validation error",
					CostInfo: &types.CostInfo{
						InputTokens:   50,
						OutputTokens:  100,
						InputCostUSD:  0.0005,
						OutputCostUSD: 0.001,
						TotalCost:     0.0015,
					},
				},
			}

			// Save to StateStore
			if req.StateStoreConfig != nil && req.StateStoreConfig.Store != nil {
				state, loadErr := req.StateStoreConfig.Store.(statestore.Store).Load(ctx, req.ConversationID)
				if loadErr != nil && !errors.Is(loadErr, statestore.ErrNotFound) {
					return loadErr
				}
				if state == nil {
					state = &statestore.ConversationState{
						ID:       req.ConversationID,
						Messages: []types.Message{},
					}
				}

				state.Messages = append(state.Messages, messages...)

				if saveErr := req.StateStoreConfig.Store.(statestore.Store).Save(ctx, state); saveErr != nil {
					return saveErr
				}
			}

			// Return validation error AFTER saving messages
			return errors.New("validation failed (*validators.BannedWordsValidator): [guarantee]")
		},
	}

	executor := NewDefaultConversationExecutor(
		scriptedExecutor,
		nil,
		nil,
		createTestPromptRegistry(t),
	)

	scenario := &config.Scenario{
		ID:       "validation-test",
		TaskType: "support",
		Turns: []config.TurnDefinition{
			{Role: "user", Content: "Test message"},
		},
	}

	conversationID := "test-validation-conversation"

	req := ConversationRequest{
		Region:         "us",
		Scenario:       scenario,
		Provider:       &MockProvider{id: "test"},
		ConversationID: conversationID,
		Config: &config.Config{
			Defaults: config.Defaults{
				Verbose: false,
			},
		},
		StateStoreConfig: &StateStoreConfig{
			Store:  store,
			UserID: "test-user",
		},
	}

	result := executor.ExecuteConversation(context.Background(), req)

	// Verify the result is marked as failed
	if !result.Failed {
		t.Error("Expected result.Failed to be true")
	}

	if result.Error == "" {
		t.Error("Expected error message to be set")
	}

	if result.Error != "validation failed (*validators.BannedWordsValidator): [guarantee]" {
		t.Errorf("Expected validation error message, got: %s", result.Error)
	}

	// Verify messages were preserved
	if len(result.Messages) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(result.Messages))
	}

	if result.Messages[0].Role != "user" {
		t.Errorf("Expected first message role to be 'user', got: %s", result.Messages[0].Role)
	}

	if result.Messages[1].Role != "assistant" {
		t.Errorf("Expected second message role to be 'assistant', got: %s", result.Messages[1].Role)
	}

	if result.Messages[1].Content != "I guarantee this will trigger validation error" {
		t.Errorf("Expected assistant message content to be preserved, got: %s", result.Messages[1].Content)
	}

	// Verify cost information was preserved
	if result.Cost.TotalCost != 0.0015 {
		t.Errorf("Expected total cost 0.0015, got: %f", result.Cost.TotalCost)
	}

	if result.Cost.InputTokens != 50 {
		t.Errorf("Expected 50 input tokens, got: %d", result.Cost.InputTokens)
	}

	if result.Cost.OutputTokens != 100 {
		t.Errorf("Expected 100 output tokens, got: %d", result.Cost.OutputTokens)
	}
}

func TestExecuteConversation_ValidationFailureMultipleTurns(t *testing.T) {
	// Create a StateStore to capture messages
	store := createTestStateStore()

	callCount := 0

	// Create a mock executor that succeeds on first turn, fails on second
	scriptedExecutor := &MockTurnExecutor{
		executeFunc: func(ctx context.Context, req turnexecutors.TurnRequest) error {
			callCount++

			// Load existing state
			state, loadErr := req.StateStoreConfig.Store.(statestore.Store).Load(ctx, req.ConversationID)
			if loadErr != nil && !errors.Is(loadErr, statestore.ErrNotFound) {
				return loadErr
			}
			if state == nil {
				state = &statestore.ConversationState{
					ID:       req.ConversationID,
					Messages: []types.Message{},
				}
			}

			if callCount == 1 {
				// First turn succeeds
				messages := []types.Message{
					{
						Role:    "user",
						Content: "First message",
					},
					{
						Role:    "assistant",
						Content: "First response without banned words",
						CostInfo: &types.CostInfo{
							InputTokens:  10,
							OutputTokens: 20,
							TotalCost:    0.0001,
						},
					},
				}
				state.Messages = append(state.Messages, messages...)
				_ = req.StateStoreConfig.Store.(statestore.Store).Save(ctx, state) // Ignore error in test
				return nil
			} else {
				// Second turn fails validation but saves messages first
				messages := []types.Message{
					{
						Role:    "user",
						Content: "Second message",
					},
					{
						Role:    "assistant",
						Content: "I guarantee this fails",
						CostInfo: &types.CostInfo{
							InputTokens:  30,
							OutputTokens: 40,
							TotalCost:    0.0002,
						},
					},
				}
				state.Messages = append(state.Messages, messages...)
				_ = req.StateStoreConfig.Store.(statestore.Store).Save(ctx, state) // Ignore error in test
				return errors.New("validation failed (*validators.BannedWordsValidator): [guarantee]")
			}
		},
	}

	executor := NewDefaultConversationExecutor(
		scriptedExecutor,
		nil,
		nil,
		createTestPromptRegistry(t),
	)

	scenario := &config.Scenario{
		ID:       "multi-turn-validation-test",
		TaskType: "support",
		Turns: []config.TurnDefinition{
			{Role: "user", Content: "First message"},
			{Role: "user", Content: "Second message"},
		},
	}

	conversationID := "test-multi-turn-validation"

	req := ConversationRequest{
		Region:         "us",
		Scenario:       scenario,
		Provider:       &MockProvider{id: "test"},
		ConversationID: conversationID,
		Config: &config.Config{
			Defaults: config.Defaults{
				Verbose: false,
			},
		},
		StateStoreConfig: &StateStoreConfig{
			Store:  store,
			UserID: "test-user",
		},
	}

	result := executor.ExecuteConversation(context.Background(), req)

	// Verify the result is marked as failed
	if !result.Failed {
		t.Error("Expected result.Failed to be true")
	}

	if result.Error == "" {
		t.Error("Expected error message to be set")
	}

	// Verify ALL messages were preserved (from both turns)
	if len(result.Messages) != 4 {
		t.Fatalf("Expected 4 messages (2 turns), got %d", len(result.Messages))
	}

	// Verify first turn messages
	if result.Messages[0].Content != "First message" {
		t.Errorf("Expected first user message preserved, got: %s", result.Messages[0].Content)
	}

	if result.Messages[1].Content != "First response without banned words" {
		t.Errorf("Expected first assistant message preserved, got: %s", result.Messages[1].Content)
	}

	// Verify second turn messages (the one that failed)
	if result.Messages[2].Content != "Second message" {
		t.Errorf("Expected second user message preserved, got: %s", result.Messages[2].Content)
	}

	if result.Messages[3].Content != "I guarantee this fails" {
		t.Errorf("Expected second assistant message preserved, got: %s", result.Messages[3].Content)
	}

	// Verify total cost includes both turns
	expectedCost := 0.0001 + 0.0002
	tolerance := 0.000001
	if result.Cost.TotalCost < expectedCost-tolerance || result.Cost.TotalCost > expectedCost+tolerance {
		t.Errorf("Expected total cost %f, got: %f", expectedCost, result.Cost.TotalCost)
	}

	// Verify total tokens
	expectedInputTokens := 10 + 30
	expectedOutputTokens := 20 + 40
	if result.Cost.InputTokens != expectedInputTokens {
		t.Errorf("Expected %d input tokens, got: %d", expectedInputTokens, result.Cost.InputTokens)
	}
	if result.Cost.OutputTokens != expectedOutputTokens {
		t.Errorf("Expected %d output tokens, got: %d", expectedOutputTokens, result.Cost.OutputTokens)
	}
}
