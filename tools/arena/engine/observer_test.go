package engine

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockObserver tracks observer callbacks for testing
type mockObserver struct {
	mu         sync.Mutex
	started    []string
	completed  map[string]observedRun
	failed     map[string]error
	startOrder []string
}

type observedRun struct {
	scenario string
	provider string
	region   string
	duration time.Duration
	cost     float64
}

func newMockObserver() *mockObserver {
	return &mockObserver{
		completed:  make(map[string]observedRun),
		failed:     make(map[string]error),
		startOrder: make([]string, 0),
	}
}

func (m *mockObserver) OnRunStarted(runID, scenario, provider, region string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.started = append(m.started, runID)
	m.startOrder = append(m.startOrder, fmt.Sprintf("%s/%s/%s", provider, scenario, region))
}

func (m *mockObserver) OnRunCompleted(runID string, duration time.Duration, cost float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Record completion
	m.completed[runID] = observedRun{
		duration: duration,
		cost:     cost,
	}
}

func (m *mockObserver) OnRunFailed(runID string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.failed[runID] = err
}

func (m *mockObserver) assertStarted(t *testing.T, expectedCount int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	assert.Equal(t, expectedCount, len(m.started), "Expected %d runs to start", expectedCount)
}

func (m *mockObserver) assertCompleted(t *testing.T, expectedCount int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	assert.Equal(t, expectedCount, len(m.completed), "Expected %d runs to complete", expectedCount)
}

func (m *mockObserver) assertFailed(t *testing.T, expectedCount int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	assert.Equal(t, expectedCount, len(m.failed), "Expected %d runs to fail", expectedCount)
}

func TestObserver_SetAndGet(t *testing.T) {
	// Create a minimal engine for testing
	eng := &Engine{}

	// Initially no observer
	assert.Nil(t, eng.observer)

	// Set an observer
	obs := newMockObserver()
	eng.SetObserver(obs)
	assert.NotNil(t, eng.observer)

	// Set to nil
	eng.SetObserver(nil)
	assert.Nil(t, eng.observer)
}

func TestObserver_NoObserver(t *testing.T) {
	// Test that operations work fine without an observer
	// This test would need a full engine setup, so we'll keep it simple
	eng := &Engine{}

	// Should not panic when observer is nil
	assert.NotPanics(t, func() {
		eng.SetObserver(nil)
	})
}

func TestObserver_Integration(t *testing.T) {
	// Full integration test with mock provider
	tmpDir := t.TempDir()
	
	cfg := &config.Config{
		Defaults: config.Defaults{
			Verbose: false,
		},
	}

	eng := newTestEngine(t, tmpDir, cfg)
	defer eng.Close()

	// Add a test scenario
	eng.scenarios = map[string]*config.Scenario{
		"test-scenario": {
			ID:       "test-scenario",
			TaskType: "test",
			Turns: []config.TurnDefinition{
				{
					Role:    "user",
					Content: "Hello",
				},
			},
		},
	}

	// Enable mock provider mode
	err := eng.EnableMockProviderMode("")
	require.NoError(t, err)

	// Set up observer
	obs := newMockObserver()
	eng.SetObserver(obs)

	// Create run plan with test scenario
	plan := &RunPlan{
		Combinations: []RunCombination{
			{ScenarioID: "test-scenario", ProviderID: "mock", Region: "us"},
		},
	}

	// Execute runs
	ctx := context.Background()
	runIDs, err := eng.ExecuteRuns(ctx, plan, 1)
	require.NoError(t, err)
	require.Len(t, runIDs, 1)

	// Verify observer received callbacks
	obs.assertStarted(t, 1)

	// Check if the run completed or failed
	obs.mu.Lock()
	completed := len(obs.completed)
	failed := len(obs.failed)
	obs.mu.Unlock()

	// Either completed or failed should be 1, but not both
	assert.Equal(t, 1, completed+failed, "Expected exactly one completion or failure")

	// Verify run details if completed
	if completed == 1 {
		obs.mu.Lock()
		runID := obs.started[0]
		run := obs.completed[runID]
		obs.mu.Unlock()

		assert.Greater(t, run.duration, time.Duration(0))
		assert.GreaterOrEqual(t, run.cost, 0.0)
	}
}

