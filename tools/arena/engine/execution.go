package engine

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
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
	regions := e.resolveRegions(regionFilter)
	scenarioIDs, err := e.resolveScenarios(scenarioFilter)
	if err != nil {
		return nil, err
	}

	combinations, err := e.generateCombinations(regions, scenarioIDs, providerFilter)
	if err != nil {
		return nil, err
	}

	return &RunPlan{Combinations: combinations}, nil
}

// resolveRegions returns the regions to use, applying default if empty
func (e *Engine) resolveRegions(regionFilter []string) []string {
	if len(regionFilter) == 0 {
		return []string{"default"}
	}
	return regionFilter
}

// resolveScenarios returns the scenario IDs to use, applying all scenarios if empty
func (e *Engine) resolveScenarios(scenarioFilter []string) ([]string, error) {
	if len(scenarioFilter) > 0 {
		return scenarioFilter, nil
	}

	var scenarioIDs []string
	for id := range e.scenarios {
		scenarioIDs = append(scenarioIDs, id)
	}
	return scenarioIDs, nil
}

// generateCombinations creates all valid combinations of regions, scenarios, and providers
func (e *Engine) generateCombinations(regions, scenarioIDs []string, providerFilter []string) ([]RunCombination, error) {
	var combinations []RunCombination

	for _, region := range regions {
		for _, scenarioID := range scenarioIDs {
			scenarioCombinations, err := e.generateScenarioCombinations(region, scenarioID, providerFilter)
			if err != nil {
				return nil, err
			}
			combinations = append(combinations, scenarioCombinations...)
		}
	}

	return combinations, nil
}

// generateScenarioCombinations creates combinations for a specific scenario
func (e *Engine) generateScenarioCombinations(region, scenarioID string, providerFilter []string) ([]RunCombination, error) {
	scenario, exists := e.scenarios[scenarioID]
	if !exists {
		return nil, fmt.Errorf("scenario %s not found", scenarioID)
	}

	providers := e.resolveProvidersForScenario(scenario, providerFilter)

	var combinations []RunCombination
	for _, providerID := range providers {
		combinations = append(combinations, RunCombination{
			Region:     region,
			ScenarioID: scenarioID,
			ProviderID: providerID,
		})
	}

	return combinations, nil
}

// resolveProvidersForScenario determines which providers to use for a specific scenario
func (e *Engine) resolveProvidersForScenario(scenario *config.Scenario, providerFilter []string) []string {
	// Determine initial provider list based on scenario
	scenarioProviders := e.getInitialProviders(scenario, providerFilter)

	// Apply filter if both scenario specifies providers and filter is provided
	if len(scenario.Providers) > 0 && len(providerFilter) > 0 {
		return e.intersectProviders(scenarioProviders, providerFilter)
	}

	return scenarioProviders
}

// getInitialProviders gets the initial provider list based on scenario and filters
func (e *Engine) getInitialProviders(scenario *config.Scenario, providerFilter []string) []string {
	if len(scenario.Providers) > 0 {
		return scenario.Providers
	}

	group := scenario.ProviderGroup
	if group == "" {
		group = "default"
	}

	var providers []string
	for id := range e.providers {
		refGroup := e.config.ProviderGroups[id]
		if refGroup == "" {
			refGroup = "default"
		}
		if refGroup == group {
			providers = append(providers, id)
		}
	}

	// Fallback to all providers if none matched
	if len(providers) == 0 {
		providers = e.providerRegistry.List()
	}

	// Apply global provider filter if provided
	if len(providerFilter) > 0 {
		// If no providers were discovered, honor the filter directly (test convenience)
		if len(providers) == 0 {
			return providerFilter
		}
		return e.applyProviderFilter(providers, providerFilter)
	}

	return providers
}

