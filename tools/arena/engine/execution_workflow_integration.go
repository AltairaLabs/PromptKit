package engine

import (
	"context"
	"encoding/json"
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
	factory, getDriver := newArenaDriverFactory(provider, combo.ScenarioID, e.toolRegistry)
	executor := wf.NewExecutor(factory)

	// Execute the workflow
	result := executor.Execute(ctx, wfScenario)

	// Convert workflow result to messages + assertions for state store.
	// The driver provides state→system prompt lookup so each transition
	// shows the new system prompt in the report.
	drv := getDriver()
	messages, assertionResults := workflowResultToMessages(result, drv)

	// Evaluate conversation-level assertions (e.g. skill_activated, skill_not_activated)
	if len(scenario.ConversationAssertions) > 0 {
		convAssertionResults := evaluateWorkflowConversationAssertions(
			ctx, scenario.ConversationAssertions, messages,
		)
		assertionResults = append(assertionResults, convAssertionResults...)
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

// evaluateWorkflowConversationAssertions runs conversation-level assertions
// (e.g. skill_activated, skill_not_activated) against the full message trace.
func evaluateWorkflowConversationAssertions(
	ctx context.Context,
	configs []asrt.AssertionConfig,
	messages []types.Message,
) []asrt.ConversationValidationResult {
	// Extract tool calls from all assistant messages
	var toolCalls []asrt.ToolCallRecord
	for idx := range messages {
		msg := messages[idx]
		if len(msg.ToolCalls) == 0 {
			continue
		}
		for _, tc := range msg.ToolCalls {
			var args map[string]interface{}
			if len(tc.Args) > 0 {
				_ = json.Unmarshal(tc.Args, &args)
			}
			toolCalls = append(toolCalls, asrt.ToolCallRecord{
				TurnIndex: idx,
				ToolName:  tc.Name,
				Arguments: args,
			})
		}
	}

	convCtx := &asrt.ConversationContext{
		AllTurns:  messages,
		ToolCalls: toolCalls,
	}

	var assertions []asrt.ConversationAssertion
	for _, c := range configs {
		assertions = append(assertions, asrt.ConversationAssertion(c))
	}

	reg := asrt.NewConversationAssertionRegistry()
	return reg.ValidateConversations(ctx, assertions, convCtx)
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
			sysMsg := types.Message{Role: "system", Content: sp}
			if toolNames := drv.AvailableToolNames(); len(toolNames) > 0 {
				sysMsg.Meta = map[string]interface{}{
					"_available_tools": toolNames,
				}
			}
			messages = append(messages, sysMsg)
		}
		messages = append(messages, drv.MessageTrace()...)
	}

	return messages, allAssertions
}
