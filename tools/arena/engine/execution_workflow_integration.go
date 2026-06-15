package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/composition"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/skills"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/runtime/workflow"
	arenastore "github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

const roleSystem = "system"

// resolveWorkflowStartState maps a scenario's task_type to the workflow state it
// should start in. An empty task_type defaults to the workflow entry. A task_type
// matching the entry state's prompt_task resolves to the entry (which also
// disambiguates when several states share a prompt_task). A task_type matching
// exactly one non-entry state pins that state — enabling single-stage unit tests.
// No match, or an ambiguous non-entry match, is a hard error so the previously
// silent override surfaces as a clear config failure.
func resolveWorkflowStartState(spec *workflow.Spec, taskType string) (string, error) {
	if taskType == "" {
		return spec.Entry, nil
	}
	if entry := spec.States[spec.Entry]; entry != nil && entry.PromptTask == taskType {
		return spec.Entry, nil
	}

	var matches []string
	for name, st := range spec.States {
		if st != nil && st.PromptTask == taskType {
			matches = append(matches, name)
		}
	}
	sort.Strings(matches)

	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf(
			"scenario task_type %q matches no workflow state's prompt_task (valid: %s)",
			taskType, strings.Join(workflowPromptTasks(spec), ", "))
	default:
		return "", fmt.Errorf(
			"scenario task_type %q is ambiguous: matches workflow states %s (give them distinct prompt_task values)",
			taskType, strings.Join(matches, ", "))
	}
}

// workflowPromptTasks returns the sorted, de-duplicated prompt_task values
// declared across a workflow spec's states, for error diagnostics.
func workflowPromptTasks(spec *workflow.Spec) []string {
	seen := make(map[string]struct{}, len(spec.States))
	tasks := make([]string, 0, len(spec.States))
	for _, st := range spec.States {
		if st == nil {
			continue
		}
		if _, ok := seen[st.PromptTask]; ok {
			continue
		}
		seen[st.PromptTask] = struct{}{}
		tasks = append(tasks, st.PromptTask)
	}
	sort.Strings(tasks)
	return tasks
}

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

	// Register the per-run artifact executor + workflow__set_artifact tool when
	// any state declares artifacts. The executor dispatches to the per-run
	// state machine via the shared transExec run map.
	e.toolRegistry.RegisterExecutor(&workflowArtifactExecutor{transExec: transExec})
	workflow.RegisterArtifactTool(e.toolRegistry, spec)

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
func (e *Engine) prepareWorkflowScenario(scenario *config.Scenario, runID string) (*EvalOrchestrator, error) {
	if e.workflowSpec == nil {
		return nil, nil
	}

	// Resolve which workflow state this scenario starts in. An explicit
	// task_type pins a stage (enabling single-stage unit tests); empty defaults
	// to the entry. A task_type that names no state is a hard error rather than
	// a silent reroute through the entry.
	startState, err := resolveWorkflowStartState(e.workflowSpec, scenario.TaskType)
	if err != nil {
		return nil, fmt.Errorf("scenario %q: %w", scenario.ID, err)
	}
	if st := e.workflowSpec.States[startState]; st != nil {
		scenario.TaskType = st.PromptTask
	}

	// Build the per-run emitter once and reuse it for both the
	// workflowTransitionExecutor (which fires events on every commit, eager
	// or deferred) and the workflow metadata provider (which may eager-commit
	// during a metadata read).
	var emitter *events.Emitter
	if e.eventBus != nil {
		emitter = events.NewEmitter(e.eventBus, runID, runID, runID)
	}

	// Register a per-run state machine for concurrent scenario execution,
	// started at the resolved stage.
	if e.workflowTransExec != nil {
		e.workflowTransExec.RegisterRunAtState(runID, scenario, emitter, startState)
	}

	// Clone the eval orchestrator for this run with per-run workflow metadata.
	// This avoids data races when concurrent runs set different providers on a
	// shared orchestrator.
	var orch *EvalOrchestrator
	if e.workflowTransExec != nil {
		provider := &workflowRunMetadataProvider{
			exec:       e.workflowTransExec,
			scenarioID: runID,
			emitter:    emitter,
		}
		if e.evalOrchestrator != nil {
			orch = e.evalOrchestrator.Clone()
			orch.SetWorkflowMetadataProvider(provider)
		}

		// RFC 0010 Task 5: create a per-run composition recorder and wire it
		// so composition_* assertions can read step outputs, branch targets, and
		// parallel statuses. The same recorder is stashed on the run state (for
		// retrieval in buildTurnRequest) and set as the EvalOrchestrator's
		// CompositionMetadataProvider (so assertions see it in buildEvalContext).
		rec := stage.NewCompositionRecorder()
		e.workflowTransExec.SetCompositionRecorder(runID, rec)
		if orch != nil {
			orch.SetCompositionMetadataProvider(rec)
		}
	}

	return orch, nil
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
// the per-run skill filter into context for the next turn's tool execution,
// ActiveCompositionResolver resolves the active composition for the
// current workflow state (RFC 0010), and CompositionRecorder threads the
// per-run recorder so buildStagePipeline can wire it into CompositionStage.
func (e *Engine) wireWorkflowHooks(req *ConversationRequest, runID string) {
	req.PostTurnHook = func() error {
		var emitter *events.Emitter
		if req.EventBus != nil {
			emitter = events.NewEmitter(req.EventBus, req.RunID, req.RunID, req.ConversationID)
		}
		return e.workflowTransExec.CommitPendingTransition(runID, emitter)
	}
	req.ContextEnricher = func(ctx context.Context) context.Context {
		filter := e.workflowTransExec.SkillFilter(runID)
		if filter != "" {
			return skills.WithSkillFilter(ctx, filter)
		}
		return ctx
	}
	req.ActiveCompositionResolver = e.buildCompositionResolver(runID)
	// RFC 0010 Task 5: thread the per-run composition recorder so that
	// buildTurnRequest can stamp it onto every TurnRequest, enabling
	// NewCompositionStageWithRecorder to record step outputs per turn.
	req.CompositionRecorder = e.workflowTransExec.CompositionRecorder(runID)
}

// buildCompositionResolver returns a function that resolves the active
// *composition.Composition for the current workflow state of the given run.
// Returns nil when the current state does not have orchestration: composition,
// or when no pack/compositions are loaded.
func (e *Engine) buildCompositionResolver(runID string) func() *composition.Composition {
	return func() *composition.Composition {
		if e.config.LoadedPack == nil || len(e.config.LoadedPack.Compositions) == 0 {
			return nil
		}
		sm := e.workflowTransExec.StateMachine(runID)
		if sm == nil {
			return nil
		}
		stateName := sm.CurrentState()
		state, ok := e.workflowSpec.States[stateName]
		if !ok || state == nil || state.Orchestration != workflow.OrchestrationComposition {
			return nil
		}
		compName := state.Composition
		comp, ok := e.config.LoadedPack.Compositions[compName]
		if !ok || comp == nil {
			logger.Warn("composition state references unknown composition",
				"state", stateName, "composition", compName)
			return nil
		}
		return comp
	}
}

// setMeta sets a key on a message's Meta map, initializing if needed.
func setMeta(msg *types.Message, key string, value interface{}) {
	if msg.Meta == nil {
		msg.Meta = map[string]interface{}{}
	}
	msg.Meta[key] = value
}
