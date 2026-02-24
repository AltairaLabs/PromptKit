package assertions

import (
	"testing"
)

func TestToolResultIncludesValidator(t *testing.T) {
	tests := []struct {
		name       string
		tool       string
		patterns   []string
		occurrence int
		toolCalls  []testToolCall
		wantPassed bool
	}{
		{
			name:     "all patterns found",
			tool:     "get_order",
			patterns: []string{"shipped", "tracking_number"},
			toolCalls: []testToolCall{
				{id: "c1", name: "get_order", result: `{"status":"shipped","tracking_number":"TRK-123"}`, round: 0},
			},
			wantPassed: true,
		},
		{
			name:     "case insensitive matching",
			tool:     "get_order",
			patterns: []string{"SHIPPED", "Tracking_Number"},
			toolCalls: []testToolCall{
				{id: "c1", name: "get_order", result: `{"status":"shipped","tracking_number":"TRK-123"}`, round: 0},
			},
			wantPassed: true,
		},
		{
			name:     "missing pattern",
			tool:     "get_order",
			patterns: []string{"shipped", "refunded"},
			toolCalls: []testToolCall{
				{id: "c1", name: "get_order", result: `{"status":"shipped"}`, round: 0},
			},
			wantPassed: false,
		},
		{
			name:       "occurrence threshold met",
			tool:       "get_order",
			patterns:   []string{"shipped"},
			occurrence: 2,
			toolCalls: []testToolCall{
				{id: "c1", name: "get_order", result: `shipped`, round: 0},
				{id: "c2", name: "get_order", result: `shipped`, round: 0},
			},
			wantPassed: true,
		},
		{
			name:       "occurrence threshold not met",
			tool:       "get_order",
			patterns:   []string{"shipped"},
			occurrence: 3,
			toolCalls: []testToolCall{
				{id: "c1", name: "get_order", result: `shipped`, round: 0},
				{id: "c2", name: "get_order", result: `shipped`, round: 0},
			},
			wantPassed: false,
		},
		{
			name:     "tool not called",
			tool:     "get_order",
			patterns: []string{"shipped"},
			toolCalls: []testToolCall{
				{id: "c1", name: "other_tool", result: "shipped", round: 0},
			},
			wantPassed: false,
		},
		{
			name:       "no patterns - always passes",
			tool:       "get_order",
			patterns:   []string{},
			wantPassed: true,
		},
		{
			name:     "check all tools when tool not specified",
			patterns: []string{"success"},
			toolCalls: []testToolCall{
				{id: "c1", name: "tool_a", result: "success", round: 0},
			},
			wantPassed: true,
		},
		{
			name:       "duplex path skipped",
			tool:       "get_order",
			patterns:   []string{"shipped"},
			wantPassed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := map[string]interface{}{
				"patterns": tt.patterns,
			}
			if tt.tool != "" {
				params["tool"] = tt.tool
			}
			if tt.occurrence > 0 {
				params["occurrence"] = float64(tt.occurrence)
			}
			validator := NewToolResultIncludesValidator(params)

			var enhancedParams map[string]interface{}
			if tt.name == "duplex path skipped" {
				enhancedParams = map[string]interface{}{}
			} else {
				enhancedParams = map[string]interface{}{
					"_turn_messages": buildTurnMessages(tt.toolCalls...),
				}
			}

			result := validator.Validate("", enhancedParams)
			if result.Passed != tt.wantPassed {
				t.Errorf("Validate() Passed = %v, want %v, details = %v",
					result.Passed, tt.wantPassed, result.Details)
			}
		})
	}
}
