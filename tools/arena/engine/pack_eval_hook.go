package engine

import (
	"context"
	"encoding/json"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
)

// PackEvalHook manages pack eval execution during Arena conversation runs.
// It wraps an EvalRunner and converts results into the assertion format
// used by Arena's reporting pipeline.
type PackEvalHook struct {
	runner   *evals.EvalRunner
	defs     []evals.EvalDef
	taskType string
	metadata map[string]any // injected into every EvalContext (e.g. judge_targets)
}

// NewPackEvalHook creates a hook for executing pack evals during Arena runs.
// If skipEvals is true, the runner is nil and all methods are no-ops.
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

	var runner *evals.EvalRunner
	if !skipEvals {
		runner = evals.NewEvalRunner(registry)
	}

	return &PackEvalHook{
		runner:   runner,
		defs:     filteredDefs,
		taskType: taskType,
	}
}

// SetMetadata sets metadata that will be injected into every EvalContext.
// Used to pass judge_targets, prompt_registry, and other config to eval handlers.
func (h *PackEvalHook) SetMetadata(metadata map[string]any) {
	if h == nil {
		return
	}
	h.metadata = metadata
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
	if h == nil || !h.HasEvals() || h.runner == nil {
		return nil
	}

	evalCtx := h.buildEvalContext(messages, turnIndex, sessionID)
	results := h.runner.RunTurnEvals(ctx, h.defs, evalCtx)
	return assertions.ConvertEvalResults(results)
}

// RunSessionEvals runs session-complete evals after conversation finishes.
// Returns converted ConversationValidationResult entries.
func (h *PackEvalHook) RunSessionEvals(
	ctx context.Context,
	messages []types.Message,
	sessionID string,
) []assertions.ConversationValidationResult {
	if h == nil || !h.HasEvals() || h.runner == nil {
		return nil
	}

	turnIndex := len(messages) - 1
	if turnIndex < 0 {
		turnIndex = 0
	}
	evalCtx := h.buildEvalContext(messages, turnIndex, sessionID)
	results := h.runner.RunSessionEvals(ctx, h.defs, evalCtx)
	return assertions.ConvertEvalResults(results)
}

// RunConversationEvals runs conversation-complete evals after all turns finish.
// Returns converted ConversationValidationResult entries.
func (h *PackEvalHook) RunConversationEvals(
	ctx context.Context,
	messages []types.Message,
	sessionID string,
) []assertions.ConversationValidationResult {
	if h == nil || !h.HasEvals() || h.runner == nil {
		return nil
	}

	turnIndex := len(messages) - 1
	if turnIndex < 0 {
		turnIndex = 0
	}
	evalCtx := h.buildEvalContext(messages, turnIndex, sessionID)
	results := h.runner.RunConversationEvals(ctx, h.defs, evalCtx)
	return assertions.ConvertEvalResults(results)
}

// RunAssertionsAsEvals converts assertion configs to EvalDefs and runs them
// through the runner. Returns raw EvalResults (not converted to assertion format).
// The trigger parameter overrides the default trigger on each converted def.
func (h *PackEvalHook) RunAssertionsAsEvals(
	ctx context.Context,
	assertionConfigs []assertions.AssertionConfig,
	messages []types.Message,
	turnIndex int,
	sessionID string,
	trigger evals.EvalTrigger,
) []evals.EvalResult {
	if h == nil || h.runner == nil || len(assertionConfigs) == 0 {
		return nil
	}

	defs := make([]evals.EvalDef, len(assertionConfigs))
	for i, cfg := range assertionConfigs {
		defs[i] = assertions.ToEvalDef(cfg, i)
		defs[i].Trigger = trigger
	}

	evalCtx := h.buildEvalContext(messages, turnIndex, sessionID)

	switch trigger { //nolint:exhaustive // Only conversation and turn triggers are meaningful here
	case evals.TriggerOnConversationComplete:
		return h.runner.RunConversationEvals(ctx, defs, evalCtx)
	case evals.TriggerEveryTurn:
		return h.runner.RunTurnEvals(ctx, defs, evalCtx)
	default:
		return h.runner.RunTurnEvals(ctx, defs, evalCtx)
	}
}

// RunAssertionsAsConversationResults converts assertion configs to EvalDefs,
// runs them through the runner, and wraps results in ConversationValidationResult.
// The results use the original assertion type names (not pack_eval: prefixed).
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
	converted := assertions.ConvertEvalResults(results)
	// Restore original assertion type names — ConvertEvalResults adds pack_eval:
	// prefix which is only appropriate for pack-defined evals, not scenario assertions.
	for i := range converted {
		if i < len(assertionConfigs) {
			converted[i].Type = assertionConfigs[i].Type
		}
	}
	return converted
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
		Metadata:      h.metadata,
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
		// Prefer multimodal Parts over text-only Content for eval assertions
		if resultMsg.ToolResult != nil && len(resultMsg.ToolResult.Parts) > 0 {
			record.Result = resultMsg.ToolResult.Parts
		} else {
			record.Result = resultMsg.Content
		}
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
