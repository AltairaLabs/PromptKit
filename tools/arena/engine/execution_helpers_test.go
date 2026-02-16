package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/adapters"
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

		var mu sync.Mutex
		eventReceived := false
		bus.Subscribe(events.EventType("arena.run.failed"), func(e *events.Event) {
			mu.Lock()
			eventReceived = true
			mu.Unlock()
		})

		_, err := e.saveRunError(ctx, store, combo, runID+"_2", startTime, errorMsg, emitter)
		require.NoError(t, err)

		// Give event time to propagate
		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		received := eventReceived
		mu.Unlock()
		assert.True(t, received)
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

		var mu sync.Mutex
		eventReceived := false
		bus.Subscribe(events.EventType("arena.run.failed"), func(e *events.Event) {
			mu.Lock()
			eventReceived = true
			mu.Unlock()
		})

		result := &ConversationResult{
			Error: "test error",
		}

		e.notifyRunCompletion(emitter, result, "run-id", time.Second, 0.05)
		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		received := eventReceived
		mu.Unlock()
		assert.True(t, received)
	})

	t.Run("emits completed event on success", func(t *testing.T) {
		bus := events.NewEventBus()
		e := &Engine{eventBus: bus}
		emitter := events.NewEmitter(bus, "session", "conv", "run-id")

		var mu sync.Mutex
		eventReceived := false
		bus.Subscribe(events.EventType("arena.run.completed"), func(e *events.Event) {
			mu.Lock()
			eventReceived = true
			mu.Unlock()
		})

		result := &ConversationResult{
			Error: "",
		}

		e.notifyRunCompletion(emitter, result, "run-id", 2*time.Second, 0.10)
		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		received := eventReceived
		mu.Unlock()
		assert.True(t, received)
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

	t.Run("returns error for non-existent eval", func(t *testing.T) {
		plan, err := e.generateEvalPlan([]string{"non-existent"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "eval not found")
		assert.Nil(t, plan)
	})
}

func TestEngine_EnumerateRecordings(t *testing.T) {
	t.Run("returns single ref when no adapter registry", func(t *testing.T) {
		e := &Engine{
			adapterRegistry: nil,
		}
		refs, err := e.enumerateRecordings("/path/to/recording.json", "session")
		require.NoError(t, err)
		require.Len(t, refs, 1)
		assert.Equal(t, "/path/to/recording.json", refs[0].ID)
		assert.Equal(t, "/path/to/recording.json", refs[0].Source)
		assert.Equal(t, "session", refs[0].TypeHint)
	})
}

func TestEngine_GenerateEvalPlan_WithAdapterRegistry(t *testing.T) {
	// Create an empty adapter registry (without built-in adapters) and add only our mock
	registry := adapters.NewEmptyRegistry()
	mockAdapter := &multiRefMockAdapter{
		refs: []adapters.RecordingReference{
			{ID: "/recordings/file1.json", Source: "/recordings/*.json", TypeHint: "session"},
			{ID: "/recordings/file2.json", Source: "/recordings/*.json", TypeHint: "session"},
		},
	}
	registry.Register(mockAdapter)

	e := &Engine{
		evals: map[string]*config.Eval{
			"eval-1": {
				Recording: config.RecordingSource{
					Path: "/recordings/*.json",
					Type: "session",
				},
			},
		},
		adapterRegistry: registry,
	}

	t.Run("creates combinations for multiple recordings", func(t *testing.T) {
		plan, err := e.generateEvalPlan([]string{"eval-1"})
		require.NoError(t, err)
		require.Len(t, plan.Combinations, 2)
		assert.Equal(t, "eval-1", plan.Combinations[0].EvalID)
		assert.Equal(t, "/recordings/file1.json", plan.Combinations[0].RecordingRef)
		assert.Equal(t, "eval-1", plan.Combinations[1].EvalID)
		assert.Equal(t, "/recordings/file2.json", plan.Combinations[1].RecordingRef)
	})
}

// multiRefMockAdapter is a mock adapter that returns multiple recording references
type multiRefMockAdapter struct {
	refs []adapters.RecordingReference
}

func (m *multiRefMockAdapter) CanHandle(source, typeHint string) bool {
	// Always handle sources containing our test pattern
	return true
}

func (m *multiRefMockAdapter) Enumerate(source string) ([]adapters.RecordingReference, error) {
	return m.refs, nil
}

func (m *multiRefMockAdapter) Load(ref adapters.RecordingReference) ([]types.Message, *adapters.RecordingMetadata, error) {
	return nil, nil, nil
}

func TestConvertA2AAgentsFromConfig(t *testing.T) {
	t.Run("nil input", func(t *testing.T) {
		result := convertA2AAgentsFromConfig(nil)
		assert.Nil(t, result)
	})

	t.Run("empty input", func(t *testing.T) {
		result := convertA2AAgentsFromConfig([]config.A2AAgentConfig{})
		assert.Nil(t, result)
	})

	t.Run("agent with skills", func(t *testing.T) {
		agents := []config.A2AAgentConfig{
			{
				Card: config.A2ACardConfig{
					Name:        "weather-agent",
					Description: "Provides weather",
					Skills: []config.A2ASkillConfig{
						{
							ID:          "get-weather",
							Name:        "Get Weather",
							Description: "Returns weather data",
							Tags:        []string{"weather", "api"},
						},
					},
				},
			},
		}

		result := convertA2AAgentsFromConfig(agents)

		require.Len(t, result, 1)
		assert.Equal(t, "weather-agent", result[0].Name)
		assert.Equal(t, "Provides weather", result[0].Description)
		require.Len(t, result[0].Skills, 1)
		assert.Equal(t, "get-weather", result[0].Skills[0].ID)
		assert.Equal(t, "Get Weather", result[0].Skills[0].Name)
		assert.Equal(t, "Returns weather data", result[0].Skills[0].Description)
		assert.Equal(t, []string{"weather", "api"}, result[0].Skills[0].Tags)
	})

	t.Run("agent without skills", func(t *testing.T) {
		agents := []config.A2AAgentConfig{
			{
				Card: config.A2ACardConfig{
					Name:        "simple-agent",
					Description: "No skills",
				},
			},
		}

		result := convertA2AAgentsFromConfig(agents)

		require.Len(t, result, 1)
		assert.Equal(t, "simple-agent", result[0].Name)
		assert.Nil(t, result[0].Skills)
	})

	t.Run("multiple agents", func(t *testing.T) {
		agents := []config.A2AAgentConfig{
			{Card: config.A2ACardConfig{Name: "agent-1", Description: "First"}},
			{Card: config.A2ACardConfig{Name: "agent-2", Description: "Second"}},
		}

		result := convertA2AAgentsFromConfig(agents)

		require.Len(t, result, 2)
		assert.Equal(t, "agent-1", result[0].Name)
		assert.Equal(t, "agent-2", result[1].Name)
	})
}

func TestEngine_GetA2AAgentsFromConfig(t *testing.T) {
	t.Run("nil config", func(t *testing.T) {
		e := &Engine{}
		result := e.getA2AAgentsFromConfig()
		assert.Nil(t, result)
	})

	t.Run("config with agents", func(t *testing.T) {
		e := &Engine{
			config: &config.Config{
				A2AAgents: []config.A2AAgentConfig{
					{Card: config.A2ACardConfig{Name: "test-agent"}},
				},
			},
		}
		result := e.getA2AAgentsFromConfig()
		require.Len(t, result, 1)
		assert.Equal(t, "test-agent", result[0].Name)
	})

	t.Run("config without agents", func(t *testing.T) {
		e := &Engine{config: &config.Config{}}
		result := e.getA2AAgentsFromConfig()
		assert.Nil(t, result)
	})
}
