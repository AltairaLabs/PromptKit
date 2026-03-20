package engine

import (
	"context"
	"math"
	"testing"

	runtimestore "github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

func TestGroupTrialRuns(t *testing.T) {
	t.Run("no trials", func(t *testing.T) {
		combos := []RunCombination{
			{ScenarioID: "s1", ProviderID: "p1", Region: "r1", TotalTrials: 1},
		}
		runIDs := []string{"run-1"}
		groups := groupTrialRuns(runIDs, combos)
		if len(groups) != 1 {
			t.Fatalf("expected 1 group, got %d", len(groups))
		}
		if len(groups[0].runIDs) != 1 {
			t.Fatalf("expected 1 run in group, got %d", len(groups[0].runIDs))
		}
	})

	t.Run("multiple trials grouped", func(t *testing.T) {
		combos := []RunCombination{
			{ScenarioID: "s1", ProviderID: "p1", Region: "r1", TotalTrials: 3, TrialIndex: 0},
			{ScenarioID: "s1", ProviderID: "p1", Region: "r1", TotalTrials: 3, TrialIndex: 1},
			{ScenarioID: "s1", ProviderID: "p1", Region: "r1", TotalTrials: 3, TrialIndex: 2},
		}
		runIDs := []string{"run-1", "run-2", "run-3"}
		groups := groupTrialRuns(runIDs, combos)
		if len(groups) != 1 {
			t.Fatalf("expected 1 group, got %d", len(groups))
		}
		if len(groups[0].runIDs) != 3 {
			t.Fatalf("expected 3 runs in group, got %d", len(groups[0].runIDs))
		}
	})

	t.Run("mixed trials and non-trials", func(t *testing.T) {
		combos := []RunCombination{
			{ScenarioID: "s1", ProviderID: "p1", Region: "r1", TotalTrials: 2, TrialIndex: 0},
			{ScenarioID: "s1", ProviderID: "p1", Region: "r1", TotalTrials: 2, TrialIndex: 1},
			{ScenarioID: "s2", ProviderID: "p1", Region: "r1", TotalTrials: 1},
		}
		runIDs := []string{"run-1", "run-2", "run-3"}
		groups := groupTrialRuns(runIDs, combos)
		if len(groups) != 2 {
			t.Fatalf("expected 2 groups, got %d", len(groups))
		}
	})

	t.Run("skips empty run IDs", func(t *testing.T) {
		combos := []RunCombination{
			{ScenarioID: "s1", ProviderID: "p1", Region: "r1", TotalTrials: 3, TrialIndex: 0},
			{ScenarioID: "s1", ProviderID: "p1", Region: "r1", TotalTrials: 3, TrialIndex: 1},
			{ScenarioID: "s1", ProviderID: "p1", Region: "r1", TotalTrials: 3, TrialIndex: 2},
		}
		runIDs := []string{"run-1", "", "run-3"}
		groups := groupTrialRuns(runIDs, combos)
		if len(groups) != 1 {
			t.Fatalf("expected 1 group, got %d", len(groups))
		}
		if len(groups[0].runIDs) != 2 {
			t.Fatalf("expected 2 runs (skipping empty), got %d", len(groups[0].runIDs))
		}
	})
}

func TestComputeTrialResults(t *testing.T) {
	t.Run("all pass", func(t *testing.T) {
		trials := []trialRunResult{
			{runID: "r1", allPassed: true, assertionResults: map[string]bool{"contains": true}},
			{runID: "r2", allPassed: true, assertionResults: map[string]bool{"contains": true}},
			{runID: "r3", allPassed: true, assertionResults: map[string]bool{"contains": true}},
		}
		result := computeTrialResults(trials)
		if result.TrialCount != 3 {
			t.Fatalf("expected 3 trials, got %d", result.TrialCount)
		}
		if result.PassRate != 1.0 {
			t.Fatalf("expected pass rate 1.0, got %f", result.PassRate)
		}
		if result.FlakinessScore != 0 {
			t.Fatalf("expected flakiness 0, got %f", result.FlakinessScore)
		}
		stats := result.PerAssertionStats["contains"]
		if stats.PassRate != 1.0 {
			t.Fatalf("expected assertion pass rate 1.0, got %f", stats.PassRate)
		}
	})

	t.Run("mixed results", func(t *testing.T) {
		trials := []trialRunResult{
			{runID: "r1", allPassed: true, assertionResults: map[string]bool{"contains": true, "regex": true}},
			{runID: "r2", allPassed: false, assertionResults: map[string]bool{"contains": true, "regex": false}},
			{runID: "r3", allPassed: false, assertionResults: map[string]bool{"contains": false, "regex": false}},
		}
		result := computeTrialResults(trials)
		if result.TrialCount != 3 {
			t.Fatalf("expected 3 trials, got %d", result.TrialCount)
		}
		// 1/3 pass overall
		if math.Abs(result.PassRate-1.0/3.0) > 1e-9 {
			t.Fatalf("expected pass rate ~0.333, got %f", result.PassRate)
		}
		// contains: 2/3 pass
		containsStats := result.PerAssertionStats["contains"]
		if math.Abs(containsStats.PassRate-2.0/3.0) > 1e-9 {
			t.Fatalf("expected contains pass rate ~0.667, got %f", containsStats.PassRate)
		}
		if containsStats.PassCount != 2 || containsStats.FailCount != 1 {
			t.Fatalf("expected 2 pass, 1 fail, got %d/%d", containsStats.PassCount, containsStats.FailCount)
		}
		// regex: 1/3 pass
		regexStats := result.PerAssertionStats["regex"]
		if math.Abs(regexStats.PassRate-1.0/3.0) > 1e-9 {
			t.Fatalf("expected regex pass rate ~0.333, got %f", regexStats.PassRate)
		}
	})

	t.Run("all fail", func(t *testing.T) {
		trials := []trialRunResult{
			{runID: "r1", allPassed: false, assertionResults: map[string]bool{"contains": false}},
			{runID: "r2", allPassed: false, assertionResults: map[string]bool{"contains": false}},
		}
		result := computeTrialResults(trials)
		if result.PassRate != 0 {
			t.Fatalf("expected pass rate 0, got %f", result.PassRate)
		}
		if result.FlakinessScore != 0 {
			t.Fatalf("expected flakiness 0 (deterministic failure), got %f", result.FlakinessScore)
		}
	})

	t.Run("50/50 is maximally flaky", func(t *testing.T) {
		trials := []trialRunResult{
			{runID: "r1", allPassed: true, assertionResults: map[string]bool{}},
			{runID: "r2", allPassed: false, assertionResults: map[string]bool{}},
		}
		result := computeTrialResults(trials)
		if result.FlakinessScore != 1.0 {
			t.Fatalf("expected flakiness 1.0, got %f", result.FlakinessScore)
		}
	})
}

func TestGenerateScenarioCombinations_Trials(t *testing.T) {
	// Test that trial expansion works in generateScenarioCombinations
	// by verifying the RunCombination fields
	combos := []RunCombination{
		{ScenarioID: "s1", ProviderID: "p1", Region: "r1", TotalTrials: 3, TrialIndex: 0},
		{ScenarioID: "s1", ProviderID: "p1", Region: "r1", TotalTrials: 3, TrialIndex: 1},
		{ScenarioID: "s1", ProviderID: "p1", Region: "r1", TotalTrials: 3, TrialIndex: 2},
	}

	for i, c := range combos {
		if c.TrialIndex != i {
			t.Errorf("combo %d: expected TrialIndex %d, got %d", i, i, c.TrialIndex)
		}
		if c.TotalTrials != 3 {
			t.Errorf("combo %d: expected TotalTrials 3, got %d", i, c.TotalTrials)
		}
	}
}

func TestAggregateTrialResults(t *testing.T) {
	t.Run("single run no aggregation", func(t *testing.T) {
		store := statestore.NewArenaStateStore()
		combos := []RunCombination{
			{ScenarioID: "s1", ProviderID: "p1", Region: "r1", TotalTrials: 1},
		}
		runIDs := []string{"run-1"}
		summaryIDs := AggregateTrialResults(store, runIDs, combos)
		if len(summaryIDs) != 1 {
			t.Fatalf("expected 1 summary ID, got %d", len(summaryIDs))
		}
		if summaryIDs[0] != "run-1" {
			t.Fatalf("expected run-1, got %s", summaryIDs[0])
		}
	})

	t.Run("multi-trial aggregation end-to-end", func(t *testing.T) {
		store := statestore.NewArenaStateStore()
		ctx := context.Background()

		// Set up 3 trial runs with mixed results
		setupTrialRun(t, store, ctx, "run-a", true, []assertions.ConversationValidationResult{
			{Type: "contains", Passed: true, Message: "ok"},
		})
		setupTrialRun(t, store, ctx, "run-b", false, []assertions.ConversationValidationResult{
			{Type: "contains", Passed: false, Message: "fail"},
		})
		setupTrialRun(t, store, ctx, "run-c", true, []assertions.ConversationValidationResult{
			{Type: "contains", Passed: true, Message: "ok"},
		})

		combos := []RunCombination{
			{ScenarioID: "s1", ProviderID: "p1", Region: "r1", TotalTrials: 3, TrialIndex: 0},
			{ScenarioID: "s1", ProviderID: "p1", Region: "r1", TotalTrials: 3, TrialIndex: 1},
			{ScenarioID: "s1", ProviderID: "p1", Region: "r1", TotalTrials: 3, TrialIndex: 2},
		}
		runIDs := []string{"run-a", "run-b", "run-c"}

		summaryIDs := AggregateTrialResults(store, runIDs, combos)
		if len(summaryIDs) != 1 {
			t.Fatalf("expected 1 summary ID, got %d", len(summaryIDs))
		}
		if summaryIDs[0] != "run-a" {
			t.Fatalf("expected run-a, got %s", summaryIDs[0])
		}

		// Verify the trial results were attached to the first run
		rr, err := store.GetResult(ctx, "run-a")
		if err != nil {
			t.Fatalf("GetResult: %v", err)
		}
		if rr.TrialResults == nil {
			t.Fatal("expected TrialResults to be attached")
		}
	})
}

func TestLoadTrialResults(t *testing.T) {
	t.Run("loads results from store", func(t *testing.T) {
		store := statestore.NewArenaStateStore()
		ctx := context.Background()

		setupTrialRun(t, store, ctx, "r1", true, []assertions.ConversationValidationResult{
			{Type: "contains", Passed: true, Message: "ok"},
			{Type: "regex", Passed: true, Message: "ok"},
		})
		setupTrialRun(t, store, ctx, "r2", false, []assertions.ConversationValidationResult{
			{Type: "contains", Passed: true, Message: "ok"},
			{Type: "regex", Passed: false, Message: "fail"},
		})

		results := loadTrialResults(store, []string{"r1", "r2"})
		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}
		if !results[0].allPassed {
			t.Fatal("expected r1 to pass")
		}
		if results[1].allPassed {
			t.Fatal("expected r2 to fail")
		}
		if !results[0].assertionResults["contains"] {
			t.Fatal("expected r1 contains to pass")
		}
		if results[1].assertionResults["regex"] {
			t.Fatal("expected r2 regex to fail")
		}
	})

	t.Run("missing run returns conversation error", func(t *testing.T) {
		store := statestore.NewArenaStateStore()
		results := loadTrialResults(store, []string{"nonexistent"})
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if !results[0].conversationError {
			t.Fatal("expected conversationError for missing run")
		}
	})

	t.Run("run with error marked as conversation error", func(t *testing.T) {
		store := statestore.NewArenaStateStore()
		ctx := context.Background()

		setupTrialRunWithError(t, store, ctx, "r-err", "provider timeout")

		results := loadTrialResults(store, []string{"r-err"})
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if !results[0].conversationError {
			t.Fatal("expected conversationError for errored run")
		}
		if results[0].allPassed {
			t.Fatal("expected allPassed to be false for errored run")
		}
	})
}

