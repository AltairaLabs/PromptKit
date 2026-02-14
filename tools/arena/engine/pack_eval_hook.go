package engine

import (
	"context"

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
	if !h.HasEvals() {
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
	if !h.HasEvals() {
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

// buildEvalContext constructs an EvalContext from Arena messages.
func (h *PackEvalHook) buildEvalContext(
	messages []types.Message,
	turnIndex int,
	sessionID string,
) *evals.EvalContext {
	// Extract current output from the last assistant message
	var currentOutput string
	var toolCalls []evals.ToolCallRecord

	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			currentOutput = messages[i].Content
			// Extract tool calls
			for _, tc := range messages[i].ToolCalls {
				toolCalls = append(toolCalls, evals.ToolCallRecord{
					TurnIndex: i,
					ToolName:  tc.Name,
				})
			}
			break
		}
	}

	return &evals.EvalContext{
		Messages:      convertToEvalMessages(messages),
		TurnIndex:     turnIndex,
		CurrentOutput: currentOutput,
		ToolCalls:     toolCalls,
		SessionID:     sessionID,
		PromptID:      h.taskType,
	}
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
