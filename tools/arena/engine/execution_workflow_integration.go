package engine

import (
	"context"
	"encoding/json"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/skills"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/runtime/workflow"
	arenastore "github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

const roleSystem = "system"

// initWorkflow parses the workflow config and registers the workflow__transition
// tool in the tool registry. Called during engine initialization when config.Workflow
// is present.
//
// The transition tool executor updates scenario.TaskType on each transition so that
// the next pipeline turn uses the correct prompt from the PromptRegistry.
func (e *Engine) initWorkflow() error {
	if e.config.Workflow == nil {
		return nil
	}

	spec, err := workflow.ParseConfig(e.config.Workflow)
	if err != nil {
		return err
	}

	// Register transition tool executor
	transExec := newWorkflowTransitionExecutor(spec, e.toolRegistry)
	e.toolRegistry.RegisterExecutor(transExec)

	// Register the transition tool descriptor for the entry state
	if entryState := spec.States[spec.Entry]; entryState != nil {
		registerTransitionTool(e.toolRegistry, entryState)
	}

	e.workflowSpec = spec
	e.workflowTransExec = transExec

	// Wire skill filtering so transitions update skill availability
	if e.skillExecutor != nil {
		transExec.skillFilterer = e.skillExecutor
	}

	return nil
}

// prepareWorkflowScenario sets up a workflow scenario for execution through
// the standard ConversationExecutor. It sets the TaskType to the current
// workflow state's prompt_task and converts Steps to Turns if needed.
//
// Called from executeRun before ConversationExecutor.ExecuteConversation().
// prepareWorkflowScenario returns a per-run EvalOrchestrator clone with the
// workflow metadata provider set. The caller should set it on the ConversationRequest.
func (e *Engine) prepareWorkflowScenario(scenario *config.Scenario, runID string) *EvalOrchestrator {
	if e.workflowSpec == nil {
		return nil
	}

	// Set TaskType to entry state's prompt_task
	if entryState := e.workflowSpec.States[e.workflowSpec.Entry]; entryState != nil {
		scenario.TaskType = entryState.PromptTask
	}

	// Register a per-run state machine for concurrent scenario execution
	if e.workflowTransExec != nil {
		e.workflowTransExec.RegisterRun(runID, scenario)
	}

	// Clone the eval orchestrator for this run with per-run workflow metadata.
	// This avoids data races when concurrent runs set different providers on a
	// shared orchestrator.
	var orch *EvalOrchestrator
	if e.workflowTransExec != nil {
		provider := &workflowRunMetadataProvider{exec: e.workflowTransExec, scenarioID: runID}
		if e.evalOrchestrator != nil {
			orch = e.evalOrchestrator.Clone()
			orch.SetWorkflowMetadataProvider(provider)
		}
	}

	return orch
}

// enrichMessagesWithWorkflowState adds _workflow_state metadata to assistant messages
// that contain workflow__transition tool calls. This makes workflow state visible in
// the HTML report's devtools panel.
func (e *Engine) enrichMessagesWithWorkflowState(
	ctx context.Context,
	store *arenastore.ArenaStateStore,
	conversationID string,
) {
	if e.workflowSpec == nil {
		return
	}

	convState, err := store.Load(ctx, conversationID)
	if err != nil {
		logger.Warn("Failed to load conversation for workflow enrichment",
			"conversation_id", conversationID, "error", err)
		return
	}
	if len(convState.Messages) == 0 {
		return
	}

	if e.enrichWorkflowMessages(convState.Messages) {
		if saveErr := store.Save(ctx, convState); saveErr != nil {
			logger.Warn("Failed to save workflow state metadata",
				"conversation_id", convState.ID, "error", saveErr)
		}
	}
}

// enrichWorkflowMessages walks messages and adds _workflow_state metadata.
func (e *Engine) enrichWorkflowMessages(messages []types.Message) bool {
	currentState := e.workflowSpec.Entry
	enriched := false
	for i := range messages {
		msg := &messages[i]
		if msg.Role == "tool" && msg.ToolResult != nil && msg.ToolResult.Name == workflow.TransitionToolName {
			if newState, ws := e.buildTransitionMeta(msg, currentState); ws != nil {
				currentState = newState
				setMeta(msg, "_workflow_state", ws)
				enriched = true
			}
		}
		if msg.Role == roleSystem && i == 0 {
			setMeta(msg, "_workflow_state", e.buildEntryStateMeta(currentState))
			enriched = true
		}
	}
	return enriched
}

// buildTransitionMeta extracts transition info from a tool result and returns
// the new state name and metadata map. Returns ("", nil) if not a transition.
//
//nolint:gocritic // unnamedResult: intentional for clarity
func (e *Engine) buildTransitionMeta(
	msg *types.Message, previousState string,
) (string, map[string]interface{}) {
	var result struct {
		NewState string `json:"new_state"`
		Event    string `json:"event"`
	}
	content := msg.ToolResult.GetTextContent()
	if json.Unmarshal([]byte(content), &result) != nil || result.NewState == "" {
		return "", nil
	}
	ws := map[string]interface{}{
		"current_state":  result.NewState,
		"previous_state": previousState,
		"transition":     result.Event,
	}
	if state := e.workflowSpec.States[result.NewState]; state != nil {
		if state.Description != "" {
			ws["description"] = state.Description
		}
		ws["terminal"] = len(state.OnEvent) == 0
	}
	return result.NewState, ws
}

// buildEntryStateMeta builds workflow metadata for the entry state system prompt.
func (e *Engine) buildEntryStateMeta(stateName string) map[string]interface{} {
	ws := map[string]interface{}{"current_state": stateName}
	if state := e.workflowSpec.States[stateName]; state != nil {
		if state.Description != "" {
			ws["description"] = state.Description
		}
		if len(state.OnEvent) > 0 {
			ws["available_events"] = state.OnEvent
		}
	}
	return ws
}

// wireWorkflowHooks sets up per-turn hooks for workflow scenarios:
// PostTurnHook commits deferred transitions, ContextEnricher injects
// the per-run skill filter into context for the next turn's tool execution.
func (e *Engine) wireWorkflowHooks(req *ConversationRequest, runID string) {
	req.PostTurnHook = func() error {
		return e.workflowTransExec.CommitPendingTransition(runID)
	}
	req.ContextEnricher = func(ctx context.Context) context.Context {
		filter := e.workflowTransExec.SkillFilter(runID)
		if filter != "" {
			return skills.WithSkillFilter(ctx, filter)
		}
		return ctx
	}
}

// setMeta sets a key on a message's Meta map, initializing if needed.
func setMeta(msg *types.Message, key string, value interface{}) {
	if msg.Meta == nil {
		msg.Meta = map[string]interface{}{}
	}
	msg.Meta[key] = value
}
