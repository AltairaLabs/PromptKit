package assertions

import (
	"context"
	"testing"
)

func TestSkillActivatedConversationValidator(t *testing.T) {
	v := NewSkillActivatedConversationValidator()
	ctx := context.Background()

	conv := &ConversationContext{
		ToolCalls: []ToolCallRecord{
			{TurnIndex: 1, ToolName: "skill__activate", Arguments: map[string]interface{}{"skill": "pci-compliance"}},
			{TurnIndex: 2, ToolName: "get_order"},
			{TurnIndex: 3, ToolName: "skill__activate", Arguments: map[string]interface{}{"skill": "refund-processing"}},
			{TurnIndex: 4, ToolName: "skill__activate", Arguments: map[string]interface{}{"skill": "pci-compliance"}},
		},
	}

	// All required skills present
	params := map[string]interface{}{
		"skill_names": []string{"pci-compliance", "refund-processing"},
		"min_calls":   1,
	}
	res := v.ValidateConversation(ctx, conv, params)
	if !res.Passed {
		t.Fatalf("expected pass, got: %+v", res)
	}

	// Require at least 2 activations of pci-compliance
	params2 := map[string]interface{}{
		"skill_names": []string{"pci-compliance"},
		"min_calls":   2,
	}
	res2 := v.ValidateConversation(ctx, conv, params2)
	if !res2.Passed {
		t.Fatalf("expected pass for min_calls=2, got: %+v", res2)
	}

	// Missing skill
	params3 := map[string]interface{}{
		"skill_names": []string{"escalation-policy"},
	}
	res3 := v.ValidateConversation(ctx, conv, params3)
	if res3.Passed {
		t.Fatalf("expected fail when required skill missing")
	}

	// min_calls not met
	params4 := map[string]interface{}{
		"skill_names": []string{"refund-processing"},
		"min_calls":   3,
	}
	res4 := v.ValidateConversation(ctx, conv, params4)
	if res4.Passed {
		t.Fatalf("expected fail when min_calls not met")
	}
}

func TestSkillActivatedConversationValidator_Type(t *testing.T) {
	v := NewSkillActivatedConversationValidator()
	if v.Type() != "skill_activated" {
		t.Errorf("Type() = %q, want %q", v.Type(), "skill_activated")
	}
}

func TestSkillActivatedConversationValidator_EmptyConversation(t *testing.T) {
	v := NewSkillActivatedConversationValidator()
	ctx := context.Background()

	conv := &ConversationContext{
		ToolCalls: []ToolCallRecord{},
	}

	params := map[string]interface{}{
		"skill_names": []string{"pci-compliance"},
	}
	res := v.ValidateConversation(ctx, conv, params)
	if res.Passed {
		t.Fatalf("expected fail with empty conversation, got pass")
	}
}

func TestSkillActivatedConversationValidator_NoRequiredSkills(t *testing.T) {
	v := NewSkillActivatedConversationValidator()
	ctx := context.Background()

	conv := &ConversationContext{
		ToolCalls: []ToolCallRecord{
			{TurnIndex: 0, ToolName: "skill__activate", Arguments: map[string]interface{}{"skill": "pci-compliance"}},
		},
	}

	params := map[string]interface{}{
		"skill_names": []string{},
	}
	res := v.ValidateConversation(ctx, conv, params)
	if !res.Passed {
		t.Fatalf("expected pass with no required skills, got fail")
	}
}

func TestSkillActivatedConversationValidator_IgnoresOtherTools(t *testing.T) {
	v := NewSkillActivatedConversationValidator()
	ctx := context.Background()

	// Only non-skill tool calls
	conv := &ConversationContext{
		ToolCalls: []ToolCallRecord{
			{TurnIndex: 0, ToolName: "get_order"},
			{TurnIndex: 1, ToolName: "search"},
		},
	}

	params := map[string]interface{}{
		"skill_names": []string{"pci-compliance"},
	}
	res := v.ValidateConversation(ctx, conv, params)
	if res.Passed {
		t.Fatalf("expected fail when only non-skill tools called")
	}
}

