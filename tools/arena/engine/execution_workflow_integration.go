package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
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
	factory, getDriver := newArenaDriverFactory(provider, combo.ScenarioID)
	executor := wf.NewExecutor(factory)

	// Execute the workflow
	result := executor.Execute(ctx, wfScenario)

	// Convert workflow result to messages + assertions for state store.
	// The driver provides state→system prompt lookup so each transition
	// shows the new system prompt in the report.
	drv := getDriver()
	messages, assertionResults := workflowResultToMessages(result, drv)

	// Build conversation result for metadata
	convResult := &ConversationResult{
		Messages:                     messages,
		ConversationAssertionResults: assertionResults,
	}
	if result.Failed {
		convResult.Failed = true
		convResult.Error = result.Error
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
		// Prepend the initial state's system prompt
		if sp := drv.InitialSystemPrompt(); sp != "" {
			messages = append(messages, types.Message{Role: "system", Content: sp})
		}
		messages = append(messages, drv.MessageTrace()...)
	}

	return messages, allAssertions
}
