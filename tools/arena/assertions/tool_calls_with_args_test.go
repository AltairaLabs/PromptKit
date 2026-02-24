package assertions

import (
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestToolCallsWithArgsValidator_ExactMatch(t *testing.T) {
	params := map[string]interface{}{
		"tool_name": "get_weather",
		"expected_args": map[string]interface{}{
			"location": "test_city",
		},
	}
	validator := NewToolCallsWithArgsValidator(params)

	// Create test messages with tool call
	args, _ := json.Marshal(map[string]interface{}{"location": "test_city"})
	messages := []types.Message{
		{
			Role: "assistant",
			ToolCalls: []types.MessageToolCall{
				{Name: "get_weather", Args: args},
			},
		},
	}

	result := validator.Validate("", map[string]interface{}{
		"_turn_messages": messages,
	})

	if !result.Passed {
		t.Fatalf("expected pass for matching args, got: %+v", result)
	}
}

func TestToolCallsWithArgsValidator_ExactMismatch(t *testing.T) {
	params := map[string]interface{}{
		"tool_name": "get_weather",
		"expected_args": map[string]interface{}{
			"location": "test_city",
		},
	}
	validator := NewToolCallsWithArgsValidator(params)

	// Create test messages with wrong location
	args, _ := json.Marshal(map[string]interface{}{"location": "wrong_city"})
	messages := []types.Message{
		{
			Role: "assistant",
			ToolCalls: []types.MessageToolCall{
				{Name: "get_weather", Args: args},
			},
		},
	}

	result := validator.Validate("", map[string]interface{}{
		"_turn_messages": messages,
	})

	if result.Passed {
		t.Fatalf("expected failure for mismatched args, got: %+v", result)
	}
}

func TestToolCallsWithArgsValidator_PatternMatch(t *testing.T) {
	params := map[string]interface{}{
		"tool_name": "analyze_image",
		"args_match": map[string]interface{}{
			"description": "(?i)(google|logo|colorful)",
		},
	}
	validator := NewToolCallsWithArgsValidator(params)

	// Create test messages with description containing "Google logo"
	args, _ := json.Marshal(map[string]interface{}{
		"description": "This is a colorful Google logo with letters",
	})
	messages := []types.Message{
		{
			Role: "assistant",
			ToolCalls: []types.MessageToolCall{
				{Name: "analyze_image", Args: args},
			},
		},
	}

	result := validator.Validate("", map[string]interface{}{
		"_turn_messages": messages,
	})

	if !result.Passed {
		t.Fatalf("expected pass for matching pattern, got: %+v", result)
	}
}

func TestToolCallsWithArgsValidator_PatternMismatch(t *testing.T) {
	params := map[string]interface{}{
		"tool_name": "analyze_image",
		"args_match": map[string]interface{}{
			"description": "(?i)(microsoft|apple)",
		},
	}
	validator := NewToolCallsWithArgsValidator(params)

	// Create test messages with description NOT containing Microsoft/Apple
	args, _ := json.Marshal(map[string]interface{}{
		"description": "This is a colorful Google logo",
	})
	messages := []types.Message{
		{
			Role: "assistant",
			ToolCalls: []types.MessageToolCall{
				{Name: "analyze_image", Args: args},
			},
		},
	}

	result := validator.Validate("", map[string]interface{}{
		"_turn_messages": messages,
	})

	if result.Passed {
		t.Fatalf("expected failure for non-matching pattern, got: %+v", result)
	}
}

func TestToolCallsWithArgsValidator_ToolNotCalled(t *testing.T) {
	params := map[string]interface{}{
		"tool_name": "analyze_image",
		"args_match": map[string]interface{}{
			"description": ".*",
		},
	}
	validator := NewToolCallsWithArgsValidator(params)

	// Create test messages with different tool called
	args, _ := json.Marshal(map[string]interface{}{"location": "NYC"})
	messages := []types.Message{
		{
			Role: "assistant",
			ToolCalls: []types.MessageToolCall{
				{Name: "get_weather", Args: args},
			},
		},
	}

	result := validator.Validate("", map[string]interface{}{
		"_turn_messages": messages,
	})

	if result.Passed {
		t.Fatalf("expected failure when tool not called, got: %+v", result)
	}

	details, ok := result.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details to be map[string]interface{}, got: %T", result.Details)
	}
	if details["error"] != "tool_not_called" {
		t.Fatalf("expected 'tool_not_called' error, got: %v", details["error"])
	}
}

func TestToolCallsWithArgsValidator_CombinedExactAndPattern(t *testing.T) {
	params := map[string]interface{}{
		"tool_name": "search",
		"expected_args": map[string]interface{}{
			"limit": float64(10), // JSON numbers are float64
		},
		"args_match": map[string]interface{}{
			"query": "(?i)test",
		},
	}
	validator := NewToolCallsWithArgsValidator(params)

	// Create test messages with matching exact and pattern args
	args, _ := json.Marshal(map[string]interface{}{
		"query": "This is a TEST query",
		"limit": 10,
	})
	messages := []types.Message{
		{
			Role: "assistant",
			ToolCalls: []types.MessageToolCall{
				{Name: "search", Args: args},
			},
		},
	}

	result := validator.Validate("", map[string]interface{}{
		"_turn_messages": messages,
	})

	if !result.Passed {
		t.Fatalf("expected pass for combined match, got: %+v", result)
	}
}