func TestSkillActivatedConversationValidator_NilArguments(t *testing.T) {
	v := NewSkillActivatedConversationValidator()
	ctx := context.Background()

	// skill__activate with nil arguments
	conv := &ConversationContext{
		ToolCalls: []ToolCallRecord{
			{TurnIndex: 0, ToolName: "skill__activate", Arguments: nil},
		},
	}

	params := map[string]interface{}{
		"skill_names": []string{"pci-compliance"},
	}
	res := v.ValidateConversation(ctx, conv, params)
	if res.Passed {
		t.Fatalf("expected fail when skill__activate has nil arguments")
	}
}

func TestSkillNotActivatedConversationValidator(t *testing.T) {
	v := NewSkillNotActivatedConversationValidator()
	ctx := context.Background()

	conv := &ConversationContext{
		ToolCalls: []ToolCallRecord{
			{TurnIndex: 1, ToolName: "skill__activate", Arguments: map[string]interface{}{"skill": "pci-compliance"}},
			{TurnIndex: 2, ToolName: "get_order"},
		},
	}

	// Forbidden skill was activated — should fail
	params := map[string]interface{}{
		"skill_names": []string{"pci-compliance"},
	}
	res := v.ValidateConversation(ctx, conv, params)
	if res.Passed {
		t.Fatalf("expected fail when forbidden skill activated")
	}
	if len(res.Violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(res.Violations))
	}
	if res.Violations[0].TurnIndex != 1 {
		t.Errorf("expected violation at turn 1, got %d", res.Violations[0].TurnIndex)
	}

	// Non-forbidden skill — should pass
	params2 := map[string]interface{}{
		"skill_names": []string{"escalation-policy"},
	}
	res2 := v.ValidateConversation(ctx, conv, params2)
	if !res2.Passed {
		t.Fatalf("expected pass when forbidden skill not activated")
	}
}

func TestSkillNotActivatedConversationValidator_Type(t *testing.T) {
	v := NewSkillNotActivatedConversationValidator()
	if v.Type() != "skill_not_activated" {
		t.Errorf("Type() = %q, want %q", v.Type(), "skill_not_activated")
	}
}

func TestSkillNotActivatedConversationValidator_EmptyConversation(t *testing.T) {
	v := NewSkillNotActivatedConversationValidator()
	ctx := context.Background()

	conv := &ConversationContext{
		ToolCalls: []ToolCallRecord{},
	}

	params := map[string]interface{}{
		"skill_names": []string{"pci-compliance"},
	}
	res := v.ValidateConversation(ctx, conv, params)
	if !res.Passed {
		t.Fatalf("expected pass with empty conversation")
	}
}

func TestSkillNotActivatedConversationValidator_IgnoresOtherTools(t *testing.T) {
	v := NewSkillNotActivatedConversationValidator()
	ctx := context.Background()

	conv := &ConversationContext{
		ToolCalls: []ToolCallRecord{
			{TurnIndex: 0, ToolName: "get_order"},
			{TurnIndex: 1, ToolName: "skill__deactivate", Arguments: map[string]interface{}{"skill": "pci-compliance"}},
		},
	}

	// skill__deactivate is not skill__activate — should pass
	params := map[string]interface{}{
		"skill_names": []string{"pci-compliance"},
	}
	res := v.ValidateConversation(ctx, conv, params)
	if !res.Passed {
		t.Fatalf("expected pass when only deactivate called, not activate")
	}
}

func TestSkillNotActivatedConversationValidator_MultipleViolations(t *testing.T) {
	v := NewSkillNotActivatedConversationValidator()
	ctx := context.Background()

	conv := &ConversationContext{
		ToolCalls: []ToolCallRecord{
			{TurnIndex: 1, ToolName: "skill__activate", Arguments: map[string]interface{}{"skill": "pci-compliance"}},
			{TurnIndex: 3, ToolName: "skill__activate", Arguments: map[string]interface{}{"skill": "pci-compliance"}},
		},
	}

	params := map[string]interface{}{
		"skill_names": []string{"pci-compliance"},
	}
	res := v.ValidateConversation(ctx, conv, params)
	if res.Passed {
		t.Fatalf("expected fail")
	}
	if len(res.Violations) != 2 {
		t.Fatalf("expected 2 violations, got %d", len(res.Violations))
	}
}
