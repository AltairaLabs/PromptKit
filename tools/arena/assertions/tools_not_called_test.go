package validators

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestToolsNotCalledValidator(t *testing.T) {
	tests := []struct {
		name           string
		forbiddenTools []string
		actualCalls    []types.MessageToolCall
		wantOK         bool
		wantCalled     []string
	}{
		{
			name:           "No forbidden tools called",
			forbiddenTools: []string{"get_customer_info", "delete_account"},
			actualCalls: []types.MessageToolCall{
				{Name: "check_subscription_status"},
			},
			wantOK:     true,
			wantCalled: nil,
		},
		{
			name:           "One forbidden tool called",
			forbiddenTools: []string{"get_customer_info", "delete_account"},
			actualCalls: []types.MessageToolCall{
				{Name: "get_customer_info"},
			},
			wantOK:     false,
			wantCalled: []string{"get_customer_info"},
		},
		{
			name:           "Multiple forbidden tools called",
			forbiddenTools: []string{"get_customer_info", "delete_account"},
			actualCalls: []types.MessageToolCall{
				{Name: "get_customer_info"},
				{Name: "delete_account"},
			},
			wantOK:     false,
			wantCalled: []string{"get_customer_info", "delete_account"},
		},
		{
			name:           "No tools called",
			forbiddenTools: []string{"get_customer_info"},
			actualCalls:    []types.MessageToolCall{},
			wantOK:         true,
			wantCalled:     nil,
		},
		{
			name:           "No forbidden tools specified",
			forbiddenTools: []string{},
			actualCalls: []types.MessageToolCall{
				{Name: "any_tool"},
			},
			wantOK:     true,
			wantCalled: nil,
		},
		{
			name:           "Forbidden tool called multiple times",
			forbiddenTools: []string{"delete_account"},
			actualCalls: []types.MessageToolCall{
				{Name: "delete_account"},
				{Name: "delete_account"},
			},
			wantOK:     false,
			wantCalled: []string{"delete_account"},
		},
		{
			name:           "Mix of forbidden and allowed tools",
			forbiddenTools: []string{"delete_account"},
			actualCalls: []types.MessageToolCall{
				{Name: "get_customer_info"},
				{Name: "delete_account"},
				{Name: "check_subscription"},
			},
			wantOK:     false,
			wantCalled: []string{"delete_account"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create validator with factory pattern
			params := map[string]interface{}{
				"tools": tt.forbiddenTools,
			}
			validator := NewToolsNotCalledValidator(params)

			// Prepare enhanced params with tool calls
			enhancedParams := map[string]interface{}{
				"_message_tool_calls": tt.actualCalls,
			}

			// Validate
			result := validator.Validate("", enhancedParams)

			if result.OK != tt.wantOK {
				t.Errorf("Validate() OK = %v, want %v", result.OK, tt.wantOK)
			}

			// Check forbidden tools that were called
			if !result.OK {
				details, ok := result.Details.(map[string]interface{})
				if !ok {
					t.Fatalf("Expected details to be map[string]interface{}, got %T", result.Details)
				}

				called, ok := details["forbidden_tools_called"].([]string)
				if !ok {
					t.Fatalf("Expected forbidden_tools_called to be []string, got %T", details["forbidden_tools_called"])
				}

				if len(called) != len(tt.wantCalled) {
					t.Errorf("Got %d called tools, want %d: %v", len(called), len(tt.wantCalled), called)
				}

				// Check that all expected tools are present (order might differ)
				calledMap := make(map[string]bool)
				for _, tool := range called {
					calledMap[tool] = true
				}

				for _, want := range tt.wantCalled {
					if !calledMap[want] {
						t.Errorf("Expected forbidden tool %q to be in called list, got %v", want, called)
					}
				}
			}
		})
	}
}

func TestToolsNotCalledValidator_FactoryWithSliceTypes(t *testing.T) {
	tests := []struct {
		name   string
		params map[string]interface{}
		want   []string
	}{
		{
			name: "String slice",
			params: map[string]interface{}{
				"tools": []string{"tool1", "tool2"},
			},
			want: []string{"tool1", "tool2"},
		},
		{
			name: "Interface slice",
			params: map[string]interface{}{
				"tools": []interface{}{"tool1", "tool2"},
			},
			want: []string{"tool1", "tool2"},
		},
		{
			name:   "Missing tools param",
			params: map[string]interface{}{},
			want:   []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := NewToolsNotCalledValidator(tt.params)

			// Test that it doesn't panic and basic functionality works
			result := validator.Validate("", map[string]interface{}{
				"_message_tool_calls": []types.MessageToolCall{},
			})

			// Should always pass when no tools are called
			if !result.OK {
				t.Error("Expected OK=true when no tools called")
			}
		})
	}
}
