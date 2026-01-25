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
