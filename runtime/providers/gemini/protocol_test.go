package gemini

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

func TestBuildCostInfo(t *testing.T) {
	t.Run("nil usage returns nil", func(t *testing.T) {
		s := &StreamSession{inputCostPer1K: 1, outputCostPer1K: 2}
		if got := s.buildCostInfo(nil); got != nil {
			t.Errorf("expected nil CostInfo for nil usage, got %+v", got)
		}
	})

	t.Run("tokens populated but zero cost when rates unset", func(t *testing.T) {
		s := &StreamSession{} // both rates zero
		usage := &UsageMetadata{PromptTokenCount: 1000, ResponseTokenCount: 500}
		got := s.buildCostInfo(usage)
		if got == nil {
			t.Fatal("expected non-nil CostInfo")
		}
		if got.InputTokens != 1000 || got.OutputTokens != 500 {
			t.Errorf("tokens = %d/%d, want 1000/500", got.InputTokens, got.OutputTokens)
		}
		if got.InputCostUSD != 0 || got.OutputCostUSD != 0 || got.TotalCost != 0 {
			t.Errorf("expected zero cost when rates unset, got %+v", got)
		}
	})

	t.Run("cost skipped when only one rate is set", func(t *testing.T) {
		s := &StreamSession{inputCostPer1K: 3.0} // outputCostPer1K == 0
		got := s.buildCostInfo(&UsageMetadata{PromptTokenCount: 1000, ResponseTokenCount: 1000})
		if got.TotalCost != 0 {
			t.Errorf("expected zero cost when only input rate set, got %f", got.TotalCost)
		}
	})

	t.Run("full billing math", func(t *testing.T) {
		// 2000 input tokens @ $3/1K = $6.00; 1000 output @ $10/1K = $10.00
		s := &StreamSession{inputCostPer1K: 3.0, outputCostPer1K: 10.0}
		got := s.buildCostInfo(&UsageMetadata{PromptTokenCount: 2000, ResponseTokenCount: 1000})
		if got.InputCostUSD != 6.0 {
			t.Errorf("InputCostUSD = %f, want 6.0", got.InputCostUSD)
		}
		if got.OutputCostUSD != 10.0 {
			t.Errorf("OutputCostUSD = %f, want 10.0", got.OutputCostUSD)
		}
		if got.TotalCost != 16.0 {
			t.Errorf("TotalCost = %f, want 16.0", got.TotalCost)
		}
	})
}

func TestHandleToolCalls(t *testing.T) {
	t.Run("FunctionCall maps to MessageToolCall with marshaled args", func(t *testing.T) {
		s := &StreamSession{
			ctx:    context.Background(),
			emitCh: make(chan providers.StreamChunk, 4),
		}
		msg := &ToolCallMsg{FunctionCalls: []FunctionCall{
			{ID: "call_1", Name: "lookup", Args: map[string]interface{}{"q": "hi"}},
			{ID: "call_2", Name: "noargs"}, // nil Args -> "null"? see below
		}}
		if err := s.handleToolCalls(msg); err != nil {
			t.Fatalf("handleToolCalls: %v", err)
		}

		chunk := <-s.emitCh
		if chunk.FinishReason == nil || *chunk.FinishReason != "tool_calls" {
			t.Errorf("FinishReason = %v, want tool_calls", chunk.FinishReason)
		}
		if len(chunk.ToolCalls) != 2 {
			t.Fatalf("expected 2 tool calls, got %d", len(chunk.ToolCalls))
		}
		if chunk.ToolCalls[0].ID != "call_1" || chunk.ToolCalls[0].Name != "lookup" {
			t.Errorf("call[0] = %+v", chunk.ToolCalls[0])
		}
		if string(chunk.ToolCalls[0].Args) != `{"q":"hi"}` {
			t.Errorf("call[0].Args = %s, want {\"q\":\"hi\"}", chunk.ToolCalls[0].Args)
		}
		// nil map marshals to "null" (json.Marshal(nil map) == "null").
		if string(chunk.ToolCalls[1].Args) != "null" {
			t.Errorf("call[1].Args = %s, want null", chunk.ToolCalls[1].Args)
		}
	})

	t.Run("empty args map marshals to {}", func(t *testing.T) {
		s := &StreamSession{
			ctx:    context.Background(),
			emitCh: make(chan providers.StreamChunk, 1),
		}
		msg := &ToolCallMsg{FunctionCalls: []FunctionCall{
			{ID: "c", Name: "n", Args: map[string]interface{}{}},
		}}
		if err := s.handleToolCalls(msg); err != nil {
			t.Fatalf("handleToolCalls: %v", err)
		}
		chunk := <-s.emitCh
		if string(chunk.ToolCalls[0].Args) != "{}" {
			t.Errorf("Args = %s, want {}", chunk.ToolCalls[0].Args)
		}
	})
}
