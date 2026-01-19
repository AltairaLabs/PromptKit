package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	runtimestore "github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

func TestEngine_ExecuteRun_InvalidStateStore(t *testing.T) {
	ctx := context.Background()
	// Create engine with non-ArenaStateStore
	e := &Engine{
		config:               &config.Config{},
		scenarios:            make(map[string]*config.Scenario),
		evals:                make(map[string]*config.Eval),
		providers:            make(map[string]*config.Provider),
		providerRegistry:     providers.NewRegistry(),
		conversationExecutor: nil,                      // Not needed for this error path
		stateStore:           &mockInvalidStateStore{}, // Invalid type
	}

	combo := RunCombination{
		ScenarioID: "test-scenario",
		ProviderID: "test-provider",
		Region:     "default",
	}

	_, err := e.executeRun(ctx, combo)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "statestore is not ArenaStateStore")
}

func TestEngine_ExecuteRun_ScenarioNotFound(t *testing.T) {
	ctx := context.Background()
	e := &Engine{
		config:           &config.Config{},
		scenarios:        make(map[string]*config.Scenario), // Empty - scenario not found
		evals:            make(map[string]*config.Eval),
		providers:        make(map[string]*config.Provider),
		providerRegistry: providers.NewRegistry(),
		stateStore:       statestore.NewArenaStateStore(),
		eventBus:         events.NewEventBus(),
	}

	combo := RunCombination{
		ScenarioID: "missing-scenario",
		ProviderID: "test-provider",
		Region:     "default",
	}

	runID, err := e.executeRun(ctx, combo)
	require.NoError(t, err) // Error is saved, not returned
	assert.NotEmpty(t, runID)

	// Verify error was saved
	result, err := e.stateStore.(*statestore.ArenaStateStore).GetRunResult(ctx, runID)
	require.NoError(t, err)
	assert.Contains(t, result.Error, "scenario not found: missing-scenario")
}

func TestEngine_ExecuteRun_ProviderNotFound(t *testing.T) {
	ctx := context.Background()
	e := &Engine{
		config: &config.Config{},
		scenarios: map[string]*config.Scenario{
			"test-scenario": {},
		},
		evals:            make(map[string]*config.Eval),
		providers:        make(map[string]*config.Provider),
		providerRegistry: providers.NewRegistry(), // Empty - provider not found
		stateStore:       statestore.NewArenaStateStore(),
		eventBus:         events.NewEventBus(),
	}

	combo := RunCombination{
		ScenarioID: "test-scenario",
		ProviderID: "missing-provider",
		Region:     "default",
	}

	runID, err := e.executeRun(ctx, combo)
	require.NoError(t, err)
	assert.NotEmpty(t, runID)

	// Verify error was saved
	result, err := e.stateStore.(*statestore.ArenaStateStore).GetRunResult(ctx, runID)
	require.NoError(t, err)
	assert.Contains(t, result.Error, "provider not found: missing-provider")
}

func TestEngine_ExecuteRun_EvalNotFound(t *testing.T) {
	ctx := context.Background()
	e := &Engine{
		config:    &config.Config{},
		scenarios: make(map[string]*config.Scenario),
		evals:     make(map[string]*config.Eval), // Empty - eval not found
		providers: make(map[string]*config.Provider),
		// No conversation executor needed for this error path
		providerRegistry: providers.NewRegistry(),
		stateStore:       statestore.NewArenaStateStore(),
		eventBus:         events.NewEventBus(),
	}

	combo := RunCombination{
		EvalID: "missing-eval",
	}

	runID, err := e.executeRun(ctx, combo)
	require.NoError(t, err)
	assert.NotEmpty(t, runID)
	assert.Contains(t, runID, "eval")

	// Verify error was saved
	result, err := e.stateStore.(*statestore.ArenaStateStore).GetRunResult(ctx, runID)
	require.NoError(t, err)
	assert.Contains(t, result.Error, "eval not found: missing-eval")
}

