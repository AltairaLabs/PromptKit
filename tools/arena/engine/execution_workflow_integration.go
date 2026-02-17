package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
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
	factory := newArenaDriverFactory(provider, combo.ScenarioID)
	executor := wf.NewExecutor(factory)

	// Execute the workflow
	result := executor.Execute(ctx, wfScenario)

	// Convert workflow result to messages + assertions for state store
	messages, assertionResults := workflowResultToMessages(wfScenario, result)

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
			Type:        wf.StepType(cs.Type),
			Content:     cs.Content,
			Event:       cs.Event,
			ExpectState: cs.ExpectState,
			Assertions:  assertions,
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

// workflowResultToMessages converts a workflow.Result and its source scenario into
// Arena messages and assertion results.
// Input steps produce user + assistant message pairs.
// Event steps produce a system message noting the transition.
func workflowResultToMessages(
	scenario *wf.Scenario, result *wf.Result,
) ([]types.Message, []asrt.ConversationValidationResult) {
	var messages []types.Message
	var allAssertions []asrt.ConversationValidationResult

	for _, step := range result.Steps {
		switch step.Type {
		case wf.StepInput:
			// Get the original user content from the scenario step
			userContent := ""
			if step.Index < len(scenario.Steps) {
				userContent = scenario.Steps[step.Index].Content
			}
			messages = append(messages,
				types.Message{Role: "user", Content: userContent},
				types.Message{Role: "assistant", Content: step.Response},
			)
		case wf.StepEvent:
			eventName := ""
			if step.Index < len(scenario.Steps) {
				eventName = scenario.Steps[step.Index].Event
			}
			messages = append(messages, types.Message{
				Role:    "system",
				Content: fmt.Sprintf("[workflow] event %q → state %q", eventName, step.State),
			})
		}

		allAssertions = append(allAssertions, step.AssertionResults...)
	}

	return messages, allAssertions
}
