package assertions

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestToolsNotCalledWithArgsConversationValidator(t *testing.T) {
	v := NewToolsNotCalledWithArgsConversationValidator()
	ctx := context.Background()

	conv := &ConversationContext{
		ToolCalls: []ToolCallRecord{
			{TurnIndex: 1, ToolName: "get_sensor_data", Arguments: map[string]interface{}{"device_id": "MOTOR-001"}},
			{TurnIndex: 2, ToolName: "get_sensor_data", Arguments: map[string]interface{}{"device_id": "TURBINE-101"}},
		},
	}

	params := map[string]interface{}{
		"tool_name": "get_sensor_data",
		"forbidden_args": map[string]interface{}{
			"device_id": []interface{}{"TURBINE-101"},
		},
	}
	res := v.ValidateConversation(ctx, conv, params)
	if res.Passed {
		t.Fatalf("expected fail due to forbidden arg; got pass")
	}
	if len(res.Violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(res.Violations))
	}

	// Passing case
	params2 := map[string]interface{}{
		"tool_name": "get_sensor_data",
		"forbidden_args": map[string]interface{}{
			"device_id": []string{"COMPRESSOR-102"},
		},
	}
	res2 := v.ValidateConversation(ctx, conv, params2)
	if !res2.Passed {
		t.Fatalf("expected pass; got fail: %+v", res2)
	}
}

func TestContentNotIncludesConversationValidator(t *testing.T) {
	v := NewContentNotIncludesConversationValidator()
	ctx := context.Background()

	conv := &ConversationContext{
		AllTurns: []types.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Your device TURBINE-101 is ready"},
			{Role: "assistant", Content: "All good"},
		},
	}

	params := map[string]interface{}{
		"patterns": []string{"TURBINE-101"},
	}
	res := v.ValidateConversation(ctx, conv, params)
	if res.Passed {
		t.Fatalf("expected fail due to forbidden content; got pass")
	}
	if len(res.Violations) == 0 {
		t.Fatalf("expected violations to be reported")
	}

	params2 := map[string]interface{}{
		"patterns": []string{"globex"},
	}
	res2 := v.ValidateConversation(ctx, conv, params2)
	if !res2.Passed {
		t.Fatalf("expected pass; got fail: %+v", res2)
	}
}