func TestEngine_GenerateScenarioCombinations_ScenarioNotFound(t *testing.T) {
	e := &Engine{
		scenarios: make(map[string]*config.Scenario), // Empty
	}

	_, err := e.generateScenarioCombinations("default", "missing-scenario", []string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scenario missing-scenario not found")
}

func TestEngine_GenerateScenarioCombinations_Success(t *testing.T) {
	e := &Engine{
		config: &config.Config{},
		scenarios: map[string]*config.Scenario{
			"test-scenario": {
				Providers: []string{"openai", "anthropic"},
			},
		},
		providers:        make(map[string]*config.Provider),
		providerRegistry: providers.NewRegistry(),
	}

	combos, err := e.generateScenarioCombinations("us-west-1", "test-scenario", []string{})
	require.NoError(t, err)
	assert.Len(t, combos, 2)

	// Verify combinations
	assert.Equal(t, "us-west-1", combos[0].Region)
	assert.Equal(t, "test-scenario", combos[0].ScenarioID)
	assert.Contains(t, []string{"openai", "anthropic"}, combos[0].ProviderID)

	assert.Equal(t, "us-west-1", combos[1].Region)
	assert.Equal(t, "test-scenario", combos[1].ScenarioID)
	assert.Contains(t, []string{"openai", "anthropic"}, combos[1].ProviderID)
}

func TestEngine_GenerateCombinations_Error(t *testing.T) {
	e := &Engine{
		config: &config.Config{},
		scenarios: map[string]*config.Scenario{
			"scenario-1": {Providers: []string{"openai"}},
		},
		providers:        make(map[string]*config.Provider),
		providerRegistry: providers.NewRegistry(),
	}

	t.Run("error on missing scenario", func(t *testing.T) {
		regions := []string{"default"}
		scenarios := []string{"missing"}

		_, err := e.generateCombinations(regions, scenarios, []string{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "scenario missing not found")
	})
}

func TestEngine_GenerateRunPlan_Scenarios(t *testing.T) {
	e := &Engine{
		config: &config.Config{},
		scenarios: map[string]*config.Scenario{
			"scenario-1": {Providers: []string{"openai"}},
		},
		evals:            make(map[string]*config.Eval),
		providers:        make(map[string]*config.Provider),
		providerRegistry: providers.NewRegistry(),
	}

	plan, err := e.GenerateRunPlan(
		[]string{"us-west-1"},
		[]string{},
		[]string{"scenario-1"},
		[]string{}, // No evals
	)

	require.NoError(t, err)
	assert.Len(t, plan.Combinations, 1)
	assert.Equal(t, "scenario-1", plan.Combinations[0].ScenarioID)
	assert.Equal(t, "openai", plan.Combinations[0].ProviderID)
	assert.Equal(t, "us-west-1", plan.Combinations[0].Region)
}

func TestEngine_GenerateRunPlan_Evals(t *testing.T) {
	e := &Engine{
		config:    &config.Config{},
		scenarios: make(map[string]*config.Scenario),
		evals: map[string]*config.Eval{
			"eval-1": {},
			"eval-2": {},
		},
		providers:        make(map[string]*config.Provider),
		providerRegistry: providers.NewRegistry(),
	}

	t.Run("generates eval plan when eval filter provided", func(t *testing.T) {
		plan, err := e.GenerateRunPlan(
			[]string{},
			[]string{},
			[]string{},
			[]string{"eval-1"}, // Eval filter
		)

		require.NoError(t, err)
		assert.Len(t, plan.Combinations, 1)
		assert.Equal(t, "eval-1", plan.Combinations[0].EvalID)
		assert.Empty(t, plan.Combinations[0].ProviderID)
		assert.Empty(t, plan.Combinations[0].Region)
	})

	t.Run("generates eval plan when only evals exist", func(t *testing.T) {
		plan, err := e.GenerateRunPlan(
			[]string{},
			[]string{},
			[]string{}, // No scenario filter
			[]string{}, // No eval filter
		)

		require.NoError(t, err)
		assert.Len(t, plan.Combinations, 2) // All evals
	})
}

// mockInvalidStateStore is a non-ArenaStateStore implementation for testing error paths
type mockInvalidStateStore struct{}

func (m *mockInvalidStateStore) Save(ctx context.Context, state *runtimestore.ConversationState) error {
	return nil
}

func (m *mockInvalidStateStore) Load(ctx context.Context, id string) (*runtimestore.ConversationState, error) {
	return nil, nil
}

func (m *mockInvalidStateStore) Fork(ctx context.Context, baseID, newID string) error {
	return nil
}

func TestEvalConversationExecutor_LoadRecording_NilAdapter(t *testing.T) {
	executor := &EvalConversationExecutor{
		adapterRegistry: nil, // Nil registry
	}

	req := &ConversationRequest{
		Eval: &config.Eval{
			Recording: config.RecordingSource{
				Path: "test.json",
				Type: "arena",
			},
		},
	}

	_, _, err := executor.loadRecording(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "adapter registry not configured")
}

func TestEvalConversationExecutor_ValidateEvalConfig(t *testing.T) {
	executor := &EvalConversationExecutor{}

	t.Run("valid config", func(t *testing.T) {
		eval := &config.Eval{
			ID: "test-eval",
			Recording: config.RecordingSource{
				Path: "test.json",
			},
		}
		err := executor.validateEvalConfig(eval)
		assert.NoError(t, err)
	})

	t.Run("nil eval", func(t *testing.T) {
		err := executor.validateEvalConfig(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "eval configuration is required")
	})

	t.Run("empty recording path", func(t *testing.T) {
		eval := &config.Eval{
			ID:        "test-eval",
			Recording: config.RecordingSource{},
		}
		err := executor.validateEvalConfig(eval)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "recording path is required")
	})
}
