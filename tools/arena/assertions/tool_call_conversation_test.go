package assertions

import (
	"context"
	"testing"
)

func TestNoToolErrorsConversationValidator(t *testing.T) {
	v := NewNoToolErrorsConversationValidator()
	ctx := context.Background()

	tests := []struct {
		name       string
		toolCalls  []ToolCallRecord
		params     map[string]interface{}
		wantPassed bool
	}{
		{
			name: "all succeed",
			toolCalls: []ToolCallRecord{
				{TurnIndex: 0, ToolName: "get_order", Result: "ok"},
				{TurnIndex: 1, ToolName: "refund", Result: "done"},
			},
			params:     map[string]interface{}{},
			wantPassed: true,
		},
		{
			name: "one error",
			toolCalls: []ToolCallRecord{
				{TurnIndex: 0, ToolName: "get_order", Result: "ok"},
				{TurnIndex: 1, ToolName: "refund", Error: "insufficient funds"},
			},
			params:     map[string]interface{}{},
			wantPassed: false,
		},
		{
			name: "scoped - error on unscoped tool passes",
			toolCalls: []ToolCallRecord{
				{TurnIndex: 0, ToolName: "get_order", Result: "ok"},
				{TurnIndex: 1, ToolName: "refund", Error: "error"},
			},
			params:     map[string]interface{}{"tools": []string{"get_order"}},
			wantPassed: true,
		},
		{
			name: "scoped - error on scoped tool fails",
			toolCalls: []ToolCallRecord{
				{TurnIndex: 0, ToolName: "refund", Error: "error"},
			},
			params:     map[string]interface{}{"tools": []string{"refund"}},
			wantPassed: false,
		},
		{
			name:       "empty tool calls",
			toolCalls:  nil,
			params:     map[string]interface{}{},
			wantPassed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conv := &ConversationContext{ToolCalls: tt.toolCalls}
			res := v.ValidateConversation(ctx, conv, tt.params)
			if res.Passed != tt.wantPassed {
				t.Errorf("Passed = %v, want %v, msg = %s", res.Passed, tt.wantPassed, res.Message)
			}
		})
	}
}

func TestToolCallCountConversationValidator(t *testing.T) {
	v := NewToolCallCountConversationValidator()
	ctx := context.Background()

	conv := &ConversationContext{
		ToolCalls: []ToolCallRecord{
			{TurnIndex: 0, ToolName: "search"},
			{TurnIndex: 1, ToolName: "search"},
			{TurnIndex: 2, ToolName: "read"},
		},
	}

	tests := []struct {
		name       string
		params     map[string]interface{}
		wantPassed bool
	}{
		{
			name:       "count all within bounds",
			params:     map[string]interface{}{"min": 1, "max": 5},
			wantPassed: true,
		},
		{
			name:       "count specific tool",
			params:     map[string]interface{}{"tool": "search", "min": 2, "max": 2},
			wantPassed: true,
		},
		{
			name:       "min violation",
			params:     map[string]interface{}{"tool": "search", "min": 5},
			wantPassed: false,
		},
		{
			name:       "max violation",
			params:     map[string]interface{}{"tool": "search", "max": 1},
			wantPassed: false,
		},
		{
			name:       "no bounds - always passes",
			params:     map[string]interface{}{},
			wantPassed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := v.ValidateConversation(ctx, conv, tt.params)
			if res.Passed != tt.wantPassed {
				t.Errorf("Passed = %v, want %v, msg = %s", res.Passed, tt.wantPassed, res.Message)
			}
		})
	}
}