func TestToolCallsWithArgsValidator_MissingArgument(t *testing.T) {
	params := map[string]interface{}{
		"tool_name": "search",
		"expected_args": map[string]interface{}{
			"query": nil, // presence-only check
			"limit": nil,
		},
	}
	validator := NewToolCallsWithArgsValidator(params)

	// Create test messages missing limit arg
	args, _ := json.Marshal(map[string]interface{}{
		"query": "test",
	})
	messages := []types.Message{
		{
			Role: "assistant",
			ToolCalls: []types.MessageToolCall{
				{Name: "search", Args: args},
			},
		},
	}

	result := validator.Validate("", map[string]interface{}{
		"_turn_messages": messages,
	})

	if result.Passed {
		t.Fatalf("expected failure for missing argument, got: %+v", result)
	}
}

func TestToolCallsWithArgsValidator_EmptyArgs(t *testing.T) {
	params := map[string]interface{}{
		"tool_name": "simple_tool",
		"expected_args": map[string]interface{}{
			"param": "value",
		},
	}
	validator := NewToolCallsWithArgsValidator(params)

	// Create test messages with empty args
	messages := []types.Message{
		{
			Role: "assistant",
			ToolCalls: []types.MessageToolCall{
				{Name: "simple_tool", Args: nil}, // empty args
			},
		},
	}

	result := validator.Validate("", map[string]interface{}{
		"_turn_messages": messages,
	})

	if result.Passed {
		t.Fatalf("expected failure for empty args when args required, got: %+v", result)
	}
}

func TestToolCallsWithArgsValidator_InvalidJSON(t *testing.T) {
	params := map[string]interface{}{
		"tool_name": "test_tool",
		"expected_args": map[string]interface{}{
			"param": "value",
		},
	}
	validator := NewToolCallsWithArgsValidator(params)

	// Create test messages with invalid JSON args
	messages := []types.Message{
		{
			Role: "assistant",
			ToolCalls: []types.MessageToolCall{
				{Name: "test_tool", Args: []byte("invalid json{")},
			},
		},
	}

	result := validator.Validate("", map[string]interface{}{
		"_turn_messages": messages,
	})

	if result.Passed {
		t.Fatalf("expected failure for invalid JSON args, got: %+v", result)
	}
}

func TestToolCallsWithArgsValidator_InvalidRegexPattern(t *testing.T) {
	params := map[string]interface{}{
		"tool_name": "test_tool",
		"args_match": map[string]interface{}{
			"description": "[invalid(regex",
		},
	}
	validator := NewToolCallsWithArgsValidator(params)

	args, _ := json.Marshal(map[string]interface{}{
		"description": "some value",
	})
	messages := []types.Message{
		{
			Role: "assistant",
			ToolCalls: []types.MessageToolCall{
				{Name: "test_tool", Args: args},
			},
		},
	}

	result := validator.Validate("", map[string]interface{}{
		"_turn_messages": messages,
	})

	if result.Passed {
		t.Fatalf("expected failure for invalid regex pattern, got: %+v", result)
	}
}

func TestToolCallsWithArgsValidator_MissingArgumentForPattern(t *testing.T) {
	params := map[string]interface{}{
		"tool_name": "test_tool",
		"args_match": map[string]interface{}{
			"missing_field": ".*",
		},
	}
	validator := NewToolCallsWithArgsValidator(params)

	args, _ := json.Marshal(map[string]interface{}{
		"other_field": "some value",
	})
	messages := []types.Message{
		{
			Role: "assistant",
			ToolCalls: []types.MessageToolCall{
				{Name: "test_tool", Args: args},
			},
		},
	}

	result := validator.Validate("", map[string]interface{}{
		"_turn_messages": messages,
	})

	if result.Passed {
		t.Fatalf("expected failure for missing argument for pattern, got: %+v", result)
	}
}

func TestToolCallsWithArgsValidator_NoRequirements(t *testing.T) {
	params := map[string]interface{}{
		"tool_name": "test_tool",
		// No expected_args or args_match
	}
	validator := NewToolCallsWithArgsValidator(params)

	args, _ := json.Marshal(map[string]interface{}{"any": "value"})
	messages := []types.Message{
		{
			Role: "assistant",
			ToolCalls: []types.MessageToolCall{
				{Name: "test_tool", Args: args},
			},
		},
	}

	result := validator.Validate("", map[string]interface{}{
		"_turn_messages": messages,
	})

	if !result.Passed {
		t.Fatalf("expected pass when no requirements, got: %+v", result)
	}
}

