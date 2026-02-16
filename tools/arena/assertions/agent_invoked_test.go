package assertions

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestAgentInvokedValidator(t *testing.T) {
	tests := []struct {
		name           string
		expectedAgents []string
		actualCalls    []types.MessageToolCall
		wantPassed     bool
		wantMissing    []string
	}{
		{
			name:           "All expected agents invoked",
			expectedAgents: []string{"researcher", "writer"},
			actualCalls: []types.MessageToolCall{
				{Name: "researcher"},
				{Name: "writer"},
			},
			wantPassed:  true,
			wantMissing: nil,
		},
		{
			name:           "Missing one agent",
			expectedAgents: []string{"researcher", "writer"},
			actualCalls: []types.MessageToolCall{
				{Name: "researcher"},
			},
			wantPassed:  false,
			wantMissing: []string{"writer"},
		},
		{
			name:           "Missing all agents",
			expectedAgents: []string{"researcher", "writer"},
			actualCalls:    []types.MessageToolCall{},
			wantPassed:     false,
			wantMissing:    []string{"researcher", "writer"},
		},
		{
			name:           "Extra agents invoked is OK",
			expectedAgents: []string{"researcher"},
			actualCalls: []types.MessageToolCall{
				{Name: "researcher"},
				{Name: "editor"},
			},
			wantPassed:  true,
			wantMissing: nil,
		},
		{
			name:           "No expected agents",
			expectedAgents: []string{},
			actualCalls: []types.MessageToolCall{
				{Name: "researcher"},
			},
			wantPassed:  true,
			wantMissing: nil,
		},
		{
			name:           "Agent invoked multiple times",
			expectedAgents: []string{"researcher"},
			actualCalls: []types.MessageToolCall{
				{Name: "researcher"},
				{Name: "researcher"},
			},
			wantPassed:  true,
			wantMissing: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create validator with factory pattern
			params := map[string]interface{}{
				"agents": tt.expectedAgents,
			}
			validator := NewAgentInvokedValidator(params)

			// Prepare enhanced params with tool calls
			enhancedParams := map[string]interface{}{
				"_message_tool_calls": tt.actualCalls,
			}

			// Validate
			result := validator.Validate("", enhancedParams)

			if result.Passed != tt.wantPassed {
				t.Errorf("Validate() Passed = %v, want %v", result.Passed, tt.wantPassed)
			}

			// Check missing agents
			if !result.Passed {
				details, ok := result.Details.(map[string]interface{})
				if !ok {
					t.Fatalf("Expected details to be map[string]interface{}, got %T", result.Details)
				}

				missing, ok := details["missing_agents"].([]string)
				if !ok {
					t.Fatalf("Expected missing_agents to be []string, got %T", details["missing_agents"])
				}

				if len(missing) != len(tt.wantMissing) {
					t.Errorf("Got %d missing agents, want %d: %v", len(missing), len(tt.wantMissing), missing)
				}

				for i, want := range tt.wantMissing {
					if i >= len(missing) || missing[i] != want {
						t.Errorf("Missing agent %d = %v, want %v", i, missing, tt.wantMissing)
					}
				}
			}
		})
	}
}

func TestAgentInvokedValidator_TurnMessages(t *testing.T) {
	params := map[string]interface{}{
		"agents": []string{"researcher"},
	}
	validator := NewAgentInvokedValidator(params)

	// Test with _turn_messages (new approach)
	enhancedParams := map[string]interface{}{
		"_turn_messages": []types.Message{
			{Role: "user", Content: "Find me some info"},
			{
				Role: "assistant",
				ToolCalls: []types.MessageToolCall{
					{ID: "call_1", Name: "researcher", Args: []byte(`{"query": "test"}`)},
				},
			},
		},
	}

	result := validator.Validate("", enhancedParams)
	if !result.Passed {
		t.Errorf("Expected pass with turn messages, got fail")
	}
}

func TestAgentInvokedValidator_FactoryWithSliceTypes(t *testing.T) {
	tests := []struct {
		name   string
		params map[string]interface{}
		want   []string
	}{
		{
			name: "String slice",
			params: map[string]interface{}{
				"agents": []string{"agent1", "agent2"},
			},
			want: []string{"agent1", "agent2"},
		},
		{
			name: "Interface slice",
			params: map[string]interface{}{
				"agents": []interface{}{"agent1", "agent2"},
			},
			want: []string{"agent1", "agent2"},
		},
		{
			name:   "Missing agents param",
			params: map[string]interface{}{},
			want:   []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := NewAgentInvokedValidator(tt.params)

			result := validator.Validate("", map[string]interface{}{
				"_message_tool_calls": []types.MessageToolCall{},
			})

			if len(tt.want) == 0 && !result.Passed {
				t.Error("Expected Passed=true when no agents expected")
			}
		})
	}
}
