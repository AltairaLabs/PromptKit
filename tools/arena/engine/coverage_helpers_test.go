package engine

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestCreateErrorResult(t *testing.T) {
	ce := &DefaultConversationExecutor{}
	res := ce.createErrorResult("fail")

	if !res.Failed || res.Error != "fail" {
		t.Fatalf("unexpected error result: %+v", res)
	}
}

func TestAggregateHelpers(t *testing.T) {
	ce := &DefaultConversationExecutor{}

	total := types.CostInfo{}
	ce.aggregateMessageCost(&total, &types.CostInfo{
		InputTokens:  10,
		OutputTokens: 5,
		TotalCost:    1.5,
	})
	if total.InputTokens != 10 || total.OutputTokens != 5 || total.TotalCost != 1.5 {
		t.Fatalf("cost aggregation failed: %+v", total)
	}

	stats := &types.ToolStats{TotalCalls: 0, ByTool: make(map[string]int)}
	ce.aggregateToolStats(stats, []types.MessageToolCall{
		{Name: "toolA"},
		{Name: "toolB"},
		{Name: "toolA"},
	})
	if stats.TotalCalls != 3 {
		t.Fatalf("expected 3 tool calls, got %d", stats.TotalCalls)
	}
	if stats.ByTool["toolA"] != 2 || stats.ByTool["toolB"] != 1 {
		t.Fatalf("unexpected tool stats: %+v", stats.ByTool)
	}
}
