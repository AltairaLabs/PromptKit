package assertions

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestAgentResponseContainsValidator(t *testing.T) {
	tests := []struct {
		name       string
		agent      string
		contains   string
		messages   []types.Message
		wantPassed bool
	}{
		{
			name:     "Agent response contains expected text",
			agent:    "researcher",
			contains: "quantum computing",
			messages: []types.Message{
				{Role: "user", Content: "Research quantum computing"},
				{
					Role: "assistant",
					ToolCalls: []types.MessageToolCall{
						{ID: "call_1", Name: "researcher", Args: []byte(`{"query": "quantum computing"}`)},
					},
				},
				{
					Role: "tool",
					ToolResult: &types.MessageToolResult{
						ID:      "call_1",
						Name:    "researcher",
						Content: "Here are the results about quantum computing and its applications.",
					},
				},
			},
			wantPassed: true,
		},
		{
			name:     "Agent response does not contain expected text",
			agent:    "researcher",
			contains: "machine learning",
			messages: []types.Message{
				{Role: "user", Content: "Research quantum computing"},
				{
					Role: "assistant",
					ToolCalls: []types.MessageToolCall{
						{ID: "call_1", Name: "researcher", Args: []byte(`{"query": "quantum computing"}`)},
					},
				},
				{
					Role: "tool",
					ToolResult: &types.MessageToolResult{
						ID:      "call_1",
						Name:    "researcher",
						Content: "Here are the results about quantum computing.",
					},
				},
			},
			wantPassed: false,
		},
		{
			name:     "Agent not found in messages",
			agent:    "writer",
			contains: "some text",
			messages: []types.Message{
				{Role: "user", Content: "Do something"},
				{
					Role: "assistant",
					ToolCalls: []types.MessageToolCall{
						{ID: "call_1", Name: "researcher", Args: []byte(`{}`)},
					},
				},
				{
					Role: "tool",
					ToolResult: &types.MessageToolResult{
						ID:      "call_1",
						Name:    "researcher",
						Content: "Research results here.",
					},
				},
			},
			wantPassed: false,
		},
		{
			name:       "No messages at all",
			agent:      "researcher",
			contains:   "anything",
			messages:   []types.Message{},
			wantPassed: false,
		},
		{
			name:     "Skips statestore messages",
			agent:    "researcher",
			contains: "old result",
			messages: []types.Message{
				{
					Role:   "tool",
					Source: "statestore",
					ToolResult: &types.MessageToolResult{
						ID:      "old_call",
						Name:    "researcher",
						Content: "old result from previous turn",
					},
				},
				{Role: "user", Content: "New query"},
				{
					Role: "assistant",
					ToolCalls: []types.MessageToolCall{
						{ID: "call_2", Name: "researcher", Args: []byte(`{}`)},
					},
				},
				{
					Role: "tool",
					ToolResult: &types.MessageToolResult{
						ID:      "call_2",
						Name:    "researcher",
						Content: "new result without the expected text",
					},
				},
			},
			wantPassed: false,
		},
		{
			name:     "Multiple agents, correct one matched",
			agent:    "writer",
			contains: "draft complete",
			messages: []types.Message{
				{
					Role: "assistant",
					ToolCalls: []types.MessageToolCall{
						{ID: "call_1", Name: "researcher", Args: []byte(`{}`)},
						{ID: "call_2", Name: "writer", Args: []byte(`{}`)},
					},
				},
				{
					Role: "tool",
					ToolResult: &types.MessageToolResult{
						ID:      "call_1",
						Name:    "researcher",
						Content: "research findings here",
					},
				},
				{
					Role: "tool",
					ToolResult: &types.MessageToolResult{
						ID:      "call_2",
						Name:    "writer",
						Content: "The draft complete and ready for review.",
					},
				},
			},
			wantPassed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := map[string]interface{}{
				"agent":    tt.agent,
				"contains": tt.contains,
			}
			validator := NewAgentResponseContainsValidator(params)

			enhancedParams := map[string]interface{}{
				"_turn_messages": tt.messages,
			}

			result := validator.Validate("", enhancedParams)

			if result.Passed != tt.wantPassed {
				t.Errorf("Validate() Passed = %v, want %v, details: %v", result.Passed, tt.wantPassed, result.Details)
			}
		})
	}
}

func TestAgentResponseContainsValidator_LegacyFallback(t *testing.T) {
	// Without _turn_messages, the validator should fail gracefully
	params := map[string]interface{}{
		"agent":    "researcher",
		"contains": "some text",
	}
	validator := NewAgentResponseContainsValidator(params)

	result := validator.Validate("", map[string]interface{}{})
	if result.Passed {
		t.Errorf("Expected fail when no turn messages available, got pass")
	}

	details, ok := result.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("Expected details to be map[string]interface{}")
	}
	if details["reason"] == nil {
		t.Errorf("Expected reason in details")
	}
}
