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

func TestValidateArgsMatch_PatternMatching(t *testing.T) {
	tc := ToolCallRecord{
		TurnIndex: 1,
		ToolName:  "analyze_image",
		Arguments: map[string]interface{}{
			"description": "This is a Google logo with colorful letters",
		},
	}

	// Pattern matches
	vios := validateArgsMatch(tc, map[string]string{"description": "(?i)(google|logo)"})
	if len(vios) != 0 {
		t.Fatalf("expected no violations for matching pattern, got: %v", vios)
	}

	// Pattern does not match
	vios = validateArgsMatch(tc, map[string]string{"description": "(?i)(microsoft|apple)"})
	if len(vios) != 1 || vios[0].Description != "argument value does not match pattern" {
		t.Fatalf("expected single pattern mismatch violation, got: %v", vios)
	}

	// Missing argument
	vios = validateArgsMatch(tc, map[string]string{"nonexistent": ".*"})
	if len(vios) != 1 || vios[0].Description != "missing argument for pattern match" {
		t.Fatalf("expected single missing argument violation, got: %v", vios)
	}

	// Invalid regex
	vios = validateArgsMatch(tc, map[string]string{"description": "[invalid"})
	if len(vios) != 1 || vios[0].Description != "invalid regex pattern" {
		t.Fatalf("expected single invalid regex violation, got: %v", vios)
	}
}

func TestToolCallsWithArgsConversationValidator_ArgsMatch(t *testing.T) {
	v := NewToolCallsWithArgsConversationValidator()
	ctx := &ConversationContext{ToolCalls: []ToolCallRecord{
		{
			TurnIndex: 0,
			ToolName:  "analyze_image",
			Arguments: map[string]interface{}{
				"description": "A colorful Google logo with red, blue, yellow and green letters",
			},
		},
	}}

	// Pattern matches
	res := v.ValidateConversation(nil, ctx, map[string]interface{}{
		"tool_name": "analyze_image",
		"args_match": map[string]interface{}{
			"description": "(?i)(google|logo|colorful)",
		},
	})
	if !res.Passed {
		t.Fatalf("expected pass for matching pattern, got: %+v", res)
	}

	// Pattern does not match
	res = v.ValidateConversation(nil, ctx, map[string]interface{}{
		"tool_name": "analyze_image",
		"args_match": map[string]interface{}{
			"description": "(?i)(microsoft|apple)",
		},
	})
	if res.Passed {
		t.Fatalf("expected failure for non-matching pattern, got: %+v", res)
	}
}

func TestToolCallsWithArgsConversationValidator_ToolNotCalled(t *testing.T) {
	v := NewToolCallsWithArgsConversationValidator()
	ctx := &ConversationContext{ToolCalls: []ToolCallRecord{
		{TurnIndex: 0, ToolName: "other_tool", Arguments: map[string]interface{}{"foo": "bar"}},
	}}

	// Require a tool that wasn't called
	res := v.ValidateConversation(nil, ctx, map[string]interface{}{
		"tool_name": "analyze_image",
		"args_match": map[string]interface{}{
			"description": ".*",
		},
	})
	if res.Passed {
		t.Fatalf("expected failure when tool not called, got: %+v", res)
	}
	if res.Message != "tool 'analyze_image' was not called" {
		t.Fatalf("expected 'tool not called' message, got: %s", res.Message)
	}
}

func TestToolCallsWithArgsConversationValidator_CombinedExactAndPattern(t *testing.T) {
	v := NewToolCallsWithArgsConversationValidator()
	ctx := &ConversationContext{ToolCalls: []ToolCallRecord{
		{
			TurnIndex: 0,
			ToolName:  "get_weather",
			Arguments: map[string]interface{}{
				"location": "New York",
				"units":    "celsius",
			},
		},
	}}

	// Both exact match and pattern match
	res := v.ValidateConversation(nil, ctx, map[string]interface{}{
		"tool_name": "get_weather",
		"required_args": map[string]interface{}{
			"units": "celsius",
		},
		"args_match": map[string]interface{}{
			"location": "(?i)new york",
		},
	})
	if !res.Passed {
		t.Fatalf("expected pass for combined exact+pattern match, got: %+v", res)
	}

	// Exact match fails
	res = v.ValidateConversation(nil, ctx, map[string]interface{}{
		"tool_name": "get_weather",
		"required_args": map[string]interface{}{
			"units": "fahrenheit",
		},
		"args_match": map[string]interface{}{
			"location": "(?i)new york",
		},
	})
	if res.Passed {
		t.Fatalf("expected failure when exact match fails, got: %+v", res)
	}
}
