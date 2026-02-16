package assertions

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestAgentNotInvokedValidator(t *testing.T) {
	tests := []struct {
		name            string
		forbiddenAgents []string
		actualCalls     []types.MessageToolCall
		wantPassed      bool
		wantCalled      []string
	}{
		{
			name:            "No forbidden agents called",
			forbiddenAgents: []string{"admin_agent", "delete_agent"},
			actualCalls: []types.MessageToolCall{
				{Name: "researcher"},
				{Name: "writer"},
			},
			wantPassed: true,
			wantCalled: nil,
		},
		{
			name:            "One forbidden agent called",
			forbiddenAgents: []string{"admin_agent", "delete_agent"},
			actualCalls: []types.MessageToolCall{
				{Name: "researcher"},
				{Name: "admin_agent"},
			},
			wantPassed: false,
			wantCalled: []string{"admin_agent"},
		},
		{
			name:            "Multiple forbidden agents called",
			forbiddenAgents: []string{"admin_agent", "delete_agent"},
			actualCalls: []types.MessageToolCall{
				{Name: "admin_agent"},
				{Name: "delete_agent"},
			},
			wantPassed: false,
			wantCalled: []string{"admin_agent", "delete_agent"},
		},
		{
			name:            "Forbidden agent called multiple times reports once",
			forbiddenAgents: []string{"admin_agent"},
			actualCalls: []types.MessageToolCall{
				{Name: "admin_agent"},
				{Name: "admin_agent"},
			},
			wantPassed: false,
			wantCalled: []string{"admin_agent"},
		},
		{
			name:            "No forbidden agents defined",
			forbiddenAgents: []string{},
			actualCalls: []types.MessageToolCall{
				{Name: "researcher"},
			},
			wantPassed: true,
			wantCalled: nil,
		},
		{
			name:            "No calls at all",
			forbiddenAgents: []string{"admin_agent"},
			actualCalls:     []types.MessageToolCall{},
			wantPassed:      true,
			wantCalled:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := map[string]interface{}{
				"agents": tt.forbiddenAgents,
			}
			validator := NewAgentNotInvokedValidator(params)

			enhancedParams := map[string]interface{}{
				"_message_tool_calls": tt.actualCalls,
			}

			result := validator.Validate("", enhancedParams)

			if result.Passed != tt.wantPassed {
				t.Errorf("Validate() Passed = %v, want %v", result.Passed, tt.wantPassed)
			}

			if !result.Passed {
				details, ok := result.Details.(map[string]interface{})
				if !ok {
					t.Fatalf("Expected details to be map[string]interface{}, got %T", result.Details)
				}

				called, ok := details["forbidden_agents_called"].([]string)
				if !ok {
					t.Fatalf("Expected forbidden_agents_called to be []string, got %T", details["forbidden_agents_called"])
				}

				if len(called) != len(tt.wantCalled) {
					t.Errorf("Got %d forbidden agents called, want %d: %v", len(called), len(tt.wantCalled), called)
				}
			}
		})
	}
}

func TestAgentNotInvokedValidator_TurnMessages(t *testing.T) {
	params := map[string]interface{}{
		"agents": []string{"admin_agent"},
	}
	validator := NewAgentNotInvokedValidator(params)

	// Test with _turn_messages where forbidden agent is called
	enhancedParams := map[string]interface{}{
		"_turn_messages": []types.Message{
			{Role: "user", Content: "Do something"},
			{
				Role: "assistant",
				ToolCalls: []types.MessageToolCall{
					{ID: "call_1", Name: "admin_agent", Args: []byte(`{}`)},
				},
			},
		},
	}

	result := validator.Validate("", enhancedParams)
	if result.Passed {
		t.Errorf("Expected fail when forbidden agent is in turn messages, got pass")
	}

	// Test with _turn_messages where no forbidden agent is called
	enhancedParams2 := map[string]interface{}{
		"_turn_messages": []types.Message{
			{Role: "user", Content: "Do something"},
			{
				Role: "assistant",
				ToolCalls: []types.MessageToolCall{
					{ID: "call_1", Name: "researcher", Args: []byte(`{}`)},
				},
			},
		},
	}

	result2 := validator.Validate("", enhancedParams2)
	if !result2.Passed {
		t.Errorf("Expected pass when no forbidden agent in turn messages, got fail")
	}
}
