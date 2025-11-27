package assertions

import "testing"

func TestValidateRequiredArgs_PresenceAndMismatch(t *testing.T) {
	tc := ToolCallRecord{
		TurnIndex: 1,
		ToolName:  "search",
		Arguments: map[string]interface{}{"q": "hello", "limit": 5},
	}

	// presence-only requirement
	vios := validateRequiredArgs(tc, map[string]interface{}{"q": nil})
	if len(vios) != 0 {
		t.Fatalf("expected no violations for presence-only arg, got: %v", vios)
	}

	// value mismatch
	vios = validateRequiredArgs(tc, map[string]interface{}{"limit": 10})
	if len(vios) != 1 || vios[0].Description != "argument value mismatch" {
		t.Fatalf("expected single mismatch violation, got: %v", vios)
	}

	// missing arg
	vios = validateRequiredArgs(tc, map[string]interface{}{"missing": nil})
	if len(vios) != 1 || vios[0].Description != "missing required argument" {
		t.Fatalf("expected single missing violation, got: %v", vios)
	}
}

func TestToolCallsWithArgsConversationValidator_EndToEnd(t *testing.T) {
	v := NewToolCallsWithArgsConversationValidator()
	ctx := &ConversationContext{ToolCalls: []ToolCallRecord{
		{TurnIndex: 0, ToolName: "search", Arguments: map[string]interface{}{"q": "x"}},
		{TurnIndex: 1, ToolName: "search", Arguments: map[string]interface{}{"q": "y", "limit": 10}},
	}}

	// Require presence of q and exact limit value for all matching calls
	res := v.ValidateConversation(nil, ctx, map[string]interface{}{
		"tool_name":     "search",
		"required_args": map[string]interface{}{"q": nil, "limit": 10},
	})

	// First call is missing limit => should fail
	if res.Passed || len(res.Violations) == 0 {
		t.Fatalf("expected pass for valid args, got: %+v", res)
	}

	// Now require mismatching value
	res2 := v.ValidateConversation(nil, ctx, map[string]interface{}{
		"tool_name":     "search",
		"required_args": map[string]interface{}{"limit": 5},
	})
	if res2.Passed || len(res2.Violations) == 0 {
		t.Fatalf("expected failure with violations, got: %+v", res2)
	}
}
