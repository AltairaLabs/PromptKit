package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui"
)

func TestEventBus_PushesRunLifecycleToTUI(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.Defaults{
			Verbose: false,
		},
	}

	eng := newTestEngine(t, tmpDir, cfg)
	t.Cleanup(func() {
		_ = eng.Close()
	})

	eng.scenarios = map[string]*config.Scenario{
		"event-demo": {
			ID:       "event-demo",
			TaskType: "test",
			Turns: []config.TurnDefinition{
				{Role: "user", Content: "hi"},
			},
		},
	}

	require.NoError(t, eng.EnableMockProviderMode(""))

	bus := events.NewEventBus()
	eng.SetEventBus(bus)

	model := tui.NewModel("event-demo", 1)
	adapter := tui.NewEventAdapterWithModel(model)
	adapter.Subscribe(bus)

	plan := &RunPlan{
		Combinations: []RunCombination{
			{ScenarioID: "event-demo", ProviderID: "mock", Region: "us"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	runIDs, err := eng.ExecuteRuns(ctx, plan, 1)
	require.NoError(t, err)
	require.Len(t, runIDs, 1)

	require.Eventually(t, func() bool {
		return model.CompletedCount() >= 1
	}, time.Second, 10*time.Millisecond, "expected run completion to be observed via event bus")

	activeRuns := model.ActiveRuns()
	require.NotEmpty(t, activeRuns)
	assert.Equal(t, runIDs[0], activeRuns[0].RunID)
	assert.Contains(t, []tui.RunStatus{tui.StatusCompleted, tui.StatusFailed}, activeRuns[0].Status)
	assert.GreaterOrEqual(t, len(model.Logs()), 1)
}