func TestToolCallsWithArgsValidator_FallbackToExtractToolCalls(t *testing.T) {
	params := map[string]interface{}{
		"tool_name": "test_tool",
		"expected_args": map[string]interface{}{
			"location": "NYC",
		},
	}
	validator := NewToolCallsWithArgsValidator(params)

	// Use fallback path (no _turn_messages, use _message_tool_calls directly)
	args, _ := json.Marshal(map[string]interface{}{"location": "NYC"})
	result := validator.Validate("", map[string]interface{}{
		"_message_tool_calls": []types.MessageToolCall{
			{Name: "test_tool", Args: args},
		},
	})

	if !result.Passed {
		t.Fatalf("expected pass with fallback extraction, got: %+v", result)
	}
}

func TestToolCallsWithArgsValidator_ResultIncludes(t *testing.T) {
	params := map[string]interface{}{
		"tool_name":       "get_order",
		"result_includes": []interface{}{"shipped", "tracking"},
	}
	validator := NewToolCallsWithArgsValidator(params)

	messages := buildTurnMessages(
		testToolCall{id: "c1", name: "get_order", result: `{"status":"shipped","tracking":"TRK-123"}`, round: 0},
	)

	result := validator.Validate("", map[string]interface{}{
		"_turn_messages": messages,
	})

	if !result.Passed {
		t.Fatalf("expected pass for matching result_includes, got: %+v", result)
	}
}

func TestToolCallsWithArgsValidator_ResultIncludesFails(t *testing.T) {
	params := map[string]interface{}{
		"tool_name":       "get_order",
		"result_includes": []interface{}{"refunded"},
	}
	validator := NewToolCallsWithArgsValidator(params)

	messages := buildTurnMessages(
		testToolCall{id: "c1", name: "get_order", result: `{"status":"shipped"}`, round: 0},
	)

	result := validator.Validate("", map[string]interface{}{
		"_turn_messages": messages,
	})

	if result.Passed {
		t.Fatalf("expected failure for missing result pattern, got: %+v", result)
	}
}

func TestToolCallsWithArgsValidator_ResultMatches(t *testing.T) {
	params := map[string]interface{}{
		"tool_name":      "get_order",
		"result_matches": `status.*shipped`,
	}
	validator := NewToolCallsWithArgsValidator(params)

	messages := buildTurnMessages(
		testToolCall{id: "c1", name: "get_order", result: `{"status":"shipped"}`, round: 0},
	)

	result := validator.Validate("", map[string]interface{}{
		"_turn_messages": messages,
	})

	if !result.Passed {
		t.Fatalf("expected pass for matching result_matches, got: %+v", result)
	}
}

func TestToolCallsWithArgsValidator_NoError(t *testing.T) {
	params := map[string]interface{}{
		"tool_name": "get_order",
		"no_error":  true,
	}
	validator := NewToolCallsWithArgsValidator(params)

	messages := buildTurnMessages(
		testToolCall{id: "c1", name: "get_order", err: "not found", round: 0},
	)

	result := validator.Validate("", map[string]interface{}{
		"_turn_messages": messages,
	})

	if result.Passed {
		t.Fatalf("expected failure for tool with error + no_error constraint, got: %+v", result)
	}
}

func TestToolCallsWithArgsValidator_ResultCheckSkippedOnDuplex(t *testing.T) {
	params := map[string]interface{}{
		"tool_name":       "get_order",
		"result_includes": []interface{}{"shipped"},
	}
	validator := NewToolCallsWithArgsValidator(params)

	// Use fallback path with _message_tool_calls (no _turn_messages)
	args, _ := json.Marshal(map[string]interface{}{})
	result := validator.Validate("", map[string]interface{}{
		"_message_tool_calls": []types.MessageToolCall{
			{Name: "get_order", Args: args},
		},
	})

	// Should pass with result_check_skipped since no _turn_messages available
	if !result.Passed {
		t.Fatalf("expected pass when result check skipped on duplex, got: %+v", result)
	}
	details, ok := result.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details to be map, got: %T", result.Details)
	}
	if skipped, _ := details["result_check_skipped"].(bool); !skipped {
		t.Fatalf("expected result_check_skipped=true, got: %v", details)
	}
}

func TestExtractMapStringString_DirectMap(t *testing.T) {
	// Test extractMapStringString with map[string]string input
	params := map[string]interface{}{
		"args_match": map[string]string{
			"key": "value",
		},
	}

	result := extractMapStringString(params, "args_match")
	if result == nil {
		t.Fatal("expected non-nil result for map[string]string input")
	}
	if result["key"] != "value" {
		t.Fatalf("expected 'value', got '%s'", result["key"])
	}
}

func TestExtractMapStringInterface_Nil(t *testing.T) {
	params := map[string]interface{}{}

	result := extractMapStringInterface(params, "nonexistent")
	if result != nil {
		t.Fatal("expected nil result for nonexistent key")
	}
}

func TestExtractMapStringString_Nil(t *testing.T) {
	params := map[string]interface{}{}

	result := extractMapStringString(params, "nonexistent")
	if result != nil {
		t.Fatal("expected nil result for nonexistent key")
	}
}
