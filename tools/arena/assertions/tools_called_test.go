package assertions

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestToolsCalledValidator(t *testing.T) {
	tests := []struct {
		name          string
		expectedTools []string
		actualCalls   []types.MessageToolCall
		wantPassed    bool
		wantMissing   []string
	}{
		{
			name:          "All expected tools called",
			expectedTools: []string{"get_customer_info", "check_subscription_status"},
			actualCalls: []types.MessageToolCall{
				{Name: "get_customer_info"},
				{Name: "check_subscription_status"},
			},
			wantPassed:  true,
			wantMissing: nil,
		},
		{
			name:          "Missing one tool",
			expectedTools: []string{"get_customer_info", "check_subscription_status"},
			actualCalls: []types.MessageToolCall{
				{Name: "get_customer_info"},
			},
			wantPassed:  false,
			wantMissing: []string{"check_subscription_status"},
		},
		{
			name:          "Missing all tools",
			expectedTools: []string{"get_customer_info", "check_subscription_status"},
			actualCalls:   []types.MessageToolCall{},
			wantPassed:    false,
			wantMissing:   []string{"get_customer_info", "check_subscription_status"},
		},
		{
			name:          "Extra tools called is OK",
			expectedTools: []string{"get_customer_info"},
			actualCalls: []types.MessageToolCall{
				{Name: "get_customer_info"},
				{Name: "extra_tool"},
			},
			wantPassed:  true,
			wantMissing: nil,
		},
		{
			name:          "No expected tools",
			expectedTools: []string{},
			actualCalls: []types.MessageToolCall{
				{Name: "some_tool"},
			},
			wantPassed:  true,
			wantMissing: nil,
		},
		{
			name:          "Tool called multiple times",
			expectedTools: []string{"get_customer_info"},
			actualCalls: []types.MessageToolCall{
				{Name: "get_customer_info"},
				{Name: "get_customer_info"},
			},
			wantPassed:  true,
			wantMissing: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create validator with factory pattern
			params := map[string]interface{}{
				"tools": tt.expectedTools,
			}
			validator := NewToolsCalledValidator(params)

			// Prepare enhanced params with tool calls
			enhancedParams := map[string]interface{}{
				"_message_tool_calls": tt.actualCalls,
			}

			// Validate
			result := validator.Validate("", enhancedParams)

			if result.Passed != tt.wantPassed {
				t.Errorf("Validate() OK = %v, want %v", result.Passed, tt.wantPassed)
			}

			// Check missing tools
			if !result.Passed {
				details, ok := result.Details.(map[string]interface{})
				if !ok {
					t.Fatalf("Expected details to be map[string]interface{}, got %T", result.Details)
				}

				missing, ok := details["missing_tools"].([]string)
				if !ok {
					t.Fatalf("Expected missing_tools to be []string, got %T", details["missing_tools"])
				}

				if len(missing) != len(tt.wantMissing) {
					t.Errorf("Got %d missing tools, want %d: %v", len(missing), len(tt.wantMissing), missing)
				}

				for i, want := range tt.wantMissing {
					if i >= len(missing) || missing[i] != want {
						t.Errorf("Missing tool %d = %v, want %v", i, missing, tt.wantMissing)
					}
				}
			}
		})
	}
}

func TestToolsCalledValidator_FactoryWithSliceTypes(t *testing.T) {
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
		{
			name: "Invalid param type",
			params: map[string]interface{}{
				"tools": "not a slice",
			},
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := NewToolsCalledValidator(tt.params)

			// Use reflection or test behavior to verify
			// For now, test that it doesn't panic and basic functionality works
			result := validator.Validate("", map[string]interface{}{
				"_message_tool_calls": []types.MessageToolCall{},
			})

			// Should pass if no tools are expected or fail if tools are expected
			if len(tt.want) == 0 && !result.Passed {
				t.Error("Expected OK=true when no tools expected")
			}
		})
	}
}