// applyProviderFilter filters providers by the provided list, preserving order from input slice.
func (e *Engine) applyProviderFilter(providers, filter []string) []string {
	if len(filter) == 0 {
		return providers
	}
	filterSet := make(map[string]struct{}, len(filter))
	for _, p := range filter {
		filterSet[p] = struct{}{}
	}

	var filtered []string
	for _, p := range providers {
		if _, ok := filterSet[p]; ok {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

// intersectProviders returns the intersection of scenario providers and filter
func (e *Engine) intersectProviders(scenarioProviders, providerFilter []string) []string {
	filterSet := make(map[string]bool)
	for _, p := range providerFilter {
		filterSet[p] = true
	}

	var finalProviders []string
	for _, p := range scenarioProviders {
		if filterSet[p] {
			finalProviders = append(finalProviders, p)
		}
	}
	return finalProviders
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
	runEmitter := e.createRunEmitter(runID, combo)

	// Get Arena state store
	arenaStore, ok := e.stateStore.(*statestore.ArenaStateStore)
	if !ok {
		return runID, fmt.Errorf("statestore is not ArenaStateStore")
	}

	// Helper to save error metadata
	saveError := func(errMsg string) (string, error) {
		return e.saveRunError(ctx, arenaStore, combo, runID, startTime, errMsg, runEmitter)
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
		EventBus: e.eventBus,
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

	// Calculate duration and cost
	duration := time.Since(startTime)
	cost := convResult.Cost.TotalCost

	// Save run metadata to StateStore
	if err := e.saveRunMetadata(ctx, arenaStore, combo, convResult, runID, startTime, duration); err != nil {
		return runID, fmt.Errorf("failed to save run metadata: %w", err)
	}

	e.notifyRunCompletion(runEmitter, convResult, runID, duration, cost)

	return runID, nil
}

func (e *Engine) createRunEmitter(runID string, combo RunCombination) *events.Emitter {
	if e.eventBus == nil {
		return nil
	}
	emitter := events.NewEmitter(e.eventBus, runID, "", runID)
	emitter.EmitCustom(
		events.EventType("arena.run.started"),
		"ArenaEngine",
		"run_started",
		map[string]interface{}{
			"scenario": combo.ScenarioID,
			"provider": combo.ProviderID,
			"region":   combo.Region,
		},
		fmt.Sprintf("Run %s started", runID),
	)
	return emitter
}

func (e *Engine) saveRunError(
	ctx context.Context,
	store *statestore.ArenaStateStore,
	combo RunCombination,
	runID string,
	start time.Time,
	errMsg string,
	runEmitter *events.Emitter,
) (string, error) {
	metadata := &statestore.RunMetadata{
		RunID:      runID,
		Region:     combo.Region,
		ScenarioID: combo.ScenarioID,
		ProviderID: combo.ProviderID,
		StartTime:  start,
		EndTime:    time.Now(),
		Duration:   time.Since(start),
		Error:      errMsg,
	}
	if err := store.SaveMetadata(ctx, runID, metadata); err != nil {
		return runID, fmt.Errorf("failed to save error metadata: %w", err)
	}

	if runEmitter != nil {
		runEmitter.EmitCustom(
			events.EventType("arena.run.failed"),
			"ArenaEngine",
			"run_failed",
			map[string]interface{}{
				"error": errMsg,
			},
			fmt.Sprintf("Run %s failed", runID),
		)
	}

	return runID, nil
}

func (e *Engine) saveRunMetadata(
	ctx context.Context,
	store *statestore.ArenaStateStore,
	combo RunCombination,
	result *ConversationResult,
	runID string,
	start time.Time,
	duration time.Duration,
) error {
	metadata := &statestore.RunMetadata{
		RunID:                        runID,
		Region:                       combo.Region,
		ScenarioID:                   combo.ScenarioID,
		ProviderID:                   combo.ProviderID,
		StartTime:                    start,
		EndTime:                      time.Now(),
		Duration:                     duration,
		Error:                        result.Error,
		SelfPlay:                     result.SelfPlay,
		PersonaID:                    result.PersonaID,
		ConversationAssertionResults: result.ConversationAssertionResults,
	}

	logger.Debug("Saving run metadata",
		"runID", runID,
		"conv_assertions_count", len(result.ConversationAssertionResults),
		"conv_assertions_results", result.ConversationAssertionResults)

	return store.SaveMetadata(ctx, runID, metadata)
}

func (e *Engine) notifyRunCompletion(
	runEmitter *events.Emitter,
	result *ConversationResult,
	runID string,
	duration time.Duration,
	cost float64,
) {
	if runEmitter == nil {
		return
	}
	if result.Error != "" {
		runEmitter.EmitCustom(
			events.EventType("arena.run.failed"),
			"ArenaEngine",
			"run_failed",
			map[string]interface{}{
				"error": result.Error,
			},
			fmt.Sprintf("Run %s failed", runID),
		)
		return
	}
	runEmitter.EmitCustom(
		events.EventType("arena.run.completed"),
		"ArenaEngine",
		"run_completed",
		map[string]interface{}{
			"duration": duration,
			"cost":     cost,
		},
		fmt.Sprintf("Run %s completed", runID),
	)
}

// generateRunID creates a unique run ID for a combination
func generateRunID(combo RunCombination) string {
	timestamp := time.Now().Format("2006-01-02T15-04Z")
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s_%s_%s", combo.Region, combo.ScenarioID, combo.ProviderID)))
	return fmt.Sprintf("%s_%s_%s_%s_%x", timestamp, combo.ProviderID, combo.Region, combo.ScenarioID, hash[:4])
}
