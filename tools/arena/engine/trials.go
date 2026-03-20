package engine

import (
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

// AggregateTrialResults groups trial run results by scenario+provider+region,
// computes statistical metrics, and updates the first run in each group with
// the aggregated TrialResults. Returns the run IDs that represent trial groups
// (i.e., the first run ID of each group that now carries the summary).
func AggregateTrialResults(store *statestore.ArenaStateStore, runIDs []string, combos []RunCombination) []string {
	// Group run IDs by trial group key
	groups := groupTrialRuns(runIDs, combos)
	if len(groups) == 0 {
		return runIDs
	}

	var summaryIDs []string
	for _, group := range groups {
		if len(group.runIDs) <= 1 {
			summaryIDs = append(summaryIDs, group.runIDs...)
			continue
		}

		results := loadTrialResults(store, group.runIDs)
		trialResults := computeTrialResults(results)
		attachTrialResults(store, group.runIDs[0], trialResults)
		summaryIDs = append(summaryIDs, group.runIDs[0])
	}

	return summaryIDs
}

type trialGroup struct {
	key    TrialGroupKey
	runIDs []string
}

func groupTrialRuns(runIDs []string, combos []RunCombination) []trialGroup {
	// Use ordered approach to maintain deterministic output
	keyOrder := make([]TrialGroupKey, 0)
	groupMap := make(map[TrialGroupKey][]string)

	for i, combo := range combos {
		if combo.TotalTrials <= 1 {
			continue
		}
		if i >= len(runIDs) || runIDs[i] == "" {
			continue
		}
		key := TrialGroupKey{
			ScenarioID: combo.ScenarioID,
			ProviderID: combo.ProviderID,
			Region:     combo.Region,
		}
		if _, exists := groupMap[key]; !exists {
			keyOrder = append(keyOrder, key)
		}
		groupMap[key] = append(groupMap[key], runIDs[i])
	}

	groups := make([]trialGroup, 0, len(keyOrder))
	for _, key := range keyOrder {
		groups = append(groups, trialGroup{key: key, runIDs: groupMap[key]})
	}

	// Also include non-trial run IDs as single-entry groups
	for i, combo := range combos {
		if combo.TotalTrials > 1 {
			continue
		}
		if i >= len(runIDs) || runIDs[i] == "" {
			continue
		}
		groups = append(groups, trialGroup{
			key:    TrialGroupKey{ScenarioID: combo.ScenarioID, ProviderID: combo.ProviderID, Region: combo.Region},
			runIDs: []string{runIDs[i]},
		})
	}

	return groups
}

type trialRunResult struct {
	runID             string
	allPassed         bool
	assertionResults  map[string]bool // assertion type -> passed
	conversationError bool
}

func loadTrialResults(store *statestore.ArenaStateStore, runIDs []string) []trialRunResult {
	results := make([]trialRunResult, 0, len(runIDs))
	for _, runID := range runIDs {
		rr, err := store.GetResult(nil, runID) //nolint:staticcheck // nil ctx is fine for in-memory store
		if err != nil || rr == nil {
			results = append(results, trialRunResult{runID: runID, conversationError: true})
			continue
		}

		tr := trialRunResult{
			runID:             runID,
			allPassed:         rr.ConversationAssertions.Passed && rr.Error == "",
			assertionResults:  make(map[string]bool),
			conversationError: rr.Error != "",
		}

		for _, ar := range rr.ConversationAssertions.Results {
			tr.assertionResults[ar.Type] = ar.Passed
		}

		results = append(results, tr)
	}
	return results
}

func computeTrialResults(trials []trialRunResult) *TrialResults {
	// Overall pass/fail per trial
	overallResults := make([]bool, len(trials))
	for i, t := range trials {
		overallResults[i] = t.allPassed
	}

	// Collect assertion types across all trials
	assertionTypes := collectAssertionTypes(trials)

	// Per-assertion stats
	perAssertion := make(map[string]AssertionTrialStats, len(assertionTypes))
	for _, aType := range assertionTypes {
		results := make([]bool, 0, len(trials))
		for _, t := range trials {
			if passed, ok := t.assertionResults[aType]; ok {
				results = append(results, passed)
			}
		}
		passCount := 0
		for _, r := range results {
			if r {
				passCount++
			}
		}
		perAssertion[aType] = AssertionTrialStats{
			PassRate:       evals.PassRate(results),
			PassCount:      passCount,
			FailCount:      len(results) - passCount,
			FlakinessScore: evals.FlakinessScore(results),
		}
	}

	return &TrialResults{
		TrialCount:        len(trials),
		PassRate:          evals.PassRate(overallResults),
		FlakinessScore:    evals.FlakinessScore(overallResults),
		PerAssertionStats: perAssertion,
	}
}

func collectAssertionTypes(trials []trialRunResult) []string {
	seen := make(map[string]bool)
	var types []string
	for _, t := range trials {
		for aType := range t.assertionResults {
			if !seen[aType] {
				seen[aType] = true
				types = append(types, aType)
			}
		}
	}
	return types
}

func attachTrialResults(store *statestore.ArenaStateStore, runID string, tr *TrialResults) {
	_ = store.SetTrialResults(nil, runID, tr) //nolint:staticcheck // nil ctx is fine for in-memory store
}
