package assertions

import (
	"context"
	"testing"
)

func TestAgentInvokedConversationValidator(t *testing.T) {
	v := NewAgentInvokedConversationValidator()
	ctx := context.Background()

	conv := &ConversationContext{
		ToolCalls: []ToolCallRecord{
			{TurnIndex: 1, ToolName: "researcher"},
			{TurnIndex: 2, ToolName: "writer"},
			{TurnIndex: 3, ToolName: "researcher"},
		},
	}

	// All required agents present
	params := map[string]interface{}{
		"agent_names": []string{"researcher", "writer"},
		"min_calls":   1,
	}
	res := v.ValidateConversation(ctx, conv, params)
	if !res.Passed {
		t.Fatalf("expected pass, got: %+v", res)
	}

	// Require at least 2 calls to researcher
	params2 := map[string]interface{}{
		"agent_names": []string{"researcher"},
		"min_calls":   2,
	}
	res2 := v.ValidateConversation(ctx, conv, params2)
	if !res2.Passed {
		t.Fatalf("expected pass for min_calls=2, got: %+v", res2)
	}

	// Missing agent
	params3 := map[string]interface{}{
		"agent_names": []string{"editor"},
	}
	res3 := v.ValidateConversation(ctx, conv, params3)
	if res3.Passed {
		t.Fatalf("expected fail when required agent missing")
	}

	// min_calls not met
	params4 := map[string]interface{}{
		"agent_names": []string{"writer"},
		"min_calls":   3,
	}
	res4 := v.ValidateConversation(ctx, conv, params4)
	if res4.Passed {
		t.Fatalf("expected fail when min_calls not met")
	}
}

func TestAgentInvokedConversationValidator_Type(t *testing.T) {
	v := NewAgentInvokedConversationValidator()
	if v.Type() != "agent_invoked" {
		t.Errorf("Type() = %q, want %q", v.Type(), "agent_invoked")
	}
}

func TestAgentInvokedConversationValidator_EmptyConversation(t *testing.T) {
	v := NewAgentInvokedConversationValidator()
	ctx := context.Background()

	conv := &ConversationContext{
		ToolCalls: []ToolCallRecord{},
	}

	params := map[string]interface{}{
		"agent_names": []string{"researcher"},
	}
	res := v.ValidateConversation(ctx, conv, params)
	if res.Passed {
		t.Fatalf("expected fail with empty conversation, got pass")
	}
}

func TestAgentInvokedConversationValidator_NoRequiredAgents(t *testing.T) {
	v := NewAgentInvokedConversationValidator()
	ctx := context.Background()

	conv := &ConversationContext{
		ToolCalls: []ToolCallRecord{
			{TurnIndex: 0, ToolName: "researcher"},
		},
	}

	params := map[string]interface{}{
		"agent_names": []string{},
	}
	res := v.ValidateConversation(ctx, conv, params)
	if !res.Passed {
		t.Fatalf("expected pass with no required agents, got fail")
	}
}
