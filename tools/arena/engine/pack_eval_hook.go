package engine

import (
	"context"
	"encoding/json"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
)

// PackEvalHook manages pack eval execution during Arena conversation runs.
// It wraps an EvalDispatcher and converts results into the assertion format
// used by Arena's reporting pipeline.
type PackEvalHook struct {
	dispatcher evals.EvalDispatcher
	defs       []evals.EvalDef
	adapter    *PackEvalAdapter
	taskType   string
}

// NewPackEvalHook creates a hook for executing pack evals during Arena runs.
// If skipEvals is true, a NoOpDispatcher is used internally.
// The evalTypeFilter, when non-empty, restricts execution to matching eval types.
func NewPackEvalHook(
	registry *evals.EvalTypeRegistry,
	defs []evals.EvalDef,
	skipEvals bool,
	evalTypeFilter []string,
	taskType string,
) *PackEvalHook {
	// Filter defs by eval type if filter is set
	filteredDefs := filterEvalDefs(defs, evalTypeFilter)

	var dispatcher evals.EvalDispatcher
	if skipEvals || len(filteredDefs) == 0 {
		dispatcher = &evals.NoOpDispatcher{}
	} else {
		runner := evals.NewEvalRunner(registry)
		dispatcher = evals.NewInProcDispatcher(runner, nil)
	}

	return &PackEvalHook{
		dispatcher: dispatcher,
		defs:       filteredDefs,
		adapter:    &PackEvalAdapter{},
		taskType:   taskType,
	}
}

// HasEvals returns true if there are eval defs to execute.
func (h *PackEvalHook) HasEvals() bool {
	if h == nil {
		return false
	}
	return len(h.defs) > 0
}

// RunTurnEvals runs turn-triggered evals after a turn completes.
// Returns converted ConversationValidationResult entries.
func (h *PackEvalHook) RunTurnEvals(
	ctx context.Context,
	messages []types.Message,
	turnIndex int,
	sessionID string,
) []assertions.ConversationValidationResult {
	if h == nil || !h.HasEvals() {
		return nil
	}

	evalCtx := h.buildEvalContext(messages, turnIndex, sessionID)
	results, _ := h.dispatcher.DispatchTurnEvals(ctx, h.defs, evalCtx)
	return h.adapter.Convert(results)
}

// RunSessionEvals runs session-complete evals after conversation finishes.
// Returns converted ConversationValidationResult entries.
func (h *PackEvalHook) RunSessionEvals(
	ctx context.Context,
	messages []types.Message,
	sessionID string,
) []assertions.ConversationValidationResult {
	if h == nil || !h.HasEvals() {
		return nil
	}

	turnIndex := len(messages) - 1
	if turnIndex < 0 {
		turnIndex = 0
	}
	evalCtx := h.buildEvalContext(messages, turnIndex, sessionID)
	results, _ := h.dispatcher.DispatchSessionEvals(ctx, h.defs, evalCtx)
	return h.adapter.Convert(results)
}

// RunConversationEvals runs conversation-complete evals after all turns finish.
// Returns converted ConversationValidationResult entries.
func (h *PackEvalHook) RunConversationEvals(
	ctx context.Context,
	messages []types.Message,
	sessionID string,
) []assertions.ConversationValidationResult {
	if h == nil || !h.HasEvals() {
		return nil
	}

	turnIndex := len(messages) - 1
	if turnIndex < 0 {
		turnIndex = 0
	}
	evalCtx := h.buildEvalContext(messages, turnIndex, sessionID)
	results, _ := h.dispatcher.DispatchConversationEvals(ctx, h.defs, evalCtx)
	return h.adapter.Convert(results)
}

// RunAssertionsAsEvals converts assertion configs to EvalDefs and runs them
// through the dispatcher. Returns raw EvalResults (not converted to assertion format).
// The trigger parameter overrides the default trigger on each converted def.
func (h *PackEvalHook) RunAssertionsAsEvals(
	ctx context.Context,
	assertionConfigs []assertions.AssertionConfig,
	messages []types.Message,
	turnIndex int,
	sessionID string,
	trigger evals.EvalTrigger,
) []evals.EvalResult {
	if h == nil || len(assertionConfigs) == 0 {
		return nil
	}

	defs := make([]evals.EvalDef, len(assertionConfigs))
	for i, cfg := range assertionConfigs {
		defs[i] = cfg.ToEvalDef(i)
		defs[i].Trigger = trigger
	}

	evalCtx := h.buildEvalContext(messages, turnIndex, sessionID)

	var results []evals.EvalResult
	var err error
	switch trigger { //nolint:exhaustive // Only conversation and turn triggers are meaningful here
	case evals.TriggerOnConversationComplete:
		results, err = h.dispatcher.DispatchConversationEvals(ctx, defs, evalCtx)
	case evals.TriggerEveryTurn:
		results, err = h.dispatcher.DispatchTurnEvals(ctx, defs, evalCtx)
	default:
		results, err = h.dispatcher.DispatchTurnEvals(ctx, defs, evalCtx)
	}
	if err != nil {
		return nil
	}
	return results
}

