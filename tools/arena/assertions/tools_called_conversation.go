package assertions

import (
	"context"
)

// ToolsCalledConversationValidator checks that specific tools were called
// at least a minimum number of times across the full conversation.
// Params:
// - tool_names: []string required tools
// - min_calls: int optional (default 1) minimum calls per tool
// Type: "tools_called"
type ToolsCalledConversationValidator struct{}

// Type returns the validator type name.
func (v *ToolsCalledConversationValidator) Type() string { return "tools_called" }

// NewToolsCalledConversationValidator constructs a conversation-level tools_called validator.
func NewToolsCalledConversationValidator() ConversationValidator {
	return &ToolsCalledConversationValidator{}
}

// ValidateConversation evaluates whether all required tools were called
// at least the minimum number of times across the conversation.
func (v *ToolsCalledConversationValidator) ValidateConversation(
	ctx context.Context,
	convCtx *ConversationContext,
	params map[string]interface{},
) ConversationValidationResult {
	// Extract params
	required := extractStringSlice(params, "tool_names")
	minCalls := 1
	if m, ok := params["min_calls"].(int); ok && m > 0 {
		minCalls = m
	}

	// Count calls by tool name
	counts := make(map[string]int)
	for _, tc := range convCtx.ToolCalls {
		counts[tc.ToolName]++
	}

	// Find missing tools w.r.t minCalls
	var missing []string
	for _, name := range required {
		if counts[name] < minCalls {
			missing = append(missing, name)
		}
	}

	if len(missing) > 0 {
		return ConversationValidationResult{
			Passed:  false,
			Message: "missing required tools",
			Details: map[string]interface{}{
				"required":  required,
				"min_calls": minCalls,
				"counts":    counts,
				"missing":   missing,
			},
		}
	}

	return ConversationValidationResult{
		Passed:  true,
		Message: "all required tools were called",
		Details: map[string]interface{}{"counts": counts},
	}
}
