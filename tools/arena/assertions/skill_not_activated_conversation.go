package assertions

import (
	"context"
)

// SkillNotActivatedConversationValidator checks that specific skills were NOT activated
// anywhere in the conversation.
// Skill activations appear as tool calls to "skill__activate" with a "skill" argument.
// Params:
//   - skill_names: []string forbidden skill names
//
// Type: "skill_not_activated"
type SkillNotActivatedConversationValidator struct{}

// Type returns the validator type name.
func (v *SkillNotActivatedConversationValidator) Type() string { return "skill_not_activated" }

// NewSkillNotActivatedConversationValidator constructs a conversation-level validator.
func NewSkillNotActivatedConversationValidator() ConversationValidator {
	return &SkillNotActivatedConversationValidator{}
}

// ValidateConversation ensures forbidden skills were never activated across the conversation.
func (v *SkillNotActivatedConversationValidator) ValidateConversation(
	ctx context.Context,
	convCtx *ConversationContext,
	params map[string]interface{},
) ConversationValidationResult {
	forbidden := extractStringSlice(params, "skill_names")
	forbiddenSet := make(map[string]struct{}, len(forbidden))
	for _, n := range forbidden {
		forbiddenSet[n] = struct{}{}
	}

	var violations []ConversationViolation
	for _, tc := range convCtx.ToolCalls {
		if tc.ToolName != skillActivateToolName {
			continue
		}
		skillName := extractSkillName(tc.Arguments)
		if _, bad := forbiddenSet[skillName]; bad {
			violations = append(violations, ConversationViolation{
				TurnIndex:   tc.TurnIndex,
				Description: "forbidden skill was activated",
				Evidence: map[string]interface{}{
					"skill":     skillName,
					"arguments": tc.Arguments,
				},
			})
		}
	}

	if len(violations) > 0 {
		return ConversationValidationResult{
			Passed:     false,
			Message:    "forbidden skills were activated",
			Violations: violations,
		}
	}

	return ConversationValidationResult{Passed: true, Message: "no forbidden skills activated"}
}