func TestToolResultIncludesConversationValidator(t *testing.T) {
	v := NewToolResultIncludesConversationValidator()
	ctx := context.Background()

	conv := &ConversationContext{
		ToolCalls: []ToolCallRecord{
			{TurnIndex: 0, ToolName: "search", Result: "Found: order ORD-123 shipped"},
			{TurnIndex: 2, ToolName: "search", Result: "Found: order ORD-456 pending"},
			{TurnIndex: 3, ToolName: "other", Result: "irrelevant"},
		},
	}

	tests := []struct {
		name       string
		params     map[string]interface{}
		wantPassed bool
	}{
		{
			name: "patterns found",
			params: map[string]interface{}{
				"tool":     "search",
				"patterns": []string{"found", "order"},
			},
			wantPassed: true,
		},
		{
			name: "pattern missing",
			params: map[string]interface{}{
				"tool":     "search",
				"patterns": []string{"not_present"},
			},
			wantPassed: false,
		},
		{
			name: "occurrence met",
			params: map[string]interface{}{
				"tool":       "search",
				"patterns":   []string{"found"},
				"occurrence": 2,
			},
			wantPassed: true,
		},
		{
			name: "occurrence not met",
			params: map[string]interface{}{
				"tool":       "search",
				"patterns":   []string{"shipped"},
				"occurrence": 2,
			},
			wantPassed: false,
		},
		{
			name:       "no patterns always passes",
			params:     map[string]interface{}{"tool": "search"},
			wantPassed: true,
		},
		{
			name: "nil result handled",
			params: map[string]interface{}{
				"tool":     "nil_tool",
				"patterns": []string{"something"},
			},
			wantPassed: false,
		},
	}

	// Add a nil-result tool call for specific test
	convWithNil := &ConversationContext{
		ToolCalls: []ToolCallRecord{
			{TurnIndex: 0, ToolName: "nil_tool", Result: nil},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := conv
			if tt.name == "nil result handled" {
				c = convWithNil
			}
			res := v.ValidateConversation(ctx, c, tt.params)
			if res.Passed != tt.wantPassed {
				t.Errorf("Passed = %v, want %v, msg = %s", res.Passed, tt.wantPassed, res.Message)
			}
		})
	}
}

func TestToolResultMatchesConversationValidator(t *testing.T) {
	v := NewToolResultMatchesConversationValidator()
	ctx := context.Background()

	conv := &ConversationContext{
		ToolCalls: []ToolCallRecord{
			{TurnIndex: 0, ToolName: "get_order", Result: `{"order_id":"ORD-123"}`},
			{TurnIndex: 1, ToolName: "get_order", Result: "no match here"},
		},
	}

	tests := []struct {
		name       string
		params     map[string]interface{}
		wantPassed bool
	}{
		{
			name: "pattern matches",
			params: map[string]interface{}{
				"tool":    "get_order",
				"pattern": `ORD-\d+`,
			},
			wantPassed: true,
		},
		{
			name: "pattern no match",
			params: map[string]interface{}{
				"tool":    "get_order",
				"pattern": `NOMATCH-\d+`,
			},
			wantPassed: false,
		},
		{
			name: "invalid regex",
			params: map[string]interface{}{
				"pattern": `[invalid`,
			},
			wantPassed: false,
		},
		{
			name:       "empty pattern passes",
			params:     map[string]interface{}{},
			wantPassed: true,
		},
		{
			name: "occurrence check",
			params: map[string]interface{}{
				"tool":       "get_order",
				"pattern":    `ORD-\d+`,
				"occurrence": 2,
			},
			wantPassed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := v.ValidateConversation(ctx, conv, tt.params)
			if res.Passed != tt.wantPassed {
				t.Errorf("Passed = %v, want %v, msg = %s", res.Passed, tt.wantPassed, res.Message)
			}
		})
	}
}

func TestToolCallSequenceConversationValidator(t *testing.T) {
	v := NewToolCallSequenceConversationValidator()
	ctx := context.Background()

	conv := &ConversationContext{
		ToolCalls: []ToolCallRecord{
			{TurnIndex: 0, ToolName: "search"},
			{TurnIndex: 1, ToolName: "read"},
			{TurnIndex: 2, ToolName: "log"},
			{TurnIndex: 3, ToolName: "write"},
		},
	}

	tests := []struct {
		name       string
		params     map[string]interface{}
		wantPassed bool
	}{
		{
			name:       "full sequence match",
			params:     map[string]interface{}{"sequence": []string{"search", "read", "write"}},
			wantPassed: true,
		},
		{
			name:       "subsequence with skips",
			params:     map[string]interface{}{"sequence": []string{"search", "write"}},
			wantPassed: true,
		},
		{
			name:       "sequence not satisfied",
			params:     map[string]interface{}{"sequence": []string{"write", "search"}},
			wantPassed: false,
		},
		{
			name:       "empty sequence passes",
			params:     map[string]interface{}{},
			wantPassed: true,
		},
		{
			name:       "tool not present",
			params:     map[string]interface{}{"sequence": []string{"search", "delete"}},
			wantPassed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := v.ValidateConversation(ctx, conv, tt.params)
			if res.Passed != tt.wantPassed {
				t.Errorf("Passed = %v, want %v, msg = %s", res.Passed, tt.wantPassed, res.Message)
			}
		})
	}
}

