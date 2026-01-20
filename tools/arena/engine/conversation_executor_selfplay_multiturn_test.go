package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/selfplay"
	"github.com/AltairaLabs/PromptKit/tools/arena/turnexecutors"
)

// TestExecuteConversation_SelfPlayMultiTurn tests that self-play turns execute multiple times
// This ensures the `turns: N` field is respected for self-play scenarios (e.g., red-team testing)
func TestExecuteConversation_SelfPlayMultiTurn(t *testing.T) {
	// Track execution counts
	scriptedCallCount := 0
	selfPlayCallCount := 0

	scriptedExecutor := &MockTurnExecutor{
		executeFunc: func(ctx context.Context, req turnexecutors.TurnRequest) error {
			scriptedCallCount++

			messages := []types.Message{
				{
					Role:    "user",
					Content: req.ScriptedContent,
				},
				{
					Role:    "assistant",
					Content: "Response to: " + req.ScriptedContent,
					CostInfo: &types.CostInfo{
						InputTokens:  10,
						OutputTokens: 20,
						TotalCost:    0.0003,
					},
					LatencyMs: 100,
				},
			}

			return saveMessagesToStateStore(ctx, req, messages)
		},
	}

	selfPlayExecutor := &MockTurnExecutor{
		executeFunc: func(ctx context.Context, req turnexecutors.TurnRequest) error {
			selfPlayCallCount++

			// Each self-play turn creates a user message (from persona) and assistant response
			messages := []types.Message{
				{
					Role:    "user",
					Content: "Self-play question " + string(rune('A'+selfPlayCallCount-1)),
					Meta: map[string]interface{}{
						"persona": "curious-learner",
					},
				},
				{
					Role:    "assistant",
					Content: "Self-play answer " + string(rune('A'+selfPlayCallCount-1)),
					CostInfo: &types.CostInfo{
						InputTokens:  15,
						OutputTokens: 25,
						TotalCost:    0.0004,
					},
					LatencyMs: 120,
				},
			}

			return saveMessagesToStateStore(ctx, req, messages)
		},
	}

	// Set up self-play registry
	selfPlayRegistry := createTestSelfPlayRegistry(t)

	executor := NewDefaultConversationExecutor(
		scriptedExecutor,
		selfPlayExecutor,
		selfPlayRegistry,
		createTestPromptRegistry(t),
	)

	// Scenario with initial user turn, then 5 self-play turns
	scenario := &config.Scenario{
		ID:       "self-play-multi-turn",
		TaskType: "assistant",
		Turns: []config.TurnDefinition{
			{
				Role:    "user",
				Content: "Let's discuss security.",
			},
			{
				Role:    "attacker", // Registered self-play role
				Persona: "curious-learner",
				Turns:   5, // CRITICAL: Execute 5 self-play turns
			},
		},
	}

	req := ConversationRequest{
		Region:   "default",
		Scenario: scenario,
		Provider: &MockProvider{id: "openai-gpt4o-mini"},
		Config: &config.Config{
			Defaults: config.Defaults{
				Verbose:     false,
				Temperature: 0.7,
				MaxTokens:   1000,
			},
		},
		StateStoreConfig: &StateStoreConfig{
			Store:  createTestStateStore(),
			UserID: "test-user",
		},
		ConversationID: "test-self-play-multi",
	}

	result := executor.ExecuteConversation(context.Background(), req)

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if result.Failed {
		t.Errorf("Expected success, got error: %s", result.Error)
	}

	// CRITICAL: Scripted executor should be called once (initial turn)
	if scriptedCallCount != 1 {
		t.Errorf("CRITICAL: Expected 1 scripted turn, got %d", scriptedCallCount)
	}

	// CRITICAL: Self-play executor should be called 5 times (turns: 5)
	if selfPlayCallCount != 5 {
		t.Errorf("CRITICAL BUG: Expected 5 self-play turns, got %d", selfPlayCallCount)
		t.Errorf("This indicates the `turns: 5` field is not being respected!")
	}

	// Expected messages:
	// - 1 initial user + assistant (2 messages)
	// - 5 self-play rounds × 2 messages (user + assistant) = 10 messages
	// Total: 12 messages
	expectedMessages := 2 + (5 * 2)
	if len(result.Messages) != expectedMessages {
		t.Errorf("Expected %d messages, got %d", expectedMessages, len(result.Messages))
		for i, msg := range result.Messages {
			t.Logf("Message %d: role=%s, content=%q", i, msg.Role, msg.Content)
		}
	}

	// Verify each self-play message has proper metadata
	selfPlayUserMessages := 0
	for _, msg := range result.Messages {
		if msg.Role == "user" && msg.Meta != nil {
			if persona, ok := msg.Meta["persona"]; ok && persona == "curious-learner" {
				selfPlayUserMessages++
			}
		}
	}

	if selfPlayUserMessages != 5 {
		t.Errorf("Expected 5 self-play user messages with persona metadata, got %d", selfPlayUserMessages)
	}
}