// RunAssertionsAsConversationResults converts assertion configs to EvalDefs,
// runs them through the dispatcher, and wraps results in ConversationValidationResult.
func (h *PackEvalHook) RunAssertionsAsConversationResults(
	ctx context.Context,
	assertionConfigs []assertions.AssertionConfig,
	messages []types.Message,
	turnIndex int,
	sessionID string,
	trigger evals.EvalTrigger,
) []assertions.ConversationValidationResult {
	if h == nil {
		return nil
	}
	results := h.RunAssertionsAsEvals(ctx, assertionConfigs, messages, turnIndex, sessionID, trigger)
	return h.adapter.Convert(results)
}

// buildEvalContext constructs an EvalContext from Arena messages.
func (h *PackEvalHook) buildEvalContext(
	messages []types.Message,
	turnIndex int,
	sessionID string,
) *evals.EvalContext {
	// Extract current output from the last assistant message
	var currentOutput string
	toolCalls := extractToolCalls(messages)

	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			currentOutput = messages[i].Content
			break
		}
	}

	extras := extractWorkflowExtras(messages)

	return &evals.EvalContext{
		Messages:      convertToEvalMessages(messages),
		TurnIndex:     turnIndex,
		CurrentOutput: currentOutput,
		ToolCalls:     toolCalls,
		SessionID:     sessionID,
		PromptID:      h.taskType,
		Extras:        extras,
	}
}

// extractToolCalls builds a full ToolCallRecord list from assistant tool calls,
// matching with tool-role result messages for Arguments, Result, and Error fields.
func extractToolCalls(messages []types.Message) []evals.ToolCallRecord {
	toolResults := buildToolResultMap(messages)

	var toolCalls []evals.ToolCallRecord
	for i := range messages {
		if messages[i].Role != "assistant" {
			continue
		}
		for _, tc := range messages[i].ToolCalls {
			toolCalls = append(toolCalls, buildToolCallRecord(tc, i, toolResults))
		}
	}

	return toolCalls
}

// buildToolResultMap creates a map of tool call ID → result message from tool-role messages.
func buildToolResultMap(messages []types.Message) map[string]types.Message {
	toolResults := make(map[string]types.Message)
	for i := range messages {
		if messages[i].Role == "tool" && messages[i].ToolResult != nil {
			toolResults[messages[i].ToolResult.ID] = messages[i]
		}
	}
	return toolResults
}

// buildToolCallRecord creates a ToolCallRecord from a tool call and its result.
func buildToolCallRecord(
	tc types.MessageToolCall, turnIndex int, toolResults map[string]types.Message,
) evals.ToolCallRecord {
	record := evals.ToolCallRecord{
		TurnIndex: turnIndex,
		ToolName:  tc.Name,
	}
	if len(tc.Args) > 0 {
		record.Arguments = parseJSONArgs(tc.Args)
	}
	if resultMsg, ok := toolResults[tc.ID]; ok {
		record.Result = resultMsg.Content
		if resultMsg.ToolResult != nil && resultMsg.ToolResult.Error != "" {
			record.Error = resultMsg.ToolResult.Error
		}
	}
	return record
}

// parseJSONArgs parses JSON bytes into a map, returning nil on failure.
func parseJSONArgs(data []byte) map[string]any {
	var args map[string]any
	if err := json.Unmarshal(data, &args); err != nil {
		return nil
	}
	return args
}

// extractWorkflowExtras pulls workflow metadata from message Meta fields.
func extractWorkflowExtras(messages []types.Message) map[string]any {
	extras := make(map[string]any)
	for i := range messages {
		if messages[i].Meta == nil {
			continue
		}
		if state, ok := messages[i].Meta["_workflow_state"]; ok {
			extras["workflow_state"] = state
		}
		if transitions, ok := messages[i].Meta["_workflow_transitions"]; ok {
			extras["workflow_transitions"] = transitions
		}
		if complete, ok := messages[i].Meta["_workflow_complete"]; ok {
			extras["workflow_complete"] = complete
		}
	}
	if len(extras) == 0 {
		return nil
	}
	return extras
}

// convertToEvalMessages converts Arena types.Message to evals types.Message.
// Since both use runtime/types.Message, this is an identity conversion.
func convertToEvalMessages(messages []types.Message) []types.Message {
	return messages
}

// filterEvalDefs filters eval defs to only include types in the filter list.
// If the filter is empty, all defs are returned.
func filterEvalDefs(defs []evals.EvalDef, filter []string) []evals.EvalDef {
	if len(filter) == 0 {
		return defs
	}

	allowed := make(map[string]bool, len(filter))
	for _, t := range filter {
		allowed[t] = true
	}

	var filtered []evals.EvalDef
	for i := range defs {
		if allowed[defs[i].Type] {
			filtered = append(filtered, defs[i])
		}
	}
	return filtered
}