func TestToolCallChainConversationValidator(t *testing.T) {
	v := NewToolCallChainConversationValidator()
	ctx := context.Background()

	conv := &ConversationContext{
		ToolCalls: []ToolCallRecord{
			{
				TurnIndex: 0,
				ToolName:  "get_order",
				Arguments: map[string]interface{}{"id": "123"},
				Result:    `{"order_id":"ORD-123"}`,
			},
			{
				TurnIndex: 1,
				ToolName:  "process_refund",
				Arguments: map[string]interface{}{"order_id": "ORD-123"},
				Result:    "done",
			},
			{
				TurnIndex: 2,
				ToolName:  "send_confirmation",
				Arguments: map[string]interface{}{},
				Result:    "sent",
			},
		},
	}

	tests := []struct {
		name       string
		params     map[string]interface{}
		wantPassed bool
	}{
		{
			name: "simple chain",
			params: map[string]interface{}{
				"steps": []interface{}{
					map[string]interface{}{"tool": "get_order"},
					map[string]interface{}{"tool": "process_refund"},
				},
			},
			wantPassed: true,
		},
		{
			name: "chain with result_includes",
			params: map[string]interface{}{
				"steps": []interface{}{
					map[string]interface{}{
						"tool":            "get_order",
						"result_includes": []interface{}{"order_id"},
					},
					map[string]interface{}{"tool": "process_refund"},
				},
			},
			wantPassed: true,
		},
		{
			name: "chain with args_match",
			params: map[string]interface{}{
				"steps": []interface{}{
					map[string]interface{}{
						"tool": "process_refund",
						"args_match": map[string]interface{}{
							"order_id": `ORD-\d+`,
						},
					},
				},
			},
			wantPassed: true,
		},
		{
			name: "chain fails - missing pattern",
			params: map[string]interface{}{
				"steps": []interface{}{
					map[string]interface{}{
						"tool":            "get_order",
						"result_includes": []interface{}{"not_present"},
					},
				},
			},
			wantPassed: false,
		},
		{
			name: "incomplete chain",
			params: map[string]interface{}{
				"steps": []interface{}{
					map[string]interface{}{"tool": "get_order"},
					map[string]interface{}{"tool": "process_refund"},
					map[string]interface{}{"tool": "nonexistent"},
				},
			},
			wantPassed: false,
		},
		{
			name:       "empty steps passes",
			params:     map[string]interface{}{},
			wantPassed: true,
		},
		{
			name: "no_error constraint - passes",
			params: map[string]interface{}{
				"steps": []interface{}{
					map[string]interface{}{
						"tool":     "get_order",
						"no_error": true,
					},
				},
			},
			wantPassed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := v.ValidateConversation(ctx, conv, tt.params)
			if res.Passed != tt.wantPassed {
				t.Errorf("Passed = %v, want %v, msg = %s", res.Passed, tt.wantPassed, res.Message)
			}
		})
	}

	// Test with error tool call
	t.Run("no_error constraint - fails", func(t *testing.T) {
		errorConv := &ConversationContext{
			ToolCalls: []ToolCallRecord{
				{TurnIndex: 0, ToolName: "get_order", Error: "not found"},
			},
		}
		params := map[string]interface{}{
			"steps": []interface{}{
				map[string]interface{}{"tool": "get_order", "no_error": true},
			},
		}
		res := v.ValidateConversation(ctx, errorConv, params)
		if res.Passed {
			t.Error("expected fail for error with no_error=true")
		}
	})

	// Test with interface{} Result types
	t.Run("interface result conversion", func(t *testing.T) {
		mapConv := &ConversationContext{
			ToolCalls: []ToolCallRecord{
				{TurnIndex: 0, ToolName: "api_call", Result: map[string]interface{}{"status": "ok"}},
			},
		}
		params := map[string]interface{}{
			"steps": []interface{}{
				map[string]interface{}{
					"tool":           "api_call",
					"result_matches": `status`,
				},
			},
		}
		res := v.ValidateConversation(ctx, mapConv, params)
		if !res.Passed {
			t.Errorf("expected pass for map result containing 'status', msg = %s", res.Message)
		}
	})
}
