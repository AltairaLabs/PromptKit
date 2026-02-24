package assertions

import (
	"context"
	"fmt"
)

// ToolCallChainConversationValidator checks a dependency chain of tool calls
// with per-step constraints across the entire conversation.
// Params:
//   - steps: []map[string]interface{} — chain steps with tool name and optional constraints
//     Each step supports: tool, result_includes, result_matches, args_match, no_error
//
// Type: "tool_call_chain"
type ToolCallChainConversationValidator struct{}

// Type returns the validator type name.
func (v *ToolCallChainConversationValidator) Type() string { return "tool_call_chain" }

// NewToolCallChainConversationValidator constructs a conversation-level tool_call_chain validator.
func NewToolCallChainConversationValidator() ConversationValidator {
	return &ToolCallChainConversationValidator{}
}

// ValidateConversation checks the dependency chain across the entire conversation.
func (v *ToolCallChainConversationValidator) ValidateConversation(
	ctx context.Context,
	convCtx *ConversationContext,
	params map[string]interface{},
) ConversationValidationResult {
	stepsRaw, _ := params["steps"].([]interface{})
	steps := make([]chainStep, 0, len(stepsRaw))

	for _, raw := range stepsRaw {
		stepMap, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		step := chainStep{
			noError: extractBoolParam(stepMap, "no_error"),
		}
		step.tool, _ = stepMap["tool"].(string)
		step.resultIncludes = extractStringSlice(stepMap, "result_includes")
		step.resultMatches, _ = stepMap["result_matches"].(string)
		step.argsMatch = extractMapStringString(stepMap, "args_match")
		steps = append(steps, step)
	}

	if len(steps) == 0 {
		return ConversationValidationResult{
			Passed:  true,
			Message: "empty chain always passes",
		}
	}

	views := toolCallViewsFromRecords(convCtx.ToolCalls)
	completed, failure := coreToolCallChain(views, steps)

	if failure != nil {
		return ConversationValidationResult{
			Passed:  false,
			Message: failure["message"].(string),
			Details: failure,
		}
	}

	if completed < len(steps) {
		return ConversationValidationResult{
			Passed: false,
			Message: fmt.Sprintf(
				"chain incomplete: satisfied %d/%d steps, missing %q",
				completed, len(steps), steps[completed].tool,
			),
			Details: map[string]interface{}{
				"completed_steps": completed,
				"total_steps":     len(steps),
			},
		}
	}

	return ConversationValidationResult{
		Passed:  true,
		Message: "chain fully satisfied",
		Details: map[string]interface{}{
			"completed_steps": len(steps),
		},
	}
}
