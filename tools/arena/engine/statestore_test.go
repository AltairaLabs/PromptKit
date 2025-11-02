package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/pkg/config"
	arenastatestore "github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/turnexecutors"
)

// TestConversationExecutor_WithStateStore verifies StateStore integration
func TestConversationExecutor_WithStateStore(t *testing.T) {
	// Create a memory store
	store := statestore.NewMemoryStore()

	// Create state store config
	stateStoreConfig := &StateStoreConfig{
		Store:    store,
		UserID:   "test-user",
		Metadata: map[string]interface{}{"test": "metadata"},
	}

	// Create mock provider
	mockProvider := &testProvider{id: "test-provider"}

	// Create mock scenario
	scenario := &config.Scenario{
		ID:       "test-scenario",
		TaskType: "assistance",
		Turns: []config.TurnDefinition{
			{Role: "user", Content: "Hello"},
		},
	}

	// Create mock config
	cfg := &config.Config{
		Defaults: config.Defaults{
			Temperature: 0.7,
			MaxTokens:   100,
			Seed:        42,
		},
	}

	// Create mock executor that tracks calls
	executeCalled := false
	mockTurnExecutor := &mockTurnExecutor{
		executeFunc: func(ctx context.Context, req turnexecutors.TurnRequest) error {
			executeCalled = true

			// Verify StateStore config was passed through
			if req.StateStoreConfig == nil {
				t.Error("Expected StateStoreConfig to be passed to turn executor")
			}
			if req.ConversationID == "" {
				t.Error("Expected ConversationID to be passed to turn executor")
			}

			return nil
		},
	}

	// Create conversation executor with mock turn executor
	ce := &DefaultConversationExecutor{
		scriptedExecutor: mockTurnExecutor,
		selfPlayExecutor: nil,
		selfPlayRegistry: nil,
		promptRegistry:   nil,
	}

	// Create conversation request with StateStore
	req := ConversationRequest{
		Provider:         mockProvider,
		Scenario:         scenario,
		Config:           cfg,
		Region:           "us",
		RunID:            "test-run",
		StateStoreConfig: stateStoreConfig,
		ConversationID:   "test-run-test-scenario",
	}

	// Execute conversation
	result := ce.ExecuteConversation(context.Background(), req)

	// Verify result
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if !executeCalled {
		t.Error("Expected turn executor to be called")
	}
	if len(result.Messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(result.Messages))
	}
}

// TestConversationExecutor_WithoutStateStore verifies normal operation without StateStore
func TestConversationExecutor_WithoutStateStore(t *testing.T) {
	// Create mock provider
	mockProvider := &testProvider{id: "test-provider"}

	// Create mock scenario
	scenario := &config.Scenario{
		ID:       "test-scenario",
		TaskType: "assistance",
		Turns: []config.TurnDefinition{
			{Role: "user", Content: "Hello"},
		},
	}

	// Create mock config
	cfg := &config.Config{
		Defaults: config.Defaults{
			Temperature: 0.7,
			MaxTokens:   100,
			Seed:        42,
		},
	}

	// Create mock executor
	executeCalled := false
	mockTurnExecutor := &mockTurnExecutor{
		executeFunc: func(ctx context.Context, req turnexecutors.TurnRequest) error {
			executeCalled = true

			// Verify StateStore config is provided (defaults to MemoryStore)
			if req.StateStoreConfig == nil {
				t.Error("Expected StateStoreConfig to be provided (defaults to MemoryStore)")
			}

			return nil
		},
	}

	// Create conversation executor with mock turn executor
	ce := &DefaultConversationExecutor{
		scriptedExecutor: mockTurnExecutor,
		selfPlayExecutor: nil,
		selfPlayRegistry: nil,
		promptRegistry:   nil,
	}

	// Create conversation request with default MemoryStore
	store := statestore.NewMemoryStore()
	req := ConversationRequest{
		Provider: mockProvider,
		Scenario: scenario,
		Config:   cfg,
		Region:   "us",
		RunID:    "test-run",
		StateStoreConfig: &StateStoreConfig{
			Store:  store,
			UserID: "test-user",
		},
		ConversationID: "test-run-test-scenario",
	}

	// Execute conversation
	result := ce.ExecuteConversation(context.Background(), req)

	// Verify result
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if !executeCalled {
		t.Error("Expected turn executor to be called")
	}
	if len(result.Messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(result.Messages))
	}
}

