package assertions

import (
	"testing"
)

func TestToolResultMatchesValidator(t *testing.T) {
	tests := []struct {
		name       string
		tool       string
		pattern    string
		occurrence int
		toolCalls  []testToolCall
		wantPassed bool
	}{
		{
			name:    "pattern matches",
			tool:    "get_order",
			pattern: `status.*shipped`,
			toolCalls: []testToolCall{
				{id: "c1", name: "get_order", result: `{"status":"shipped"}`, round: 0},
			},
			wantPassed: true,
		},
		{
			name:    "pattern does not match",
			tool:    "get_order",
			pattern: `status.*refunded`,
			toolCalls: []testToolCall{
				{id: "c1", name: "get_order", result: `{"status":"shipped"}`, round: 0},
			},
			wantPassed: false,
		},
		{
			name:    "invalid regex",
			tool:    "get_order",
			pattern: `[invalid`,
			toolCalls: []testToolCall{
				{id: "c1", name: "get_order", result: "anything", round: 0},
			},
			wantPassed: false,
		},
		{
			name:       "occurrence threshold",
			tool:       "get_order",
			pattern:    `shipped`,
			occurrence: 2,
			toolCalls: []testToolCall{
				{id: "c1", name: "get_order", result: "shipped", round: 0},
				{id: "c2", name: "get_order", result: "shipped", round: 0},
			},
			wantPassed: true,
		},
		{
			name:       "occurrence threshold not met",
			tool:       "get_order",
			pattern:    `shipped`,
			occurrence: 2,
			toolCalls: []testToolCall{
				{id: "c1", name: "get_order", result: "shipped", round: 0},
				{id: "c2", name: "get_order", result: "pending", round: 0},
			},
			wantPassed: false,
		},
		{
			name:       "empty pattern always passes",
			tool:       "get_order",
			pattern:    "",
			wantPassed: true,
		},
		{
			name:    "check all tools when tool not specified",
			pattern: `ok`,
			toolCalls: []testToolCall{
				{id: "c1", name: "any_tool", result: "ok", round: 0},
			},
			wantPassed: true,
		},
		{
			name:       "duplex path skipped",
			tool:       "get_order",
			pattern:    `shipped`,
			wantPassed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := map[string]interface{}{}
			if tt.tool != "" {
				params["tool"] = tt.tool
			}
			if tt.pattern != "" {
				params["pattern"] = tt.pattern
			}
			if tt.occurrence > 0 {
				params["occurrence"] = float64(tt.occurrence)
			}
			validator := NewToolResultMatchesValidator(params)

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