// TestExecuteConversation_SelfPlayMultiTurnStreaming tests multi-turn self-play with streaming
func TestExecuteConversation_SelfPlayMultiTurnStreaming(t *testing.T) {
	selfPlayCallCount := 0

	scriptedExecutor := &MockTurnExecutor{
		executeFunc: func(ctx context.Context, req turnexecutors.TurnRequest) error {
			messages := []types.Message{
				{Role: "user", Content: req.ScriptedContent},
				{Role: "assistant", Content: "Response", LatencyMs: 100},
			}
			return saveMessagesToStateStore(ctx, req, messages)
		},
	}

	selfPlayExecutor := &MockTurnExecutor{
		executeFunc: func(ctx context.Context, req turnexecutors.TurnRequest) error {
			selfPlayCallCount++
			messages := []types.Message{
				{
					Role:      "user",
					Content:   "Question " + string(rune('A'+selfPlayCallCount-1)),
					Meta:      map[string]interface{}{"persona": "attacker"},
					LatencyMs: 110,
				},
				{
					Role:      "assistant",
					Content:   "Answer " + string(rune('A'+selfPlayCallCount-1)),
					LatencyMs: 130,
				},
			}
			return saveMessagesToStateStore(ctx, req, messages)
		},
	}

	selfPlayRegistry := createTestSelfPlayRegistry(t)

	executor := NewDefaultConversationExecutor(
		scriptedExecutor,
		selfPlayExecutor,
		selfPlayRegistry,
		createTestPromptRegistry(t),
	)

	scenario := &config.Scenario{
		ID:       "self-play-streaming",
		TaskType: "assistant",
		Turns: []config.TurnDefinition{
			{Role: "user", Content: "Start conversation."},
			{Role: "attacker", Persona: "attacker", Turns: 3, Streaming: boolPtr(true)},
		},
	}

	req := ConversationRequest{
		Region:           "default",
		Scenario:         scenario,
		Provider:         &MockProvider{id: "openai-gpt4o-mini"},
		Config:           &config.Config{},
		StateStoreConfig: &StateStoreConfig{Store: createTestStateStore(), UserID: "test-user"},
		ConversationID:   "test-self-play-streaming",
	}

	result := executor.ExecuteConversation(context.Background(), req)

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if result.Failed {
		t.Errorf("Expected success, got error: %s", result.Error)
	}

	// Should execute 3 self-play turns even with streaming enabled
	if selfPlayCallCount != 3 {
		t.Errorf("CRITICAL BUG: Expected 3 self-play turns with streaming, got %d", selfPlayCallCount)
	}

	// 1 initial + 3 self-play = 8 messages total
	expectedMessages := 2 + (3 * 2)
	if len(result.Messages) != expectedMessages {
		t.Errorf("Expected %d messages, got %d", expectedMessages, len(result.Messages))
	}
}

