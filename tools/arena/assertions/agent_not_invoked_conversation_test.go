package assertions

import (
	"context"
	"testing"
)

func TestAgentNotInvokedConversationValidator(t *testing.T) {
	v := NewAgentNotInvokedConversationValidator()
	ctx := context.Background()

	conv := &ConversationContext{
		ToolCalls: []ToolCallRecord{
			{TurnIndex: 0, ToolName: "researcher"},
			{TurnIndex: 1, ToolName: "writer"},
		},
	}

	// No forbidden agents called
	params := map[string]interface{}{
		"agent_names": []string{"admin_agent", "delete_agent"},
	}
	res := v.ValidateConversation(ctx, conv, params)
	if !res.Passed {
		t.Fatalf("expected pass (no forbidden agents), got: %+v", res)
	}

	// Forbidden agent called
	params2 := map[string]interface{}{
		"agent_names": []string{"writer"},
	}
	res2 := v.ValidateConversation(ctx, conv, params2)
	if res2.Passed {
		t.Fatalf("expected fail (forbidden agent called), got pass")
	}
	if len(res2.Violations) == 0 {
		t.Fatalf("expected violations to be reported")
	}
	if res2.Violations[0].Evidence["agent"] != "writer" {
		t.Errorf("expected violation for 'writer', got: %v", res2.Violations[0].Evidence)
	}
}

func TestAgentNotInvokedConversationValidator_Type(t *testing.T) {
	v := NewAgentNotInvokedConversationValidator()
	if v.Type() != "agent_not_invoked" {
		t.Errorf("Type() = %q, want %q", v.Type(), "agent_not_invoked")
	}
}

func TestAgentNotInvokedConversationValidator_EmptyConversation(t *testing.T) {
	v := NewAgentNotInvokedConversationValidator()
	ctx := context.Background()

	conv := &ConversationContext{
		ToolCalls: []ToolCallRecord{},
	}

	params := map[string]interface{}{
		"agent_names": []string{"admin_agent"},
	}
	res := v.ValidateConversation(ctx, conv, params)
	if !res.Passed {
		t.Fatalf("expected pass with empty conversation, got fail")
	}
}

func TestAgentNotInvokedConversationValidator_MultipleViolations(t *testing.T) {
	v := NewAgentNotInvokedConversationValidator()
	ctx := context.Background()

	conv := &ConversationContext{
		ToolCalls: []ToolCallRecord{
			{TurnIndex: 0, ToolName: "admin_agent"},
			{TurnIndex: 1, ToolName: "researcher"},
			{TurnIndex: 2, ToolName: "admin_agent"},
			{TurnIndex: 3, ToolName: "delete_agent"},
		},
	}

	params := map[string]interface{}{
		"agent_names": []string{"admin_agent", "delete_agent"},
	}
	res := v.ValidateConversation(ctx, conv, params)
	if res.Passed {
		t.Fatalf("expected fail with multiple violations, got pass")
	}
	// Should have 3 violations: admin_agent at turn 0, admin_agent at turn 2, delete_agent at turn 3
	if len(res.Violations) != 3 {
		t.Errorf("expected 3 violations, got %d", len(res.Violations))
	}
}
