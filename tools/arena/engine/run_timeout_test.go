package engine

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

// hangingConversationExecutor blocks until ctx is cancelled, simulating a hanging provider.
type hangingConversationExecutor struct {
	callCount atomic.Int32
}

func (h *hangingConversationExecutor) ExecuteConversation(
	ctx context.Context,
	_ ConversationRequest,
) *ConversationResult {
	h.callCount.Add(1)
	<-ctx.Done()
	return &ConversationResult{
		Error:  "context cancelled",
		Failed: true,
	}
}

func (h *hangingConversationExecutor) ExecuteConversationStream(
	_ context.Context,
	_ ConversationRequest,
) (<-chan ConversationStreamChunk, error) {
	return nil, nil
}

// fastConversationExecutor returns immediately with a successful result.
type fastConversationExecutor struct {
	callCount atomic.Int32
}

func (f *fastConversationExecutor) ExecuteConversation(
	_ context.Context,
	_ ConversationRequest,
) *ConversationResult {
	f.callCount.Add(1)
	return &ConversationResult{
		Messages: []types.Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi"},
		},
	}
}

func (f *fastConversationExecutor) ExecuteConversationStream(
	_ context.Context,
	_ ConversationRequest,
) (<-chan ConversationStreamChunk, error) {
	return nil, nil
}

// newMockProviderRegistry creates a provider registry with a single mock provider.
func newMockProviderRegistry(providerID string) *providers.Registry {
	registry := providers.NewRegistry()
	repo := mock.NewInMemoryMockRepository("mock response")
	mockProvider := mock.NewToolProviderWithRepository(providerID, "mock-model", false, repo)
	registry.Register(mockProvider)
	return registry
}

func TestResolveRunTimeout(t *testing.T) {
	t.Run("returns default when config is nil", func(t *testing.T) {
		e := &Engine{}
		assert.Equal(t, DefaultRunTimeout, e.resolveRunTimeout())
	})

	t.Run("returns default when run_timeout is empty", func(t *testing.T) {
		e := &Engine{
			config: &config.Config{
				Defaults: config.Defaults{
					RunTimeout: "",
				},
			},
		}
		assert.Equal(t, DefaultRunTimeout, e.resolveRunTimeout())
	})

	t.Run("parses valid duration", func(t *testing.T) {
		e := &Engine{
			config: &config.Config{
				Defaults: config.Defaults{
					RunTimeout: "30s",
				},
			},
		}
		assert.Equal(t, 30*time.Second, e.resolveRunTimeout())
	})

	t.Run("parses minutes", func(t *testing.T) {
		e := &Engine{
			config: &config.Config{
				Defaults: config.Defaults{
					RunTimeout: "10m",
				},
			},
		}
		assert.Equal(t, 10*time.Minute, e.resolveRunTimeout())
	})

	t.Run("returns default for invalid duration", func(t *testing.T) {
		e := &Engine{
			config: &config.Config{
				Defaults: config.Defaults{
					RunTimeout: "not-a-duration",
				},
			},
		}
		assert.Equal(t, DefaultRunTimeout, e.resolveRunTimeout())
	})

	t.Run("returns default for zero duration", func(t *testing.T) {
		e := &Engine{
			config: &config.Config{
				Defaults: config.Defaults{
					RunTimeout: "0s",
				},
			},
		}
		assert.Equal(t, DefaultRunTimeout, e.resolveRunTimeout())
	})

	t.Run("returns default for negative duration", func(t *testing.T) {
		e := &Engine{
			config: &config.Config{
				Defaults: config.Defaults{
					RunTimeout: "-5s",
				},
			},
		}
		assert.Equal(t, DefaultRunTimeout, e.resolveRunTimeout())
	})
}