// TestExecuteConversation_ZeroTurnsSelfPlay tests that turns: 0 defaults to 1 execution
func TestExecuteConversation_ZeroTurnsSelfPlay(t *testing.T) {
	selfPlayCallCount := 0

	scriptedExecutor := &MockTurnExecutor{
		executeFunc: func(ctx context.Context, req turnexecutors.TurnRequest) error {
			messages := []types.Message{
				{Role: "user", Content: req.ScriptedContent},
				{Role: "assistant", Content: "Response"},
			}
			return saveMessagesToStateStore(ctx, req, messages)
		},
	}

	selfPlayExecutor := &MockTurnExecutor{
		executeFunc: func(ctx context.Context, req turnexecutors.TurnRequest) error {
			selfPlayCallCount++
			messages := []types.Message{
				{Role: "user", Content: "Self-play question"},
				{Role: "assistant", Content: "Self-play answer"},
			}
			return saveMessagesToStateStore(ctx, req, messages)
		},
	}

	selfPlayRegistry := createTestSelfPlayRegistry(t)

	executor := NewDefaultConversationExecutor(
		scriptedExecutor,
		selfPlayExecutor,
		selfPlayRegistry,
		createTestPromptRegistry(t),
	)

	scenario := &config.Scenario{
		ID:       "zero-turns",
		TaskType: "assistant",
		Turns: []config.TurnDefinition{
			{Role: "user", Content: "Hello"},
			{Role: "attacker", Persona: "test", Turns: 0}, // turns: 0 should default to 1
		},
	}

	req := ConversationRequest{
		Region:           "default",
		Scenario:         scenario,
		Provider:         &MockProvider{id: "openai-gpt4o-mini"},
		Config:           &config.Config{},
		StateStoreConfig: &StateStoreConfig{Store: createTestStateStore(), UserID: "test-user"},
		ConversationID:   "test-zero-turns",
	}

	result := executor.ExecuteConversation(context.Background(), req)

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if result.Failed {
		t.Errorf("Expected success, got error: %s", result.Error)
	}

	// turns: 0 should default to 1 execution
	if selfPlayCallCount != 1 {
		t.Errorf("Expected turns: 0 to default to 1 execution, got %d", selfPlayCallCount)
	}
}

// Helper function to save messages to state store
func saveMessagesToStateStore(ctx context.Context, req turnexecutors.TurnRequest, messages []types.Message) error {
	if req.StateStoreConfig == nil || req.StateStoreConfig.Store == nil || req.ConversationID == "" {
		return nil
	}

	store, ok := req.StateStoreConfig.Store.(statestore.Store)
	if !ok {
		return nil
	}

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
	return store.Save(ctx, state)
}

func boolPtr(b bool) *bool {
	return &b
}

func ptrFloat32(f float32) *float32 {
	return &f
}

// createTestSelfPlayRegistry creates a minimal self-play registry for testing
func createTestSelfPlayRegistry(t *testing.T) *selfplay.Registry {
	t.Helper()

	// Create a mock provider registry
	providerReg := providers.NewRegistry()
	mockProvider, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID:    "mock-selfplay",
		Type:  "mock",
		Model: "mock-model",
		Defaults: providers.ProviderDefaults{
			Temperature: 0.7,
			MaxTokens:   1000,
		},
	})
	if err != nil {
		t.Fatalf("Failed to create mock provider: %v", err)
	}
	providerReg.Register(mockProvider)

	// Configure role mappings and personas
	roleMap := map[string]string{
		"attacker": "mock-selfplay",
	}

	personas := map[string]*config.UserPersonaPack{
		"curious-learner": {
			ID:           "curious-learner",
			SystemPrompt: "You are a curious learner asking questions.",
			Defaults: config.PersonaDefaults{
				Temperature: ptrFloat32(0.7),
			},
		},
		"attacker": {
			ID:           "attacker",
			SystemPrompt: "You are testing security.",
			Defaults: config.PersonaDefaults{
				Temperature: ptrFloat32(0.8),
			},
		},
		"test": {
			ID:           "test",
			SystemPrompt: "You are a test persona.",
			Defaults: config.PersonaDefaults{
				Temperature: ptrFloat32(0.7),
			},
		},
	}

	roles := []config.SelfPlayRoleGroup{
		{ID: "attacker", Provider: "mock-selfplay"},
	}

	return selfplay.NewRegistry(providerReg, roleMap, personas, roles)
}
