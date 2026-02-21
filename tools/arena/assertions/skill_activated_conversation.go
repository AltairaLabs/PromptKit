package assertions

import (
	"context"
	"strings"
)

const skillActivateToolName = "skill__activate"

// SkillActivatedConversationValidator checks that specific skills were activated
// at least a minimum number of times across the full conversation.
// Skill activations appear as tool calls to "skill__activate" with a "skill" argument.
// Params:
//   - skill_names: []string required skill names
//   - min_calls: int optional (default 1) minimum activations per skill
//
// Type: "skill_activated"
type SkillActivatedConversationValidator struct{}

// Type returns the validator type name.
func (v *SkillActivatedConversationValidator) Type() string { return "skill_activated" }

// NewSkillActivatedConversationValidator constructs a conversation-level skill_activated validator.
func NewSkillActivatedConversationValidator() ConversationValidator {
	return &SkillActivatedConversationValidator{}
}

// ValidateConversation evaluates whether all required skills were activated
// at least the minimum number of times across the conversation.
func (v *SkillActivatedConversationValidator) ValidateConversation(
	ctx context.Context,
	convCtx *ConversationContext,
	params map[string]interface{},
) ConversationValidationResult {
	required := extractStringSlice(params, "skill_names")
	minCalls := 1
	if m, ok := params["min_calls"].(int); ok && m > 0 {
		minCalls = m
	}

	// Count skill activations from tool calls
	counts := countSkillActivations(convCtx.ToolCalls)

	// Find missing skills w.r.t minCalls
	var missing []string
	requirements := make([]map[string]interface{}, 0, len(required))
	for _, name := range required {
		requirements = append(requirements, map[string]interface{}{
			"skill":         name,
			"calls":         counts[name],
			"requiredCalls": minCalls,
		})
		if counts[name] < minCalls {
			missing = append(missing, name)
		}
	}

	if len(missing) > 0 {
		return ConversationValidationResult{
			Passed:  false,
			Message: "missing required skill activations: " + strings.Join(missing, ", "),
			Details: map[string]interface{}{
				"requirements": requirements,
				"counts":       counts,
			},
		}
	}

	return ConversationValidationResult{
		Passed:  true,
		Message: "all required skills were activated",
		Details: map[string]interface{}{
			"requirements": requirements,
			"counts":       counts,
		},
	}
}

// countSkillActivations extracts skill names from skill__activate tool calls
// and counts how many times each was activated.
func countSkillActivations(toolCalls []ToolCallRecord) map[string]int {
	counts := make(map[string]int)
	for _, tc := range toolCalls {
		if tc.ToolName != skillActivateToolName {
			continue
		}
		skillName := extractSkillName(tc.Arguments)
		if skillName != "" {
			counts[skillName]++
		}
	}
	return counts
}

// extractSkillName gets the skill name from a skill__activate tool call's arguments.
func extractSkillName(args map[string]interface{}) string {
	if args == nil {
		return ""
	}
	if name, ok := args["skill"].(string); ok {
		return name
	}
	return ""
}
