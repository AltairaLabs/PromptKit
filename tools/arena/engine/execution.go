package engine

import (
	"context"
	"crypto/md5"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

// GenerateRunPlan creates a comprehensive test execution plan from filter criteria.
// The plan contains all combinations of regions × providers × scenarios that match
// the provided filters. Scenarios can optionally specify which providers they should
// be tested against via the `providers` field.
//
// Filter behavior:
// - regionFilter: Empty = all regions from prompt configs (or default)
// - providerFilter: Empty = all registered providers (or scenario-specified providers)
// - scenarioFilter: Empty = all loaded scenarios
//
// Provider selection logic:
// 1. If scenario specifies providers: use those (intersected with CLI filter if provided)
// 2. If scenario doesn't specify providers: use all arena providers (intersected with CLI filter)
//
// Returns a RunPlan containing all matching combinations, ready for execution.
// Each combination represents one independent test run that will be executed
// and validated separately.
func (e *Engine) GenerateRunPlan(regionFilter, providerFilter, scenarioFilter []string) (*RunPlan, error) {
	var combinations []RunCombination

	// Get all regions from prompt configs if no region filter
	regions := regionFilter
	if len(regions) == 0 {
		// Use default region if no region filter specified
		regions = []string{"default"}
	}

	// Get all scenario IDs if no scenario filter
	scenarioIDs := scenarioFilter
	if len(scenarioIDs) == 0 {
		for id := range e.scenarios {
			scenarioIDs = append(scenarioIDs, id)
		}
	}

	// Generate combinations - provider selection is per-scenario
	for _, region := range regions {
		for _, scenarioID := range scenarioIDs {
			scenario, exists := e.scenarios[scenarioID]
			if !exists {
				return nil, fmt.Errorf("scenario %s not found", scenarioID)
			}

			// Determine which providers to use for this scenario
			var scenarioProviders []string

			if len(scenario.Providers) > 0 {
				// Scenario specifies providers - use those
				scenarioProviders = scenario.Providers
			} else if len(providerFilter) > 0 {
				// Scenario doesn't specify, but CLI has provider filter - use filter
				// This handles the case where provider registry might be empty (tests)
				scenarioProviders = providerFilter
			} else {
				// Scenario doesn't specify and no CLI filter - use all arena providers
				scenarioProviders = e.providerRegistry.List()
			}

			// Apply CLI provider filter if specified AND scenario specified providers
			// (if scenario didn't specify, we already used the filter above)
			var finalProviders []string
			if len(scenario.Providers) > 0 && len(providerFilter) > 0 {
				// Intersect scenario providers with CLI filter
				filterSet := make(map[string]bool)
				for _, p := range providerFilter {
					filterSet[p] = true
				}
				for _, p := range scenarioProviders {
					if filterSet[p] {
						finalProviders = append(finalProviders, p)
					}
				}
			} else {
				// No intersection needed - use scenario providers as-is
				finalProviders = scenarioProviders
			}

			// Create combinations for this scenario with its providers
			for _, providerID := range finalProviders {
				combinations = append(combinations, RunCombination{
					Region:     region,
					ScenarioID: scenarioID,
					ProviderID: providerID,
				})
			}
		}
	}

	return &RunPlan{Combinations: combinations}, nil
}

// executeRun executes a single test combination: region + scenario + provider.
// This is the core execution method that coordinates all aspects of running one test:
//
// 1. Validates that the scenario and provider exist
// 2. Resolves the task type and builds the system prompt for the region
// 3. Delegates to ConversationExecutor for turn-by-turn execution
// 4. Collects results including conversation transcript, costs, timing, tool calls
// 5. Runs validators and captures validation results
// 6. Saves results to StateStore
// 7. Handles errors gracefully, always returning a RunID (with Error saved in StateStore if failed)
//
// Returns the RunID. Results can be retrieved from StateStore using GetRunResult().
func (e *Engine) executeRun(ctx context.Context, combo RunCombination) (string, error) {
	startTime := time.Now()
	runID := generateRunID(combo)

	// Get Arena state store
	arenaStore, ok := e.stateStore.(*statestore.ArenaStateStore)
	if !ok {
		return runID, fmt.Errorf("statestore is not ArenaStateStore")
	}

	// Helper to save error metadata
	saveError := func(errMsg string) (string, error) {
		metadata := &statestore.RunMetadata{
			RunID:      runID,
			Region:     combo.Region,
			ScenarioID: combo.ScenarioID,
			ProviderID: combo.ProviderID,
			StartTime:  startTime,
			EndTime:    time.Now(),
			Duration:   time.Since(startTime),
			Error:      errMsg,
		}
		if err := arenaStore.SaveMetadata(ctx, runID, metadata); err != nil {
			return runID, fmt.Errorf("failed to save error metadata: %w", err)
		}
		return runID, nil
	}

	// Get scenario
	scenario, exists := e.scenarios[combo.ScenarioID]
	if !exists {
		return saveError(fmt.Sprintf("scenario not found: %s", combo.ScenarioID))
	}

	// Get provider
	provider, exists := e.providerRegistry.Get(combo.ProviderID)
	if !exists {
		return saveError(fmt.Sprintf("provider not found: %s", combo.ProviderID))
	}

	// Execute conversation
	req := ConversationRequest{
		Provider: provider,
		Scenario: scenario,
		Config:   e.config,
		Region:   combo.Region,
		RunID:    runID,
	}

	// Always configure StateStore (always enabled now)
	req.StateStoreConfig = &StateStoreConfig{
		Store: e.stateStore,
		Metadata: map[string]interface{}{
			"region":     combo.Region,
			"provider":   combo.ProviderID,
			"scenario":   combo.ScenarioID,
			"started_at": startTime.Format(time.RFC3339),
		},
	}
	// Use RunID as ConversationID for Arena executions
	req.ConversationID = runID

	convResult := e.conversationExecutor.ExecuteConversation(ctx, req)

	// Save run metadata to StateStore
	metadata := &statestore.RunMetadata{
		RunID:      runID,
		Region:     combo.Region,
		ScenarioID: combo.ScenarioID,
		ProviderID: combo.ProviderID,
		StartTime:  startTime,
		EndTime:    time.Now(),
		Duration:   time.Since(startTime),
		Error:      convResult.Error,
		SelfPlay:   convResult.SelfPlay,
		PersonaID:  convResult.PersonaID,
	}

	if err := arenaStore.SaveMetadata(ctx, runID, metadata); err != nil {
		return runID, fmt.Errorf("failed to save run metadata: %w", err)
	}

	return runID, nil
}

// generateRunID creates a unique run ID for a combination
func generateRunID(combo RunCombination) string {
	timestamp := time.Now().Format("2006-01-02T15-04Z")
	hash := md5.Sum([]byte(fmt.Sprintf("%s_%s_%s", combo.Region, combo.ScenarioID, combo.ProviderID)))
	return fmt.Sprintf("%s_%s_%s_%s_%x", timestamp, combo.ProviderID, combo.Region, combo.ScenarioID, hash[:4])
}
