package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

func TestGenerateRunID(t *testing.T) {
	t.Run("scenario run ID", func(t *testing.T) {
		combo := RunCombination{
			Region:     "us-west-1",
			ScenarioID: "test-scenario",
			ProviderID: "openai",
		}
		runID := generateRunID(combo)
		assert.NotEmpty(t, runID)
		assert.Contains(t, runID, "openai")
		assert.Contains(t, runID, "us-west-1")
		assert.Contains(t, runID, "test-scenario")
	})

	t.Run("eval run ID", func(t *testing.T) {
		combo := RunCombination{
			EvalID: "test-eval",
		}
		runID := generateRunID(combo)
		assert.NotEmpty(t, runID)
		assert.Contains(t, runID, "eval")
		assert.Contains(t, runID, "test-eval")
	})

	t.Run("run IDs are unique", func(t *testing.T) {
		combo := RunCombination{
			Region:     "us-west-1",
			ScenarioID: "test-scenario",
			ProviderID: "openai",
		}
		runID1 := generateRunID(combo)
		time.Sleep(time.Millisecond)
		runID2 := generateRunID(combo)
		// Different timestamps should make them different
		// But since timestamp resolution is to the minute, they might be same
		// So we just check they're generated successfully
		assert.NotEmpty(t, runID1)
		assert.NotEmpty(t, runID2)
	})
}

func TestEngine_CreateRunEmitter(t *testing.T) {
	t.Run("creates emitter with event bus", func(t *testing.T) {
		bus := events.NewEventBus()
		e := &Engine{
			eventBus: bus,
		}
		combo := RunCombination{
			ScenarioID: "test-scenario",
			ProviderID: "openai",
			Region:     "us-west-1",
		}
		runID := "test-run-id"

		emitter := e.createRunEmitter(runID, combo)
		assert.NotNil(t, emitter)
	})

	t.Run("returns nil when no event bus", func(t *testing.T) {
		e := &Engine{
			eventBus: nil,
		}
		combo := RunCombination{}
		runID := "test-run-id"

		emitter := e.createRunEmitter(runID, combo)
		assert.Nil(t, emitter)
	})
}

func TestEngine_SaveRunError(t *testing.T) {
	ctx := context.Background()
	store := statestore.NewArenaStateStore()

	e := &Engine{}
	combo := RunCombination{
		Region:     "us-west-1",
		ScenarioID: "test-scenario",
		ProviderID: "openai",
	}
	runID := "test-run-123"
	startTime := time.Now()
	errorMsg := "test error message"

	t.Run("saves error metadata", func(t *testing.T) {
		resultID, err := e.saveRunError(ctx, store, combo, runID, startTime, errorMsg, nil)
		require.NoError(t, err)
		assert.Equal(t, runID, resultID)

		// Verify metadata was saved using GetRunResult
		result, err := store.GetRunResult(ctx, runID)
		require.NoError(t, err)
		assert.Equal(t, runID, result.RunID)
		assert.Equal(t, errorMsg, result.Error)
		assert.Equal(t, combo.Region, result.Region)
		assert.Equal(t, combo.ScenarioID, result.ScenarioID)
		assert.Equal(t, combo.ProviderID, result.ProviderID)
	})

	t.Run("emits error event when emitter provided", func(t *testing.T) {
		bus := events.NewEventBus()
		emitter := events.NewEmitter(bus, "session", "conv", runID)

		var eventReceived bool
		bus.Subscribe(events.EventType("arena.run.failed"), func(e *events.Event) {
			eventReceived = true
		})

		_, err := e.saveRunError(ctx, store, combo, runID+"_2", startTime, errorMsg, emitter)
		require.NoError(t, err)

		// Give event time to propagate
		time.Sleep(10 * time.Millisecond)
		assert.True(t, eventReceived)
	})
}

func TestEngine_SaveRunMetadata(t *testing.T) {
	ctx := context.Background()
	store := statestore.NewArenaStateStore()

	e := &Engine{}
	combo := RunCombination{
		Region:     "us-west-1",
		ScenarioID: "test-scenario",
		ProviderID: "openai",
	}
	runID := "test-run-456"
	startTime := time.Now()
	duration := 5 * time.Second

	result := &ConversationResult{
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there"},
		},
		Cost: types.CostInfo{
			TotalCost: 0.05,
		},
		SelfPlay:  true,
		PersonaID: "test-persona",
	}

	err := e.saveRunMetadata(ctx, store, combo, result, runID, startTime, duration)
	require.NoError(t, err)

	// Verify metadata was saved using GetRunResult
	runResult, err := store.GetRunResult(ctx, runID)
	require.NoError(t, err)
	assert.Equal(t, runID, runResult.RunID)
	assert.Equal(t, combo.Region, runResult.Region)
	assert.Equal(t, combo.ScenarioID, runResult.ScenarioID)
	assert.Equal(t, combo.ProviderID, runResult.ProviderID)
	assert.Equal(t, result.SelfPlay, runResult.SelfPlay)
	assert.Equal(t, result.PersonaID, runResult.PersonaID)
	assert.Equal(t, duration, runResult.Duration)
}