// TestBuildStateStore verifies StateStore construction from config
func TestBuildStateStore(t *testing.T) {
	tests := []struct {
		name      string
		config    *config.Config
		wantError bool
		wantNil   bool
	}{
		{
			name:    "no state store configured",
			config:  &config.Config{},
			wantNil: false, // Now defaults to MemoryStore
		},
		{
			name: "memory store",
			config: &config.Config{
				StateStore: &config.StateStoreConfig{
					Type: "memory",
				},
			},
			wantNil: false,
		},
		{
			name: "memory store (default)",
			config: &config.Config{
				StateStore: &config.StateStoreConfig{
					Type: "",
				},
			},
			wantNil: false,
		},
		{
			name: "redis store without config",
			config: &config.Config{
				StateStore: &config.StateStoreConfig{
					Type: "redis",
				},
			},
			wantError: true,
		},
		{
			name: "unsupported store type",
			config: &config.Config{
				StateStore: &config.StateStoreConfig{
					Type: "postgresql",
				},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := buildStateStore(tt.config)

			if tt.wantError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if tt.wantNil {
				if store != nil {
					t.Error("Expected nil store")
				}
			} else {
				if store == nil {
					t.Error("Expected non-nil store")
				}
			}
		})
	}
}

// mockTurnExecutor is a mock implementation for testing
type mockTurnExecutor struct {
	executeFunc       func(ctx context.Context, req turnexecutors.TurnRequest) error
	executeStreamFunc func(ctx context.Context, req turnexecutors.TurnRequest) (<-chan turnexecutors.MessageStreamChunk, error)
}

func (m *mockTurnExecutor) ExecuteTurn(ctx context.Context, req turnexecutors.TurnRequest) error {
	var err error

	if m.executeFunc != nil {
		err = m.executeFunc(ctx, req)
		if err != nil {
			return err
		}
	}

	// Create mock messages
	messages := []types.Message{
		{Role: "user", Content: "mock user message"},
		{Role: "assistant", Content: "mock response"},
	}

	// Save messages to StateStore if configured
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

func (m *mockTurnExecutor) ExecuteTurnStream(ctx context.Context, req turnexecutors.TurnRequest) (<-chan turnexecutors.MessageStreamChunk, error) {
	if m.executeStreamFunc != nil {
		return m.executeStreamFunc(ctx, req)
	}
	ch := make(chan turnexecutors.MessageStreamChunk)
	close(ch)
	return ch, nil
}

// testProvider is a minimal test provider implementation
type testProvider struct {
	id string
}

func (p *testProvider) ID() string              { return p.id }
func (p *testProvider) Type() string            { return "test" }
func (p *testProvider) Model() string           { return "test-model" }
func (p *testProvider) SupportsStreaming() bool { return false }
func (p *testProvider) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	return providers.ChatResponse{Content: "test response"}, nil
}
func (p *testProvider) ChatStream(ctx context.Context, req providers.ChatRequest) (<-chan providers.StreamChunk, error) {
	ch := make(chan providers.StreamChunk)
	close(ch)
	return ch, nil
}
func (p *testProvider) Close() error                 { return nil }
func (p *testProvider) ShouldIncludeRawOutput() bool { return false }
func (p *testProvider) CalculateCost(inputTokens, outputTokens, cachedTokens int) types.CostInfo {
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

// TestEngine_WithStateStore verifies StateStore integration in Engine.ExecuteRuns
func TestEngine_WithStateStore(t *testing.T) {
	// Create config with StateStore
	cfg := &config.Config{
		StateStore: &config.StateStoreConfig{
			Type: "memory",
		},
		Defaults: config.Defaults{
			Temperature: 0.7,
			MaxTokens:   100,
			Seed:        42,
		},
		LoadedScenarios: map[string]*config.Scenario{
			"test-scenario": {
				ID:       "test-scenario",
				TaskType: "assistance",
				Turns: []config.TurnDefinition{
					{Role: "user", Content: "Hello"},
				},
			},
		},
		LoadedProviders: map[string]*config.Provider{
			"test-provider": {
				ID:    "test-provider",
				Type:  "test",
				Model: "test-model",
			},
		},
	}

	// Create provider registry
	providerRegistry := providers.NewRegistry()
	testProv := &testProvider{id: "test-provider"}
	providerRegistry.Register(testProv)

	// Track if StateStore config was passed
	stateStoreUsed := false
	mockExecutor := &mockTurnExecutor{
		executeFunc: func(ctx context.Context, req turnexecutors.TurnRequest) error {
			if req.StateStoreConfig != nil {
				stateStoreUsed = true
			}
			return nil
		},
	}

	conversationExecutor := &DefaultConversationExecutor{
		scriptedExecutor: mockExecutor,
		selfPlayExecutor: nil,
		selfPlayRegistry: nil,
		promptRegistry:   nil,
	}

	// Create engine (this will call buildStateStore)
	engine, err := NewEngine(cfg, providerRegistry, nil, nil, conversationExecutor)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	// Verify StateStore was created
	if engine.stateStore == nil {
		t.Error("Expected StateStore to be created from config")
	}

	// Generate run plan
	plan := &RunPlan{
		Combinations: []RunCombination{
			{
				Region:     "default",
				ScenarioID: "test-scenario",
				ProviderID: "test-provider",
			},
		},
	}

	// Execute runs
	runIDs, err := engine.ExecuteRuns(context.Background(), plan, 1)
	if err != nil {
		t.Fatalf("ExecuteRuns failed: %v", err)
	}

	if len(runIDs) != 1 {
		t.Fatalf("Expected 1 runID, got %d", len(runIDs))
	}

	// Verify StateStore was actually used
	if !stateStoreUsed {
		t.Error("Expected StateStore config to be passed to turn executor")
	}

	// Get the result from statestore
	arenaStore, ok := engine.GetStateStore().(*arenastatestore.ArenaStateStore)
	if !ok {
		t.Fatal("Failed to get ArenaStateStore")
	}

	result, err := arenaStore.GetRunResult(context.Background(), runIDs[0])
	if err != nil {
		t.Fatalf("Failed to get run result: %v", err)
	}

	// Verify result
	if result.Error != "" {
		t.Errorf("Unexpected error in result: %s", result.Error)
	}
	if len(result.Messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(result.Messages))
	}
}

