package assertions

import (
	"testing"
)

func TestToolCallChainValidator(t *testing.T) {
	tests := []struct {
		name       string
		steps      []interface{}
		toolCalls  []testToolCall
		wantPassed bool
	}{
		{
			name: "simple chain satisfied",
			steps: []interface{}{
				map[string]interface{}{"tool": "get_order"},
				map[string]interface{}{"tool": "process_refund"},
			},
			toolCalls: []testToolCall{
				{id: "c1", name: "get_order", result: `{"order_id":"ORD-1"}`, round: 0},
				{id: "c2", name: "process_refund", result: "done", round: 1},
			},
			wantPassed: true,
		},
		{
			name: "chain with result_includes constraint",
			steps: []interface{}{
				map[string]interface{}{
					"tool":            "get_order",
					"result_includes": []interface{}{"order_id"},
				},
				map[string]interface{}{"tool": "process_refund"},
			},
			toolCalls: []testToolCall{
				{id: "c1", name: "get_order", result: `{"order_id":"ORD-1"}`, round: 0},
				{id: "c2", name: "process_refund", result: "done", round: 1},
			},
			wantPassed: true,
		},
		{
			name: "chain with result_includes fails",
			steps: []interface{}{
				map[string]interface{}{
					"tool":            "get_order",
					"result_includes": []interface{}{"not_present"},
				},
			},
			toolCalls: []testToolCall{
				{id: "c1", name: "get_order", result: `{"order_id":"ORD-1"}`, round: 0},
			},
			wantPassed: false,
		},
		{
			name: "chain with result_matches constraint",
			steps: []interface{}{
				map[string]interface{}{
					"tool":           "get_order",
					"result_matches": `ORD-\d+`,
				},
			},
			toolCalls: []testToolCall{
				{id: "c1", name: "get_order", result: `{"order_id":"ORD-123"}`, round: 0},
			},
			wantPassed: true,
		},
		{
			name: "chain with result_matches fails",
			steps: []interface{}{
				map[string]interface{}{
					"tool":           "get_order",
					"result_matches": `ORD-\d+`,
				},
			},
			toolCalls: []testToolCall{
				{id: "c1", name: "get_order", result: `no order`, round: 0},
			},
			wantPassed: false,
		},
		{
			name: "chain with args_match constraint",
			steps: []interface{}{
				map[string]interface{}{
					"tool": "process_refund",
					"args_match": map[string]interface{}{
						"order_id": `ORD-\d+`,
					},
				},
			},
			toolCalls: []testToolCall{
				{
					id:     "c1",
					name:   "process_refund",
					args:   map[string]interface{}{"order_id": "ORD-456"},
					result: "done",
					round:  0,
				},
			},
			wantPassed: true,
		},
		{
			name: "chain with args_match fails - pattern mismatch",
			steps: []interface{}{
				map[string]interface{}{
					"tool": "process_refund",
					"args_match": map[string]interface{}{
						"order_id": `ORD-\d+`,
					},
				},
			},
			toolCalls: []testToolCall{
				{
					id:     "c1",
					name:   "process_refund",
					args:   map[string]interface{}{"order_id": "INVALID"},
					result: "done",
					round:  0,
				},
			},
			wantPassed: false,
		},
		{
			name: "chain with args_match fails - missing arg",
			steps: []interface{}{
				map[string]interface{}{
					"tool": "process_refund",
					"args_match": map[string]interface{}{
						"order_id": `ORD-\d+`,
					},
				},
			},
			toolCalls: []testToolCall{
				{
					id:     "c1",
					name:   "process_refund",
					args:   map[string]interface{}{},
					result: "done",
					round:  0,
				},
			},
			wantPassed: false,
		},
		{
			name: "chain with no_error constraint - succeeds",
			steps: []interface{}{
				map[string]interface{}{
					"tool":     "get_order",
					"no_error": true,
				},
			},
			toolCalls: []testToolCall{
				{id: "c1", name: "get_order", result: "ok", round: 0},
			},
			wantPassed: true,
		},
		{
			name: "chain with no_error constraint - fails",
			steps: []interface{}{
				map[string]interface{}{
					"tool":     "get_order",
					"no_error": true,
				},
			},
			toolCalls: []testToolCall{
				{id: "c1", name: "get_order", err: "not found", round: 0},
			},
			wantPassed: false,
		},
		{
			name: "incomplete chain - missing step",
			steps: []interface{}{
				map[string]interface{}{"tool": "get_order"},
				map[string]interface{}{"tool": "process_refund"},
				map[string]interface{}{"tool": "send_confirmation"},
			},
			toolCalls: []testToolCall{
				{id: "c1", name: "get_order", result: "ok", round: 0},
				{id: "c2", name: "process_refund", result: "done", round: 1},
			},
			wantPassed: false,
		},
		{
			name:       "empty chain always passes",
			steps:      []interface{}{},
			toolCalls:  []testToolCall{{id: "c1", name: "a", result: "ok", round: 0}},
			wantPassed: true,
		},
		{
			name: "duplex path skipped",
			steps: []interface{}{
				map[string]interface{}{"tool": "get_order"},
			},
			wantPassed: true,
		},
		{
			name: "chain skips non-matching tools",
			steps: []interface{}{
				map[string]interface{}{"tool": "get_order"},
				map[string]interface{}{"tool": "process_refund"},
			},
			toolCalls: []testToolCall{
				{id: "c1", name: "get_order", result: "ok", round: 0},
				{id: "c2", name: "log_event", result: "logged", round: 0},
				{id: "c3", name: "process_refund", result: "done", round: 1},
			},
			wantPassed: true,
		},
		{
			name: "invalid regex in result_matches",
			steps: []interface{}{
				map[string]interface{}{
					"tool":           "get_order",
					"result_matches": `[invalid`,
				},
			},
			toolCalls: []testToolCall{
				{id: "c1", name: "get_order", result: "anything", round: 0},
			},
			wantPassed: false,
		},
		{
			name: "invalid regex in args_match",
			steps: []interface{}{
				map[string]interface{}{
					"tool": "get_order",
					"args_match": map[string]interface{}{
						"id": `[invalid`,
					},
				},
			},
			toolCalls: []testToolCall{
				{id: "c1", name: "get_order", args: map[string]interface{}{"id": "123"}, result: "ok", round: 0},
			},
			wantPassed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := map[string]interface{}{
				"steps": tt.steps,
			}
			validator := NewToolCallChainValidator(params)

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