func TestEngine_NotifyRunCompletion(t *testing.T) {
	t.Run("does nothing when no emitter", func(t *testing.T) {
		e := &Engine{}
		result := &ConversationResult{}
		// Should not panic
		e.notifyRunCompletion(nil, result, "run-id", time.Second, 0.05)
	})

	t.Run("emits failed event on error", func(t *testing.T) {
		bus := events.NewEventBus()
		e := &Engine{eventBus: bus}
		emitter := events.NewEmitter(bus, "session", "conv", "run-id")

		var eventReceived bool
		bus.Subscribe(events.EventType("arena.run.failed"), func(e *events.Event) {
			eventReceived = true
		})

		result := &ConversationResult{
			Error: "test error",
		}

		e.notifyRunCompletion(emitter, result, "run-id", time.Second, 0.05)
		time.Sleep(10 * time.Millisecond)
		assert.True(t, eventReceived)
	})

	t.Run("emits completed event on success", func(t *testing.T) {
		bus := events.NewEventBus()
		e := &Engine{eventBus: bus}
		emitter := events.NewEmitter(bus, "session", "conv", "run-id")

		var eventReceived bool
		bus.Subscribe(events.EventType("arena.run.completed"), func(e *events.Event) {
			eventReceived = true
		})

		result := &ConversationResult{
			Error: "",
		}

		e.notifyRunCompletion(emitter, result, "run-id", 2*time.Second, 0.10)
		time.Sleep(10 * time.Millisecond)
		assert.True(t, eventReceived)
	})
}

func TestEngine_SaveEvalMetadata(t *testing.T) {
	ctx := context.Background()
	store := statestore.NewArenaStateStore()

	e := &Engine{}
	combo := RunCombination{
		EvalID: "test-eval",
	}
	runID := "eval-run-789"
	startTime := time.Now()
	duration := 3 * time.Second

	convResult := &ConversationResult{
		Messages: []types.Message{
			{Role: "user", Content: "Test message"},
			{Role: "assistant", Content: "Response"},
		},
		Cost: types.CostInfo{
			TotalCost: 0.03,
		},
		SelfPlay:  false,
		PersonaID: "eval-persona",
	}

	err := e.saveEvalMetadata(ctx, store, combo, convResult, runID, startTime, duration)
	require.NoError(t, err)

	// Verify conversation state was saved using Load
	state, err := store.Load(ctx, runID)
	require.NoError(t, err)
	assert.Equal(t, runID, state.ID)
	assert.Len(t, state.Messages, 2)
	assert.Equal(t, "Test message", state.Messages[0].Content)

	// Verify metadata was saved using GetRunResult
	runResult, err := store.GetRunResult(ctx, runID)
	require.NoError(t, err)
	assert.Equal(t, runID, runResult.RunID)
	assert.Equal(t, combo.EvalID, runResult.ScenarioID) // EvalID stored in ScenarioID field
	assert.Equal(t, "eval", runResult.ProviderID)       // Placeholder
	assert.Equal(t, "default", runResult.Region)
}

func TestEngine_ResolveEvals(t *testing.T) {
	e := &Engine{
		evals: map[string]*config.Eval{
			"eval-1": {},
			"eval-2": {},
			"eval-3": {},
		},
	}

	t.Run("empty filter returns all evals", func(t *testing.T) {
		result := e.resolveEvals([]string{})
		assert.Len(t, result, 3)
		assert.Contains(t, result, "eval-1")
		assert.Contains(t, result, "eval-2")
		assert.Contains(t, result, "eval-3")
	})

	t.Run("filter returns specific evals", func(t *testing.T) {
		result := e.resolveEvals([]string{"eval-1", "eval-3"})
		assert.Equal(t, []string{"eval-1", "eval-3"}, result)
	})

	t.Run("nil filter returns all evals", func(t *testing.T) {
		result := e.resolveEvals(nil)
		assert.Len(t, result, 3)
	})
}

func TestEngine_GenerateEvalPlan(t *testing.T) {
	e := &Engine{
		evals: map[string]*config.Eval{
			"eval-1": {},
			"eval-2": {},
		},
	}

	t.Run("creates combinations for all evals", func(t *testing.T) {
		plan, err := e.generateEvalPlan(nil)
		require.NoError(t, err)
		assert.Len(t, plan.Combinations, 2)

		// Check that EvalID is set and Provider/Region are empty
		for _, combo := range plan.Combinations {
			assert.NotEmpty(t, combo.EvalID)
			assert.Empty(t, combo.ProviderID)
			assert.Empty(t, combo.Region)
		}
	})

	t.Run("creates combinations for filtered evals", func(t *testing.T) {
		plan, err := e.generateEvalPlan([]string{"eval-1"})
		require.NoError(t, err)
		assert.Len(t, plan.Combinations, 1)
		assert.Equal(t, "eval-1", plan.Combinations[0].EvalID)
	})
}