// TestEngine_WithoutStateStore verifies Engine works without StateStore
func TestEngine_WithoutStateStore(t *testing.T) {
	// Create config WITHOUT StateStore
	cfg := &config.Config{
		Defaults: config.Defaults{
			Temperature: 0.7,
			MaxTokens:   100,
			Seed:        42,
		},
		LoadedScenarios: map[string]*config.Scenario{
			"test-scenario": {
				ID:       "test-scenario",
				TaskType: "assistance",
				Turns: []config.TurnDefinition{
					{Role: "user", Content: "Hello"},
				},
			},
		},
		LoadedProviders: map[string]*config.Provider{
			"test-provider": {
				ID:    "test-provider",
				Type:  "test",
				Model: "test-model",
			},
		},
	}

	// Create provider registry
	providerRegistry := providers.NewRegistry()
	testProv := &testProvider{id: "test-provider"}
	providerRegistry.Register(testProv)

	// Track if StateStore config was passed
	stateStoreUsed := false
	mockExecutor := &mockTurnExecutor{
		executeFunc: func(ctx context.Context, req turnexecutors.TurnRequest) error {
			if req.StateStoreConfig != nil {
				stateStoreUsed = true
			}
			return nil
		},
	}

	conversationExecutor := &DefaultConversationExecutor{
		scriptedExecutor: mockExecutor,
		selfPlayExecutor: nil,
		selfPlayRegistry: nil,
		promptRegistry:   nil,
	}

	// Create engine (no StateStore in config, should default to MemoryStore)
	engine, err := NewEngine(cfg, providerRegistry, nil, nil, conversationExecutor)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	// Verify StateStore was created (defaults to MemoryStore)
	if engine.stateStore == nil {
		t.Error("Expected StateStore to be created (defaults to MemoryStore)")
	}

	// Generate run plan
	plan := &RunPlan{
		Combinations: []RunCombination{
			{
				Region:     "default",
				ScenarioID: "test-scenario",
				ProviderID: "test-provider",
			},
		},
	}

	// Execute runs
	runIDs, err := engine.ExecuteRuns(context.Background(), plan, 1)
	if err != nil {
		t.Fatalf("ExecuteRuns failed: %v", err)
	}

	if len(runIDs) != 1 {
		t.Fatalf("Expected 1 runID, got %d", len(runIDs))
	}

	// Verify StateStore WAS used (always used now, defaults to MemoryStore)
	if !stateStoreUsed {
		t.Error("Expected StateStore config to be passed (defaults to MemoryStore)")
	}

	// Get the result from statestore
	arenaStore, ok := engine.GetStateStore().(*arenastatestore.ArenaStateStore)
	if !ok {
		t.Fatal("Failed to get ArenaStateStore")
	}

	result, err := arenaStore.GetRunResult(context.Background(), runIDs[0])
	if err != nil {
		t.Fatalf("Failed to get run result: %v", err)
	}

	// Verify result
	if result.Error != "" {
		t.Errorf("Unexpected error in result: %s", result.Error)
	}
	if len(result.Messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(result.Messages))
	}
}
