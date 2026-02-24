package assertions

import (
	"testing"
)

func TestToolCallCountValidator(t *testing.T) {
	tests := []struct {
		name       string
		tool       string
		min        interface{}
		max        interface{}
		toolCalls  []testToolCall
		wantPassed bool
	}{
		{
			name: "count all tools - within min",
			min:  float64(2),
			toolCalls: []testToolCall{
				{id: "c1", name: "a", result: "ok", round: 0},
				{id: "c2", name: "b", result: "ok", round: 0},
				{id: "c3", name: "c", result: "ok", round: 0},
			},
			wantPassed: true,
		},
		{
			name: "count all tools - below min",
			min:  float64(3),
			toolCalls: []testToolCall{
				{id: "c1", name: "a", result: "ok", round: 0},
			},
			wantPassed: false,
		},
		{
			name: "count all tools - above max",
			max:  float64(1),
			toolCalls: []testToolCall{
				{id: "c1", name: "a", result: "ok", round: 0},
				{id: "c2", name: "b", result: "ok", round: 0},
			},
			wantPassed: false,
		},
		{
			name: "count specific tool",
			tool: "get_order",
			min:  float64(1),
			max:  float64(2),
			toolCalls: []testToolCall{
				{id: "c1", name: "get_order", result: "ok", round: 0},
				{id: "c2", name: "other", result: "ok", round: 0},
				{id: "c3", name: "get_order", result: "ok", round: 0},
			},
			wantPassed: true,
		},
		{
			name: "max zero is valid - tool not called",
			tool: "forbidden",
			max:  float64(0),
			toolCalls: []testToolCall{
				{id: "c1", name: "allowed", result: "ok", round: 0},
			},
			wantPassed: true,
		},
		{
			name: "max zero - tool called once fails",
			tool: "forbidden",
			max:  float64(0),
			toolCalls: []testToolCall{
				{id: "c1", name: "forbidden", result: "ok", round: 0},
			},
			wantPassed: false,
		},
		{
			name:       "no bounds set - always passes",
			toolCalls:  []testToolCall{{id: "c1", name: "a", result: "ok", round: 0}},
			wantPassed: true,
		},
		{
			name:       "duplex path skipped",
			wantPassed: true,
		},
		{
			name: "int params",
			min:  2,
			toolCalls: []testToolCall{
				{id: "c1", name: "a", result: "ok", round: 0},
				{id: "c2", name: "b", result: "ok", round: 0},
			},
			wantPassed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := map[string]interface{}{}
			if tt.tool != "" {
				params["tool"] = tt.tool
			}
			if tt.min != nil {
				params["min"] = tt.min
			}
			if tt.max != nil {
				params["max"] = tt.max
			}
			validator := NewToolCallCountValidator(params)

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