func TestExecuteRun_Timeout(t *testing.T) {
	t.Run("hanging provider is cancelled by timeout", func(t *testing.T) {
		store := statestore.NewArenaStateStore()
		executor := &hangingConversationExecutor{}

		e := &Engine{
			config: &config.Config{
				Defaults: config.Defaults{
					RunTimeout: "200ms", // Short timeout for test
				},
			},
			stateStore:           store,
			conversationExecutor: executor,
			providerRegistry:     newMockProviderRegistry("mock-provider"),
			scenarios: map[string]*config.Scenario{
				"test-scenario": {ID: "test-scenario", TaskType: "support"},
			},
			providers: map[string]*config.Provider{
				"mock-provider": {ID: "mock-provider"},
			},
		}

		combo := RunCombination{
			Region:     "default",
			ScenarioID: "test-scenario",
			ProviderID: "mock-provider",
		}

		ctx := context.Background()
		start := time.Now()
		runID, err := e.executeRun(ctx, combo)
		elapsed := time.Since(start)

		// Should complete within a reasonable time (timeout + overhead)
		assert.Less(t, elapsed, 2*time.Second, "run should complete quickly after timeout")
		require.NoError(t, err) // executeRun saves error in statestore, returns nil

		// Verify the run was recorded with a timeout error
		result, err := store.GetRunResult(ctx, runID)
		require.NoError(t, err)
		assert.Contains(t, result.Error, "run timed out after 200ms")

		// Verify the executor was actually called
		assert.Equal(t, int32(1), executor.callCount.Load())
	})

	t.Run("fast provider completes before timeout", func(t *testing.T) {
		store := statestore.NewArenaStateStore()
		executor := &fastConversationExecutor{}

		e := &Engine{
			config: &config.Config{
				Defaults: config.Defaults{
					RunTimeout: "10s", // Long timeout
				},
			},
			stateStore:           store,
			conversationExecutor: executor,
			providerRegistry:     newMockProviderRegistry("mock-provider"),
			scenarios: map[string]*config.Scenario{
				"test-scenario": {ID: "test-scenario", TaskType: "support"},
			},
			providers: map[string]*config.Provider{
				"mock-provider": {ID: "mock-provider"},
			},
		}

		combo := RunCombination{
			Region:     "default",
			ScenarioID: "test-scenario",
			ProviderID: "mock-provider",
		}

		ctx := context.Background()
		runID, err := e.executeRun(ctx, combo)
		require.NoError(t, err)

		// Verify the run succeeded without timeout error
		result, err := store.GetRunResult(ctx, runID)
		require.NoError(t, err)
		assert.Empty(t, result.Error, "successful run should have no error")

		// Verify the executor was called
		assert.Equal(t, int32(1), executor.callCount.Load())
	})
}

func TestExecuteRuns_ContextAwareSemaphore(t *testing.T) {
	t.Run("cancelled context skips semaphore acquisition", func(t *testing.T) {
		store := statestore.NewArenaStateStore()
		executor := &fastConversationExecutor{}

		e := &Engine{
			config: &config.Config{
				Defaults: config.Defaults{
					RunTimeout: "10s",
				},
			},
			stateStore:           store,
			conversationExecutor: executor,
			providerRegistry:     newMockProviderRegistry("p1"),
			scenarios: map[string]*config.Scenario{
				"s1": {ID: "s1", TaskType: "support"},
			},
			providers: map[string]*config.Provider{
				"p1": {ID: "p1"},
			},
		}

		plan := &RunPlan{
			Combinations: []RunCombination{
				{Region: "default", ScenarioID: "s1", ProviderID: "p1"},
				{Region: "us", ScenarioID: "s1", ProviderID: "p1"},
				{Region: "eu", ScenarioID: "s1", ProviderID: "p1"},
			},
		}

		// Cancel context immediately
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		runIDs, err := e.ExecuteRuns(ctx, plan, 1)
		// Some or all runs may fail due to cancelled context
		assert.Len(t, runIDs, 3)

		// At least some runs should have failed with context error
		hasContextErr := false
		if err != nil {
			hasContextErr = true
		}
		for _, id := range runIDs {
			if id == "" {
				hasContextErr = true
			}
		}
		assert.True(t, hasContextErr, "cancelled context should cause errors")
	})
}

func TestDefaultRunTimeout_Value(t *testing.T) {
	assert.Equal(t, 5*time.Minute, DefaultRunTimeout)
}
