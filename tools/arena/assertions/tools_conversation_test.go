package assertions

import (
	"context"
	"testing"
)

func TestToolsCalledConversationValidator(t *testing.T) {
	v := NewToolsCalledConversationValidator()
	ctx := context.Background()

	conv := &ConversationContext{
		ToolCalls: []ToolCallRecord{
			{TurnIndex: 1, ToolName: "list_devices"},
			{TurnIndex: 2, ToolName: "get_sensor_data"},
			{TurnIndex: 3, ToolName: "get_sensor_data"},
		},
	}

	params := map[string]interface{}{
		"tool_names": []string{"list_devices", "get_sensor_data"},
		"min_calls":  1,
	}
	res := v.ValidateConversation(ctx, conv, params)
	if !res.Passed {
		t.Fatalf("expected pass, got: %+v", res)
	}

	// require at least 2 calls to get_sensor_data
	params2 := map[string]interface{}{
		"tool_names": []string{"get_sensor_data"},
		"min_calls":  2,
	}
	res2 := v.ValidateConversation(ctx, conv, params2)
	if !res2.Passed {
		t.Fatalf("expected pass for min_calls=2, got: %+v", res2)
	}

	// missing tool
	params3 := map[string]interface{}{
		"tool_names": []string{"admin_override"},
	}
	res3 := v.ValidateConversation(ctx, conv, params3)
	if res3.Passed {
		t.Fatalf("expected fail when required tool missing")
	}
}

func TestToolsNotCalledConversationValidator(t *testing.T) {
	v := NewToolsNotCalledConversationValidator()
	ctx := context.Background()

	conv := &ConversationContext{
		ToolCalls: []ToolCallRecord{
			{TurnIndex: 0, ToolName: "list_devices"},
			{TurnIndex: 1, ToolName: "get_sensor_data"},
		},
	}

	params := map[string]interface{}{
		"tool_names": []string{"delete_device", "admin_override"},
	}
	res := v.ValidateConversation(ctx, conv, params)
	if !res.Passed {
		t.Fatalf("expected pass (no forbidden tools), got: %+v", res)
	}

	params2 := map[string]interface{}{
		"tool_names": []string{"get_sensor_data"},
	}
	res2 := v.ValidateConversation(ctx, conv, params2)
	if res2.Passed {
		t.Fatalf("expected fail (forbidden tool called), got pass")
	}
	if len(res2.Violations) == 0 {
		t.Fatalf("expected violations to be reported")
	}
}
