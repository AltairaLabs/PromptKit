package assertions

import (
	"testing"
)

func TestNoToolErrorsValidator(t *testing.T) {
	tests := []struct {
		name       string
		tools      []string
		toolCalls  []testToolCall
		wantPassed bool
	}{
		{
			name: "all tools succeed",
			toolCalls: []testToolCall{
				{id: "c1", name: "get_order", result: "ok", round: 0},
				{id: "c2", name: "refund", result: "done", round: 0},
			},
			wantPassed: true,
		},
		{
			name: "one tool has error",
			toolCalls: []testToolCall{
				{id: "c1", name: "get_order", result: "ok", round: 0},
				{id: "c2", name: "refund", err: "insufficient funds", round: 0},
			},
			wantPassed: false,
		},
		{
			name:  "scoped to specific tools - error on unscoped tool",
			tools: []string{"get_order"},
			toolCalls: []testToolCall{
				{id: "c1", name: "get_order", result: "ok", round: 0},
				{id: "c2", name: "refund", err: "error", round: 0},
			},
			wantPassed: true,
		},
		{
			name:  "scoped to specific tools - error on scoped tool",
			tools: []string{"refund"},
			toolCalls: []testToolCall{
				{id: "c1", name: "get_order", result: "ok", round: 0},
				{id: "c2", name: "refund", err: "error", round: 0},
			},
			wantPassed: false,
		},
		{
			name:       "no tool calls",
			toolCalls:  []testToolCall{},
			wantPassed: true,
		},
		{
			name:       "duplex path - no turn messages",
			wantPassed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := map[string]interface{}{}
			if tt.tools != nil {
				params["tools"] = tt.tools
			}
			validator := NewNoToolErrorsValidator(params)

			var enhancedParams map[string]interface{}
			if tt.name == "duplex path - no turn messages" {
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