func TestAttachTrialResults(t *testing.T) {
	t.Run("attaches to existing run", func(t *testing.T) {
		store := statestore.NewArenaStateStore()
		ctx := context.Background()

		setupTrialRun(t, store, ctx, "run-1", true, nil)

		tr := &TrialResults{
			TrialCount:     3,
			PassRate:       2.0 / 3.0,
			FlakinessScore: 0.5,
		}
		attachTrialResults(store, "run-1", tr)

		rr, err := store.GetResult(ctx, "run-1")
		if err != nil {
			t.Fatalf("GetResult: %v", err)
		}
		if rr.TrialResults == nil {
			t.Fatal("expected TrialResults to be attached")
		}
	})

	t.Run("no-op for missing run", func(t *testing.T) {
		store := statestore.NewArenaStateStore()
		tr := &TrialResults{TrialCount: 3, PassRate: 1.0}
		// Should not panic
		attachTrialResults(store, "nonexistent", tr)
	})
}

// setupTrialRun creates a run in the statestore with the given assertion results.
func setupTrialRun(t *testing.T, store *statestore.ArenaStateStore, ctx context.Context, runID string, passed bool, cvResults []assertions.ConversationValidationResult) {
	t.Helper()
	if err := store.Save(ctx, &runtimestore.ConversationState{
		ID: runID,
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	meta := &statestore.RunMetadata{
		RunID:                        runID,
		ConversationAssertionResults: cvResults,
	}
	if err := store.SaveMetadata(ctx, runID, meta); err != nil {
		t.Fatalf("SaveMetadata: %v", err)
	}
}

// setupTrialRunWithError creates a run with an error in the statestore.
func setupTrialRunWithError(t *testing.T, store *statestore.ArenaStateStore, ctx context.Context, runID, errMsg string) {
	t.Helper()
	if err := store.Save(ctx, &runtimestore.ConversationState{
		ID: runID,
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	meta := &statestore.RunMetadata{
		RunID: runID,
		Error: errMsg,
	}
	if err := store.SaveMetadata(ctx, runID, meta); err != nil {
		t.Fatalf("SaveMetadata: %v", err)
	}
}
