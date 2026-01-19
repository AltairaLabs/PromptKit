package engine

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateEvalPlan(t *testing.T) {
	t.Run("generates combinations for all evals when no filter", func(t *testing.T) {
		eng := &Engine{
			evals: map[string]*config.Eval{
				"eval1": {ID: "eval1"},
				"eval2": {ID: "eval2"},
			},
		}

		plan, err := eng.generateEvalPlan(nil)
		require.NoError(t, err)
		require.NotNil(t, plan)
		assert.Len(t, plan.Combinations, 2)

		// Verify both evals are present
		evalIDs := make(map[string]bool)
		for _, combo := range plan.Combinations {
			evalIDs[combo.EvalID] = true
			assert.Empty(t, combo.Region)
			assert.Empty(t, combo.ProviderID)
			assert.Empty(t, combo.ScenarioID)
		}
		assert.True(t, evalIDs["eval1"])
		assert.True(t, evalIDs["eval2"])
	})

	t.Run("filters to specific evals", func(t *testing.T) {
		eng := &Engine{
			evals: map[string]*config.Eval{
				"eval1": {ID: "eval1"},
				"eval2": {ID: "eval2"},
				"eval3": {ID: "eval3"},
			},
		}

		plan, err := eng.generateEvalPlan([]string{"eval1", "eval3"})
		require.NoError(t, err)
		require.NotNil(t, plan)
		assert.Len(t, plan.Combinations, 2)

		evalIDs := make(map[string]bool)
		for _, combo := range plan.Combinations {
			evalIDs[combo.EvalID] = true
		}
		assert.True(t, evalIDs["eval1"])
		assert.True(t, evalIDs["eval3"])
		assert.False(t, evalIDs["eval2"])
	})

	t.Run("empty plan when no evals", func(t *testing.T) {
		eng := &Engine{
			evals: map[string]*config.Eval{},
		}

		plan, err := eng.generateEvalPlan(nil)
		require.NoError(t, err)
		require.NotNil(t, plan)
		assert.Empty(t, plan.Combinations)
	})
}

func TestResolveEvals(t *testing.T) {
	t.Run("returns filter when provided", func(t *testing.T) {
		eng := &Engine{
			evals: map[string]*config.Eval{
				"eval1": {ID: "eval1"},
				"eval2": {ID: "eval2"},
			},
		}

		result := eng.resolveEvals([]string{"eval1"})
		assert.Equal(t, []string{"eval1"}, result)
	})

	t.Run("returns all evals when no filter", func(t *testing.T) {
		eng := &Engine{
			evals: map[string]*config.Eval{
				"eval1": {ID: "eval1"},
				"eval2": {ID: "eval2"},
			},
		}

		result := eng.resolveEvals(nil)
		assert.Len(t, result, 2)
		assert.Contains(t, result, "eval1")
		assert.Contains(t, result, "eval2")
	})

	t.Run("returns empty when no evals", func(t *testing.T) {
		eng := &Engine{
			evals: map[string]*config.Eval{},
		}

		result := eng.resolveEvals(nil)
		assert.Empty(t, result)
	})
}

func TestExecuteEvalRun(t *testing.T) {
	t.Run("generates correct runID format", func(t *testing.T) {
		combo := RunCombination{EvalID: "test-eval"}
		runID := generateRunID(combo)

		assert.Contains(t, runID, "_eval_test-eval_")
	})
}

func TestGenerateRunPlan_WithEvals(t *testing.T) {
	t.Run("generates eval plan when evalFilter provided", func(t *testing.T) {
		eng := &Engine{
			evals: map[string]*config.Eval{
				"eval1": {ID: "eval1"},
				"eval2": {ID: "eval2"},
			},
			scenarios: map[string]*config.Scenario{
				"scenario1": {ID: "scenario1"},
			},
		}

		plan, err := eng.GenerateRunPlan(nil, nil, nil, []string{"eval1"})
		require.NoError(t, err)
		require.NotNil(t, plan)
		assert.Len(t, plan.Combinations, 1)
		assert.Equal(t, "eval1", plan.Combinations[0].EvalID)
	})

	t.Run("generates scenario plan when no evalFilter", func(t *testing.T) {
		eng := &Engine{
			evals: map[string]*config.Eval{
				"eval1": {ID: "eval1"},
			},
			scenarios: map[string]*config.Scenario{
				"scenario1": {ID: "scenario1", Providers: []string{"p1"}},
			},
			providers: map[string]*config.Provider{
				"p1": {ID: "p1"},
			},
			config: &config.Config{},
		}

		// Since scenario path requires more setup, just verify it doesn't use eval path
		plan, err := eng.GenerateRunPlan(nil, []string{"p1"}, []string{"scenario1"}, nil)
		require.NoError(t, err)
		require.NotNil(t, plan)
		// Should generate scenario combinations, not eval
		if len(plan.Combinations) > 0 {
			assert.Equal(t, "scenario1", plan.Combinations[0].ScenarioID)
			assert.Empty(t, plan.Combinations[0].EvalID)
		}
	})
}
