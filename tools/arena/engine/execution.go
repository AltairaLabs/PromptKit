package engine

import (
	"context"
	"crypto/sha256"
	"fmt"
	"maps"
	"path/filepath"
	"sort"
	"sync/atomic"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	runtimestore "github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/telemetry"
	"github.com/AltairaLabs/PromptKit/tools/arena/adapters"
	"github.com/AltairaLabs/PromptKit/tools/arena/artifacts"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

// DefaultRunTimeout is the default maximum duration for a single run execution.
// If a provider call hangs, the run will be canceled after this timeout,
// freeing up the semaphore slot for other runs.
const DefaultRunTimeout = 5 * time.Minute

// GenerateRunPlan creates a comprehensive test execution plan from filter criteria.
// The plan contains all combinations of regions × providers × scenarios OR evals that match
// the provided filters. Scenarios and evals are mutually exclusive.
//
// For scenarios:
// - regionFilter: Empty = all regions from prompt configs (or default)
// - providerFilter: Empty = all registered providers (or scenario-specified providers)
// - scenarioFilter: Empty = all loaded scenarios
//
// For evals:
// - evalFilter: Empty = all loaded evals
// - Regions and providers are not used (they come from recordings)
//
// Provider selection logic (scenarios only):
// 1. If scenario specifies providers: use those (intersected with CLI filter if provided)
// 2. If scenario doesn't specify providers: use all arena providers (intersected with CLI filter)
//
// Returns a RunPlan containing all matching combinations, ready for execution.
// Each combination represents one independent test run that will be executed
// and validated separately.
func (e *Engine) GenerateRunPlan(regionFilter, providerFilter, scenarioFilter, evalFilter []string) (*RunPlan, error) {
	// If eval filter specified, generate eval combinations
	if len(evalFilter) > 0 || (len(scenarioFilter) == 0 && len(e.scenarios) == 0 && len(e.evals) > 0) {
		return e.generateEvalPlan(evalFilter)
	}

	// Otherwise generate scenario combinations
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

// resolveScenarios returns the scenario IDs to use, applying all scenarios if empty.
// Results are sorted for deterministic ordering when iterating over maps.
func (e *Engine) resolveScenarios(scenarioFilter []string) ([]string, error) {
	if len(scenarioFilter) > 0 {
		return scenarioFilter, nil
	}

	var scenarioIDs []string
	for id := range e.scenarios {
		scenarioIDs = append(scenarioIDs, id)
	}
	sort.Strings(scenarioIDs)
	return scenarioIDs, nil
}

// generateEvalPlan creates a run plan for evaluations.
// Uses the adapter registry to enumerate recordings from source patterns.
// This allows adapters to handle glob patterns, database queries, or other enumeration strategies.
func (e *Engine) generateEvalPlan(evalFilter []string) (*RunPlan, error) {
	evalIDs := e.resolveEvals(evalFilter)

	var combinations []RunCombination
	for _, evalID := range evalIDs {
		eval, exists := e.evals[evalID]
		if !exists {
			return nil, fmt.Errorf("eval not found: %s", evalID)
		}

		// Use adapter registry to enumerate recordings
		refs, err := e.enumerateRecordings(eval.Recording.Path, eval.Recording.Type)
		if err != nil {
			return nil, fmt.Errorf("failed to enumerate recordings for eval %s: %w", evalID, err)
		}

		for _, ref := range refs {
			combinations = append(combinations, RunCombination{
				EvalID:       evalID,
				RecordingRef: ref.ID,
				// Region and ProviderID not used for evals
			})
		}
	}

	return &RunPlan{Combinations: combinations}, nil
}

// enumerateRecordings uses the adapter registry to expand recording sources.
// If no adapter registry is configured, returns a single reference for the source.
func (e *Engine) enumerateRecordings(source, typeHint string) ([]adapters.RecordingReference, error) {
	if e.adapterRegistry == nil {
		// No registry configured - return single reference
		return []adapters.RecordingReference{{
			ID:       source,
			Source:   source,
			TypeHint: typeHint,
		}}, nil
	}

	refs, err := e.adapterRegistry.Enumerate(source, typeHint)
	if err != nil {
		return nil, err
	}

	if len(refs) > 1 {
		logger.Info("enumerated recording sources",
			"source", source,
			"count", len(refs))
	}

	return refs, nil
}

// resolveEvals returns the eval IDs to use, applying all evals if empty.
// Results are sorted for deterministic ordering when iterating over maps.
func (e *Engine) resolveEvals(evalFilter []string) []string {
	if len(evalFilter) > 0 {
		return evalFilter
	}

	var evalIDs []string
	for id := range e.evals {
		evalIDs = append(evalIDs, id)
	}
	sort.Strings(evalIDs)
	return evalIDs
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

// generateScenarioCombinations creates combinations for a specific scenario.
// When a scenario has Trials > 1, each provider combo is expanded into N trial entries.
func (e *Engine) generateScenarioCombinations(region, scenarioID string, providerFilter []string) ([]RunCombination, error) {
	scenario, exists := e.scenarios[scenarioID]
	if !exists {
		return nil, fmt.Errorf("scenario %s not found", scenarioID)
	}

	providers := e.resolveProvidersForScenario(scenario, providerFilter)
	trials := scenario.Trials
	if trials < 1 {
		trials = 1
	}

	// Compute perturbation variants (if any)
	perturbationVariants := ExpandPerturbations(scenario)
	perturbationCount := len(perturbationVariants)
	if perturbationCount == 0 {
		perturbationCount = 1 // No perturbations = single run
	}

	// Total trials = explicit trials × perturbation variants
	totalTrials := trials * perturbationCount

	var combinations []RunCombination
	for _, providerID := range providers {
		for t := range totalTrials {
			combinations = append(combinations, RunCombination{
				Region:            region,
				ScenarioID:        scenarioID,
				ProviderID:        providerID,
				TrialIndex:        t,
				TotalTrials:       totalTrials,
				PerturbationIndex: perturbationIndexForTrial(t, perturbationCount),
			})
		}
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

	// Filter by required capabilities if specified
	if len(scenario.RequiredCapabilities) > 0 {
		providers = e.filterByCapabilities(providers, scenario.RequiredCapabilities)
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

// filterByCapabilities filters providers to only those that have all required capabilities
func (e *Engine) filterByCapabilities(providers, required []string) []string {
	var result []string
	for _, id := range providers {
		caps := e.config.ProviderCapabilities[id]
		if hasAllCapabilities(caps, required) {
			result = append(result, id)
		}
	}
	return result
}

// hasAllCapabilities checks if the provider has all required capabilities
func hasAllCapabilities(have, need []string) bool {
	haveSet := make(map[string]struct{}, len(have))
	for _, c := range have {
		haveSet[c] = struct{}{}
	}
	for _, c := range need {
		if _, ok := haveSet[c]; !ok {
			return false
		}
	}
	return true
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

// executeRun executes a single test combination: scenario OR eval.
// This is the core execution method that coordinates all aspects of running one test:
//
// For scenario-based runs:
// 1. Validates that the scenario and provider exist
// 2. Resolves the task type and builds the system prompt for the region
// 3. Delegates to ConversationExecutor for turn-by-turn execution
// 4. Collects results including conversation transcript, costs, timing, tool calls
// 5. Runs validators and captures validation results
// 6. Saves results to StateStore
//
// For eval-based runs:
// 1. Validates that the eval config exists
// 2. Loads the recording via adapter registry
// 3. Applies assertions to the saved conversation
// 4. Saves evaluation results to StateStore
//
// Handles errors gracefully, always returning a RunID (with Error saved in StateStore if failed).
// Returns the RunID. Results can be retrieved from StateStore using GetResult().
func (e *Engine) executeRun(ctx context.Context, combo RunCombination) (string, error) {
	// Defer per-run timeout setup until after scenario lookup so duplex
	// scenarios can stretch the deadline to their declared
	// `duplex.timeout` (typically 10m for voice). For the eval-run path
	// and the no-scenario edge case we fall through to the config /
	// DefaultRunTimeout ladder.
	startTime := time.Now()
	runID := generateRunID(combo)
	runEmitter := e.createRunEmitter(ctx, runID, &combo)

	// Get Arena state store
	arenaStore, ok := e.stateStore.(*statestore.ArenaStateStore)
	if !ok {
		return runID, fmt.Errorf("statestore is not ArenaStateStore")
	}

	// Helper to save error metadata
	saveError := func(errMsg string) (string, error) {
		return e.saveRunError(ctx, arenaStore, combo, runID, startTime, errMsg, runEmitter)
	}

	// Check if this is an eval run
	if combo.EvalID != "" {
		runTimeout := e.resolveRunTimeout(nil)
		runCtx, cancel := context.WithTimeout(ctx, runTimeout)
		defer cancel()
		return e.executeEvalRun(runCtx, combo, runID, startTime, arenaStore, runEmitter, saveError)
	}

	// Otherwise it's a scenario run
	// Get scenario
	scenario, exists := e.scenarios[combo.ScenarioID]
	if !exists {
		return saveError(fmt.Sprintf("scenario not found: %s", combo.ScenarioID))
	}

	// Apply per-run timeout, now that we know the scenario shape.
	runTimeout := e.resolveRunTimeout(scenario)
	runCtx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()

	// Prepare workflow scenarios: shallow-copy to avoid mutating the shared
	// scenario, then set TaskType from state machine and wire metadata.
	// Register per-run memory scope and seed memories if configured
	if e.memoryStore != nil {
		scope := e.registerMemoryForRun(combo.ScenarioID, runID)
		if err := e.seedMemoriesForRun(scenario, scope); err != nil {
			return saveError(fmt.Sprintf("failed to seed memories: %v", err))
		}
	}

	var workflowOrch *EvalOrchestrator
	if e.workflowSpec != nil {
		wfScenario := *scenario
		scenario = &wfScenario
		var prepErr error
		workflowOrch, prepErr = e.prepareWorkflowScenario(scenario, runID)
		if prepErr != nil {
			return saveError(prepErr.Error())
		}
		runCtx = withWorkflowScenarioID(runCtx, runID)
		defer e.workflowTransExec.UnregisterRun(runID)
	}

	// Open scenario + session scoped MCPSource entries; cleanup runs via defer.
	// The returned context carries a per-run forked MCP registry so the
	// pipeline's MCPExecutor routes to this run's session-scoped servers
	// rather than the engine's shared registry.
	mcpRunCtx, mcpCleanup, mcpErr := e.openScenarioSessionMCPSources(runCtx, scenario, combo.ScenarioID, runID)
	if mcpErr != nil {
		return saveError(mcpErr.Error())
	}
	defer mcpCleanup()
	runCtx = mcpRunCtx

	// Get provider
	provider, exists := e.providerRegistry.Get(combo.ProviderID)
	if !exists {
		return saveError(fmt.Sprintf("provider not found: %s", combo.ProviderID))
	}

	// Apply perturbation substitutions if this run has a perturbation variant
	execScenario := scenario
	if combo.PerturbationIndex >= 0 {
		variants := ExpandPerturbations(scenario)
		if combo.PerturbationIndex < len(variants) {
			perturbed := *scenario
			perturbed.Turns = ApplyPerturbation(scenario.Turns, variants[combo.PerturbationIndex])
			execScenario = &perturbed
		}
	}

	// Clone the eval orchestrator for this run so concurrent runs don't race
	// on the shared runner's emitter. Workflow runs already get a clone via
	// prepareWorkflowScenario; non-workflow runs need one too.
	runOrch := workflowOrch
	if runOrch == nil && e.evalOrchestrator != nil {
		runOrch = e.evalOrchestrator.Clone()
	}

	// Build a per-run AudioRouter when monitoring is enabled and the
	// scenario is duplex. The engine doesn't own host playback — the router
	// is published to subscribers (web SSE relay, audio Monitor) via the
	// AudioMonitorHook, and they decide what to do with it.
	audioRouter := e.buildAudioMonitor(execScenario)
	defer func() {
		if audioRouter != nil {
			audioRouter.Close()
		}
	}()

	if audioRouter != nil && e.audioMonitorOpts != nil {
		e.fireAudioMonitorHooks(runID, audioRouter, e.audioMonitorOpts.Rate)
	}

	// Fire SessionHook.OnSessionStart now that sandbox sources are open and
	// the run context is fully prepared. The registry is nil-safe — if no
	// hooks are registered this is a complete no-op.
	sessionMeta := e.sessionEventMetadata(runCtx, runID)
	_ = e.sessionHooks.RunSessionStart(runCtx, hooks.SessionEvent{
		SessionID:      runID,
		ConversationID: runID,
		Metadata:       sessionMeta,
	})

	// Thread the session hook registry into the run context so per-turn
	// update events can be fired deep inside the conversation executor
	// without needing extra plumbing parameters.
	runCtx = withSessionHookContext(runCtx, e.sessionHooks, runID, sessionMeta)

	// Execute conversation
	req := ConversationRequest{
		Provider:         provider,
		Scenario:         execScenario,
		Config:           e.config,
		Region:           combo.Region,
		RunID:            runID,
		EventBus:         e.eventBus,
		EvalOrchestrator: runOrch,
		RecordingConfig:  e.recordingConfig,
		EventStore:       e.eventStore,
		AudioRouter:      audioRouter,
	}
	// Wire deferred workflow transition commit and per-run skill filtering
	if e.workflowTransExec != nil {
		e.wireWorkflowHooks(&req, runID)
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

	convResult := e.conversationExecutor.ExecuteConversation(runCtx, req)

	// Fire SessionHook.OnSessionEnd after the conversation completes.
	// convResult is never nil here — ExecuteConversation always returns a value.
	_ = e.sessionHooks.RunSessionEnd(runCtx, hooks.SessionEvent{
		SessionID:      runID,
		ConversationID: runID,
		Messages:       convResult.Messages,
		Metadata:       sessionMeta,
	})

	// Ingest any artifacts a hook staged this run through the artifact store.
	// The local backend leaves bytes in place; a remote backend would upload.
	// Best-effort — never fail a run over artifact persistence.
	if e.outputDir != "" {
		store := e.artifactStore
		if store == nil {
			store = artifacts.NewLocal()
		}
		dir := filepath.Join(e.outputDir, "artifacts", runID)
		if ingestErr := artifacts.Ingest(runCtx, store, dir, runID); ingestErr != nil {
			logger.Warn("artifact ingestion failed", "run", runID, "error", ingestErr)
		}
	}

	// Run timed out, but the executor may have produced a partial result
	// (messages exchanged, conversation_assertions evaluated on what
	// happened so far). Discarding it and synthesizing an error-only
	// record loses signals that are useful even when the conversation
	// didn't complete — e.g. an audio_emotion assertion on whichever
	// turns DID happen, or tool-call counts up to the point of timeout.
	// Stamp the timeout onto the result so the run is flagged as errored
	// but the partial content survives to the report.
	if runCtx.Err() != nil {
		timeoutMsg := fmt.Sprintf("run timed out after %s", runTimeout)
		if convResult == nil || convResult.Messages == nil {
			// No partial content to preserve — fall back to synthetic error.
			if convResult != nil && convResult.Error != "" {
				timeoutMsg = fmt.Sprintf("%s: %s", timeoutMsg, convResult.Error)
			}
			return saveError(timeoutMsg)
		}
		if convResult.Error != "" {
			convResult.Error = fmt.Sprintf("%s: %s", timeoutMsg, convResult.Error)
		} else {
			convResult.Error = timeoutMsg
		}
		convResult.Failed = true
	}

	// Enrich conversation messages with tool descriptor metadata for reports
	e.enrichMessagesWithToolDescriptors(runCtx, arenaStore, runID)

	// Enrich conversation messages with workflow state metadata for reports
	if e.workflowTransExec != nil {
		e.enrichMessagesWithWorkflowState(runCtx, arenaStore, runID)
	}

	// Calculate duration and cost
	duration := time.Since(startTime)
	cost := convResult.Cost.TotalCost

	// Save run metadata to StateStore
	if err := e.saveRunMetadata(runCtx, arenaStore, combo, convResult, runID, startTime, duration); err != nil {
		return runID, fmt.Errorf("failed to save run metadata: %w", err)
	}

	e.notifyRunCompletion(runEmitter, convResult, runID, duration, cost)

	return runID, nil
}

// enrichMessagesWithToolDescriptors adds _available_tools and _tool_descriptors
// metadata to the system prompt message in the state store. This makes tool
// information visible in the HTML report for non-workflow (pipeline) runs.
func (e *Engine) enrichMessagesWithToolDescriptors(
	ctx context.Context,
	store *statestore.ArenaStateStore,
	conversationID string,
) {
	if e.toolRegistry == nil {
		return
	}

	allTools := e.toolRegistry.GetTools()
	if len(allTools) == 0 {
		return
	}

	// Build tool names and descriptors
	toolNames := make([]string, 0, len(allTools))
	toolDescriptors := make([]map[string]interface{}, 0, len(allTools))
	for name, td := range allTools {
		toolNames = append(toolNames, name)
		desc := map[string]interface{}{
			"name":        td.Name,
			"description": td.Description,
		}
		if len(td.InputSchema) > 0 {
			desc["input_schema"] = td.InputSchema
		}
		toolDescriptors = append(toolDescriptors, desc)
	}

	// Load conversation, enrich system prompt message, re-save
	convState, err := store.Load(ctx, conversationID)
	if err != nil || len(convState.Messages) == 0 {
		return
	}

	enriched := false
	for i := range convState.Messages {
		if convState.Messages[i].Role != "system" {
			continue
		}
		if convState.Messages[i].Meta == nil {
			convState.Messages[i].Meta = map[string]interface{}{}
		}
		convState.Messages[i].Meta["_available_tools"] = toolNames
		convState.Messages[i].Meta["_tool_descriptors"] = toolDescriptors
		enriched = true
		break // only enrich the first system prompt
	}

	if enriched {
		if err := store.Save(ctx, convState); err != nil {
			logger.Warn("Failed to save enriched tool descriptors to state store",
				"conversation_id", convState.ID,
				"error", err)
		}
	}
}

func (e *Engine) createRunEmitter(ctx context.Context, runID string, combo *RunCombination) *events.Emitter {
	if e.eventBus == nil {
		return nil
	}

	// Start OTel session if tracing is configured via SetTracerProvider.
	// This creates a root span parented under the caller's context.
	if e.otelListener != nil {
		e.otelListener.StartSession(ctx, runID, telemetry.AgentInfo{
			Name: combo.ScenarioID,
			ID:   combo.ProviderID,
		})
	}

	// Use runID as sessionID for session recording - this ensures events are
	// associated with the recording file for this run
	emitter := events.NewEmitter(e.eventBus, runID, runID, runID)
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
		Labels:     e.scenarioLabels(combo.ScenarioID),
		StartTime:  start,
		EndTime:    time.Now(),
		Duration:   time.Since(start),
		Error:      errMsg,
		A2AAgents:  e.getA2AAgentsFromConfig(),
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

// scenarioLabels returns a copy of the named scenario's metadata labels, or
// nil if the scenario is unknown or has no labels. Returning a copy keeps the
// statestore independent of the in-memory scenario map.
func (e *Engine) scenarioLabels(scenarioID string) map[string]string {
	scenario, ok := e.scenarios[scenarioID]
	if !ok {
		return nil
	}
	return maps.Clone(scenario.Labels)
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
		Labels:                       e.scenarioLabels(combo.ScenarioID),
		StartTime:                    start,
		EndTime:                      time.Now(),
		Duration:                     duration,
		Error:                        result.Error,
		Skipped:                      result.Skipped,
		SkipReason:                   result.SkipReason,
		SelfPlay:                     result.SelfPlay,
		PersonaID:                    result.PersonaID,
		RecordingPath:                e.GetRecordingPath(runID),
		ConversationAssertionResults: result.ConversationAssertionResults,
		EvalResults:                  result.EvalResults,
		A2AAgents:                    e.getA2AAgentsFromConfig(),
	}

	logger.Debug("Saving run metadata",
		"run_id", runID,
		"recording_path", metadata.RecordingPath,
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
	// End OTel session if tracing is configured via SetTracerProvider
	if e.otelListener != nil {
		e.otelListener.EndSession(runID)
	}

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

// executeEvalRun executes an evaluation run (recording replay with assertions)
func (e *Engine) executeEvalRun(
	ctx context.Context,
	combo RunCombination,
	runID string,
	startTime time.Time,
	arenaStore *statestore.ArenaStateStore,
	runEmitter *events.Emitter,
	saveError func(string) (string, error),
) (string, error) {
	// Get eval config
	eval, exists := e.evals[combo.EvalID]
	if !exists {
		return saveError(fmt.Sprintf("eval not found: %s", combo.EvalID))
	}

	// For batch evals with specific recording references, create a copy with the overridden path
	evalToUse := eval
	recordingPath := eval.Recording.Path
	if combo.RecordingRef != "" && combo.RecordingRef != eval.Recording.Path {
		evalCopy := *eval
		evalCopy.Recording = config.RecordingSource{
			Path: combo.RecordingRef,
			Type: eval.Recording.Type,
		}
		evalToUse = &evalCopy
		recordingPath = combo.RecordingRef
	}

	// Execute evaluation (no provider needed - comes from recording)
	req := ConversationRequest{
		Eval:     evalToUse,
		Config:   e.config,
		Region:   "default", // Not used for evals
		RunID:    runID,
		EventBus: e.eventBus,
		StateStoreConfig: &StateStoreConfig{
			Store: e.stateStore,
			Metadata: map[string]interface{}{
				"eval_id":        combo.EvalID,
				"recording_path": recordingPath,
				"started_at":     startTime.Format(time.RFC3339),
			},
		},
		ConversationID: runID,
	}

	convResult := e.conversationExecutor.ExecuteConversation(ctx, req)

	// Calculate duration and cost
	duration := time.Since(startTime)
	cost := convResult.Cost.TotalCost

	// Save run metadata to StateStore (use EvalID in place of ScenarioID)
	if err := e.saveEvalMetadata(ctx, arenaStore, combo, convResult, runID, startTime, duration); err != nil {
		return runID, fmt.Errorf("failed to save eval metadata: %w", err)
	}

	e.notifyRunCompletion(runEmitter, convResult, runID, duration, cost)

	return runID, nil
}

// saveEvalMetadata saves evaluation run results to the state store
func (e *Engine) saveEvalMetadata(
	ctx context.Context,
	store *statestore.ArenaStateStore,
	combo RunCombination,
	convResult *ConversationResult,
	runID string,
	startTime time.Time,
	duration time.Duration,
) error {
	// Save messages to the arena state first
	state := &runtimestore.ConversationState{
		ID:       runID,
		Messages: convResult.Messages,
		Metadata: make(map[string]interface{}),
	}

	// Store the conversation state
	if err := store.Save(ctx, state); err != nil {
		return fmt.Errorf("failed to save conversation state: %w", err)
	}

	// Then save the metadata
	metadata := &statestore.RunMetadata{
		RunID:                        runID,
		ScenarioID:                   combo.EvalID, // Use EvalID in ScenarioID field for storage
		ProviderID:                   "eval",       // Placeholder - actual provider is in recording
		Region:                       "default",
		StartTime:                    startTime,
		EndTime:                      startTime.Add(duration),
		Duration:                     duration,
		Error:                        convResult.Error,
		Skipped:                      convResult.Skipped,
		SkipReason:                   convResult.SkipReason,
		SelfPlay:                     convResult.SelfPlay,
		PersonaID:                    convResult.PersonaID,
		ConversationAssertionResults: convResult.ConversationAssertionResults,
		A2AAgents:                    e.getA2AAgentsFromConfig(),
	}

	if convResult.Failed {
		metadata.Error = convResult.Error
	}

	return store.SaveMetadata(ctx, runID, metadata)
}

// getA2AAgentsFromConfig safely extracts A2A agent metadata from the engine config.
func (e *Engine) getA2AAgentsFromConfig() []statestore.A2AAgentInfo {
	if e.config == nil {
		return nil
	}
	return convertA2AAgentsFromConfig(e.config.A2AAgents)
}

// convertA2AAgentsFromConfig converts config A2A agent definitions to statestore types.
func convertA2AAgentsFromConfig(agents []config.A2AAgentConfig) []statestore.A2AAgentInfo {
	if len(agents) == 0 {
		return nil
	}
	result := make([]statestore.A2AAgentInfo, len(agents))
	for i, agent := range agents {
		info := statestore.A2AAgentInfo{
			Name:        agent.Card.Name,
			Description: agent.Card.Description,
		}
		if len(agent.Card.Skills) > 0 {
			info.Skills = make([]statestore.A2ASkillInfo, len(agent.Card.Skills))
			for j, skill := range agent.Card.Skills {
				info.Skills[j] = statestore.A2ASkillInfo{
					ID:          skill.ID,
					Name:        skill.Name,
					Description: skill.Description,
					Tags:        skill.Tags,
				}
			}
		}
		result[i] = info
	}
	return result
}

// resolveRunTimeout returns the per-run timeout. Precedence (highest first):
//  1. Scenario.Duplex.Timeout — voice scenarios are inherently slow
//     (selfplay LLM + TTS + realtime agent + tool round-trips); a 5m
//     default truncates them before any meaningful scoring happens.
//  2. Defaults.RunTimeout from arena config.
//  3. DefaultRunTimeout (5m).
//
// The scenario parameter is optional — eval runs and other non-scenario
// paths pass nil and fall through to the config / default ladder.
func (e *Engine) resolveRunTimeout(scenario *config.Scenario) time.Duration {
	if scenario != nil && scenario.Duplex != nil {
		if d := scenario.Duplex.GetTimeoutDuration(0); d > 0 {
			return d
		}
	}
	if e.config != nil && e.config.Defaults.RunTimeout != "" {
		d, err := time.ParseDuration(e.config.Defaults.RunTimeout)
		if err == nil && d > 0 {
			return d
		}
		logger.Warn("Invalid run_timeout in config, using default",
			"configured", e.config.Defaults.RunTimeout,
			"default", DefaultRunTimeout)
	}
	return DefaultRunTimeout
}

// runIDCounter provides a monotonically increasing nonce for run ID uniqueness.
var runIDCounter atomic.Uint64

// generateRunID creates a unique run ID for a combination.
// Includes seconds and an atomic counter for guaranteed uniqueness within a process.
func generateRunID(combo RunCombination) string {
	timestamp := time.Now().Format("2006-01-02T15-04-05Z")
	nonce := runIDCounter.Add(1)
	if combo.EvalID != "" {
		hash := sha256.Sum256([]byte(fmt.Sprintf("eval_%s", combo.EvalID)))
		return fmt.Sprintf("%s_eval_%s_%x_%04x", timestamp, combo.EvalID, hash[:4], nonce)
	}
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s_%s_%s", combo.Region, combo.ScenarioID, combo.ProviderID)))
	return fmt.Sprintf("%s_%s_%s_%s_%x_%04x", timestamp, combo.ProviderID, combo.Region, combo.ScenarioID, hash[:4], nonce)
}
