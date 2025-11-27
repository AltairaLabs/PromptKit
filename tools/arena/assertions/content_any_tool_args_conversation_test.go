package assertions

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestContentIncludesAnyConversationValidator(t *testing.T) {
	v := NewContentIncludesAnyConversationValidator()
	ctx := context.Background()

	conv := &ConversationContext{
		AllTurns: []types.Message{
			{Role: "assistant", Content: "We recommend a diagnosis."},
		},
	}
	params := map[string]interface{}{"patterns": []string{"recommend", "suggest", "diagnosis"}}
	res := v.ValidateConversation(ctx, conv, params)
	if !res.Passed {
		t.Fatalf("expected pass; got fail: %+v", res)
	}

	conv2 := &ConversationContext{AllTurns: []types.Message{{Role: "assistant", Content: "No advice here."}}}
	res2 := v.ValidateConversation(ctx, conv2, params)
	if res2.Passed {
		t.Fatalf("expected fail when none of patterns appear")
	}
}

func TestToolCallsWithArgsConversationValidator(t *testing.T) {
	v := NewToolCallsWithArgsConversationValidator()
	ctx := context.Background()

	conv := &ConversationContext{
		ToolCalls: []ToolCallRecord{
			{TurnIndex: 1, ToolName: "get_sensor_data", Arguments: map[string]interface{}{"customer_id": "acme-corp"}},
			{TurnIndex: 2, ToolName: "get_sensor_data", Arguments: map[string]interface{}{"customer_id": "acme-corp"}},
		},
	}
	params := map[string]interface{}{"tool_name": "get_sensor_data", "required_args": map[string]interface{}{"customer_id": "acme-corp"}}
	res := v.ValidateConversation(ctx, conv, params)
	if !res.Passed {
		t.Fatalf("expected pass; got fail: %+v", res)
	}

	conv2 := &ConversationContext{ToolCalls: []ToolCallRecord{{TurnIndex: 1, ToolName: "get_sensor_data", Arguments: map[string]interface{}{"customer_id": "globex"}}}}
	res2 := v.ValidateConversation(ctx, conv2, params)
	if res2.Passed {
		t.Fatalf("expected fail when argument value mismatch")
	}
}
