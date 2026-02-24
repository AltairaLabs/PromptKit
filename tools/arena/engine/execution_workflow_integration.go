package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	runtimestore "github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	asrt "github.com/AltairaLabs/PromptKit/tools/arena/assertions"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	wf "github.com/AltairaLabs/PromptKit/tools/arena/workflow"
)

// executeWorkflowRun executes a workflow scenario using the workflow executor.
// It converts config.Scenario → workflow.Scenario, executes via the workflow
// executor, and converts the result back to the Arena state store format.
func (e *Engine) executeWorkflowRun(
	ctx context.Context,
	combo *RunCombination,
	runID string,
	startTime time.Time,
	arenaStore *statestore.ArenaStateStore,
	runEmitter *events.Emitter,
	saveError func(string) (string, error),
) (string, error) {
	scenario, exists := e.scenarios[combo.ScenarioID]
	if !exists {
		return saveError(fmt.Sprintf("scenario not found: %s", combo.ScenarioID))
	}

	provider, exists := e.providerRegistry.Get(combo.ProviderID)
	if !exists {
		return saveError(fmt.Sprintf("provider not found: %s", combo.ProviderID))
	}

	// Convert config.Scenario → workflow.Scenario
	wfScenario := configToWorkflowScenario(scenario)

	// Create workflow executor with a driver factory for this provider
	factory, getDriver := newArenaDriverFactory(provider, combo.ScenarioID, e.toolRegistry)
	executor := wf.NewExecutor(factory)
	if e.packEvalHook != nil {
		executor.WithTurnEvalRunner(e.packEvalHook, runID)
	}

	// Execute the workflow
	result := executor.Execute(ctx, wfScenario)

	// Convert workflow result to messages + assertions for state store.
	// The driver provides state→system prompt lookup so each transition
	// shows the new system prompt in the report.
	drv := getDriver()
	messages, assertionResults := workflowResultToMessages(result, drv)

	// Evaluate conversation-level assertions (pack + scenario) via PackEvalHook
	mergedAssertionConfigs := mergeAssertionConfigs(e.config, scenario.ConversationAssertions)
	if e.packEvalHook != nil && len(mergedAssertionConfigs) > 0 {
		convAssertionResults := e.packEvalHook.RunAssertionsAsConversationResults(
			ctx, mergedAssertionConfigs, messages, len(messages)-1, runID,
			evals.TriggerOnConversationComplete,
		)
		assertionResults = append(assertionResults, convAssertionResults...)
	}

	// Run pack-level conversation evals if configured
	if e.packEvalHook != nil && e.packEvalHook.HasEvals() {
		packResults := e.packEvalHook.RunConversationEvals(ctx, messages, runID)
		assertionResults = append(assertionResults, packResults...)
	}

	// Build conversation result for metadata
	convResult := &ConversationResult{
		Messages:                     messages,
		ConversationAssertionResults: assertionResults,
	}
	if result.Failed {
		convResult.Failed = true
		convResult.Error = result.Error
	}
	// Check conversation-level assertions for failures
	for _, ar := range assertionResults {
		if !ar.Passed {
			convResult.Failed = true
			if convResult.Error == "" {
				convResult.Error = fmt.Sprintf("assertion %q failed: %s", ar.Type, ar.Message)
			}
		}
	}

	duration := time.Since(startTime)

	// Save run metadata
	metadata := &statestore.RunMetadata{
		RunID:                        runID,
		Region:                       combo.Region,
		ScenarioID:                   combo.ScenarioID,
		ProviderID:                   combo.ProviderID,
		StartTime:                    startTime,
		EndTime:                      time.Now(),
		Duration:                     duration,
		Error:                        convResult.Error,
		RecordingPath:                e.GetRecordingPath(runID),
		ConversationAssertionResults: assertionResults,
		A2AAgents:                    e.getA2AAgentsFromConfig(),
	}

	logger.Debug("Saving workflow run metadata",
		"runID", runID,
		"scenario", combo.ScenarioID,
		"final_state", result.FinalState,
		"steps", len(result.Steps),
		"failed", result.Failed,
	)

	// Save conversation messages to state store so they appear in reports
	convState := &runtimestore.ConversationState{
		ID:       runID,
		Messages: messages,
		Metadata: map[string]interface{}{
			"region":        combo.Region,
			"provider":      combo.ProviderID,
			"scenario":      combo.ScenarioID,
			"final_state":   result.FinalState,
			"system_prompt": drv.InitialSystemPrompt(),
		},
	}
	if err := arenaStore.Save(ctx, convState); err != nil {
		return runID, fmt.Errorf("failed to save workflow conversation: %w", err)
	}

	if err := arenaStore.SaveMetadata(ctx, runID, metadata); err != nil {
		return runID, fmt.Errorf("failed to save workflow run metadata: %w", err)
	}

	e.notifyRunCompletion(runEmitter, convResult, runID, duration, 0)

	return runID, nil
}

// configToWorkflowScenario converts a config.Scenario to a workflow.Scenario.
func configToWorkflowScenario(s *config.Scenario) *wf.Scenario {
	steps := make([]wf.Step, len(s.Steps))
	for i, cs := range s.Steps {
		// Convert assertions
		assertions := make([]asrt.AssertionConfig, len(cs.Assertions))
		copy(assertions, cs.Assertions)

		steps[i] = wf.Step{
			Type:       wf.StepType(cs.Type),
			Content:    cs.Content,
			Assertions: assertions,
		}
	}

	return &wf.Scenario{
		ID:                  s.ID,
		Pack:                s.Pack,
		Description:         s.Description,
		Steps:               steps,
		Variables:           s.Variables,
		ContextCarryForward: s.ContextCarryForward,
	}
}

// workflowResultToMessages returns the driver's message trace and collects all
// assertion results from the workflow execution.
// The message trace already contains: initial system prompt → user/assistant pairs
// → tool calls → tool results → new system prompts across all state transitions.
func workflowResultToMessages(
	result *wf.Result, drv *arenaWorkflowDriver,
) ([]types.Message, []asrt.ConversationValidationResult) {
	var allAssertions []asrt.ConversationValidationResult
	for _, step := range result.Steps {
		allAssertions = append(allAssertions, step.AssertionResults...)
	}

	var messages []types.Message
	if drv != nil {
		// Prepend the initial state's system prompt with available tools metadata
		if sp := drv.InitialSystemPrompt(); sp != "" {
			sysMsg := types.Message{
				Role:      "system",
				Content:   sp,
				Timestamp: time.Now(),
			}
			meta := map[string]interface{}{}
			if toolNames := drv.AvailableToolNames(); len(toolNames) > 0 {
				meta["_available_tools"] = toolNames
			}
			if toolDescs := drv.InitialToolDescriptors(); len(toolDescs) > 0 {
				meta["_tool_descriptors"] = toolDescs
			}
			if ws := drv.InitialWorkflowState(); ws != nil {
				meta["_workflow_state"] = ws
			}
			if len(meta) > 0 {
				sysMsg.Meta = meta
			}
			messages = append(messages, sysMsg)
		}
		messages = append(messages, drv.MessageTrace()...)
	}

	return messages, allAssertions
}