func TestObserver_ThreadSafety(t *testing.T) {
	// Test that observer handles concurrent callbacks correctly
	obs := newMockObserver()

	var wg sync.WaitGroup
	numGoroutines := 100

	// Simulate concurrent OnRunStarted calls
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			runID := fmt.Sprintf("run-%d", id)
			obs.OnRunStarted(runID, "scenario", "provider", "region")

			// Simulate some work
			time.Sleep(time.Millisecond)

			if id%2 == 0 {
				obs.OnRunCompleted(runID, time.Second, 0.001)
			} else {
				obs.OnRunFailed(runID, fmt.Errorf("test error"))
			}
		}(i)
	}

	wg.Wait()

	// Verify all callbacks were recorded
	obs.assertStarted(t, numGoroutines)
	obs.assertCompleted(t, numGoroutines/2)
	obs.assertFailed(t, numGoroutines/2)
}

func TestObserver_CallbackOrder(t *testing.T) {
	// Test that OnRunStarted is always called before OnRunCompleted/OnRunFailed
	obs := newMockObserver()

	runID := "test-run-1"

	// Start
	obs.OnRunStarted(runID, "scenario", "provider", "region")

	// Verify started
	obs.mu.Lock()
	require.Contains(t, obs.started, runID)
	require.NotContains(t, obs.completed, runID)
	require.NotContains(t, obs.failed, runID)
	obs.mu.Unlock()

	// Complete
	obs.OnRunCompleted(runID, time.Second, 0.001)

	// Verify completed
	obs.mu.Lock()
	require.Contains(t, obs.started, runID)
	require.Contains(t, obs.completed, runID)
	require.NotContains(t, obs.failed, runID)
	obs.mu.Unlock()
}

func TestObserver_MultipleFailures(t *testing.T) {
	// Test that multiple failures are recorded correctly
	obs := newMockObserver()

	runIDs := []string{"run-1", "run-2", "run-3"}

	for _, runID := range runIDs {
		obs.OnRunStarted(runID, "scenario", "provider", "region")
		obs.OnRunFailed(runID, fmt.Errorf("error for %s", runID))
	}

	obs.assertStarted(t, 3)
	obs.assertFailed(t, 3)
	obs.assertCompleted(t, 0)
}

func TestObserver_Integration_FailureScenario(t *testing.T) {
	// Test observer callbacks when run fails (missing provider)
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Defaults: config.Defaults{
			Verbose: false,
		},
	}

	eng := newTestEngine(t, tmpDir, cfg)
	defer eng.Close()

	// Add a test scenario
	eng.scenarios = map[string]*config.Scenario{
		"test-scenario": {
			ID:       "test-scenario",
			TaskType: "test",
			Turns: []config.TurnDefinition{
				{
					Role:    "user",
					Content: "Hello",
				},
			},
		},
	}

	// Set up observer BEFORE execution (don't enable mock mode, so provider will fail)
	obs := newMockObserver()
	eng.SetObserver(obs)

	// Create run plan with non-existent provider (will trigger failure path)
	plan := &RunPlan{
		Combinations: []RunCombination{
			{ScenarioID: "test-scenario", ProviderID: "nonexistent-provider", Region: "us"},
		},
	}

	// Execute runs (should fail but not error)
	ctx := context.Background()
	runIDs, err := eng.ExecuteRuns(ctx, plan, 1)
	require.NoError(t, err)
	require.Len(t, runIDs, 1)

	// Verify observer received callbacks for failure
	obs.assertStarted(t, 1)
	obs.assertFailed(t, 1)
	obs.assertCompleted(t, 0)

	// Verify the failure was recorded
	obs.mu.Lock()
	runID := obs.started[0]
	failErr := obs.failed[runID]
	obs.mu.Unlock()

	assert.NotNil(t, failErr)
	assert.Contains(t, failErr.Error(), "provider not found")
}
