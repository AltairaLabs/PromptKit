package assertions

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mockConvCtxWithToolCalls(verdict string, records []ToolCallRecord) (*ConversationContext, map[string]interface{}) {
	repo := mock.NewInMemoryMockRepository(verdict)
	spec := providers.ProviderSpec{
		ID:               "mock-judge",
		Type:             "mock",
		Model:            "judge-model",
		AdditionalConfig: map[string]interface{}{"repository": repo},
	}
	conv := &ConversationContext{
		AllTurns: []types.Message{
			{Role: "user", Content: "Do something"},
			{Role: "assistant", Content: "Done"},
		},
		ToolCalls: records,
		Metadata: ConversationMetadata{
			Extras: map[string]interface{}{
				"judge_targets": map[string]providers.ProviderSpec{"default": spec},
			},
		},
	}
	params := map[string]interface{}{
		"criteria": "evaluate tool usage across conversation",
	}
	return conv, params
}

func TestLLMJudgeToolCallsConversation_Pass(t *testing.T) {
	conv, params := mockConvCtxWithToolCalls(
		`{"passed":true,"score":0.9,"reasoning":"good"}`,
		[]ToolCallRecord{{ToolName: "search", Arguments: map[string]interface{}{"q": "test"}, Result: "found"}},
	)
	v := NewLLMJudgeToolCallsConversationValidator()
	res := v.ValidateConversation(context.Background(), conv, params)
	assert.True(t, res.Passed)
	assert.Equal(t, "llm_judge_tool_calls", res.Type)
	assert.Equal(t, 1, res.Details["tool_calls_sent"])
}

func TestLLMJudgeToolCallsConversation_Fail(t *testing.T) {
	conv, params := mockConvCtxWithToolCalls(
		`{"passed":false,"score":0.2,"reasoning":"bad"}`,
		[]ToolCallRecord{{ToolName: "delete", Arguments: map[string]interface{}{}, Result: "ok"}},
	)
	v := NewLLMJudgeToolCallsConversationValidator()
	res := v.ValidateConversation(context.Background(), conv, params)
	assert.False(t, res.Passed)
}

func TestLLMJudgeToolCallsConversation_SkippedNoToolCalls(t *testing.T) {
	conv, params := mockConvCtxWithToolCalls(
		`{"passed":true,"score":1.0,"reasoning":"ok"}`,
		nil, // no tool calls
	)
	v := NewLLMJudgeToolCallsConversationValidator()
	res := v.ValidateConversation(context.Background(), conv, params)
	assert.True(t, res.Passed)
	assert.True(t, res.Details["skipped"].(bool))
}

func TestLLMJudgeToolCallsConversation_FilterByTools(t *testing.T) {
	conv, params := mockConvCtxWithToolCalls(
		`{"passed":true,"score":0.8,"reasoning":"ok"}`,
		[]ToolCallRecord{
			{ToolName: "search", Arguments: map[string]interface{}{}, Result: "found"},
			{ToolName: "delete", Arguments: map[string]interface{}{}, Result: "ok"},
		},
	)
	params["tools"] = []interface{}{"search"}
	v := NewLLMJudgeToolCallsConversationValidator()
	res := v.ValidateConversation(context.Background(), conv, params)
	assert.True(t, res.Passed)
	assert.Equal(t, 1, res.Details["tool_calls_sent"])
}

func TestLLMJudgeToolCallsConversation_FilterNoMatch(t *testing.T) {
	conv, params := mockConvCtxWithToolCalls(
		`{"passed":true,"score":1.0,"reasoning":"ok"}`,
		[]ToolCallRecord{{ToolName: "delete", Arguments: map[string]interface{}{}, Result: "ok"}},
	)
	params["tools"] = []interface{}{"search"}
	v := NewLLMJudgeToolCallsConversationValidator()
	res := v.ValidateConversation(context.Background(), conv, params)
	assert.True(t, res.Passed)
	assert.True(t, res.Details["skipped"].(bool))
}

func TestLLMJudgeToolCallsConversation_MinScore(t *testing.T) {
	conv, params := mockConvCtxWithToolCalls(
		`{"passed":true,"score":0.5,"reasoning":"ok"}`,
		[]ToolCallRecord{{ToolName: "search", Arguments: map[string]interface{}{}, Result: "ok"}},
	)
	params["min_score"] = 0.8
	v := NewLLMJudgeToolCallsConversationValidator()
	res := v.ValidateConversation(context.Background(), conv, params)
	assert.False(t, res.Passed)
}

func TestLLMJudgeToolCallsConversation_MissingTargets(t *testing.T) {
	conv := &ConversationContext{
		AllTurns:  []types.Message{{Role: "assistant", Content: "hi"}},
		ToolCalls: []ToolCallRecord{{ToolName: "search", Arguments: map[string]interface{}{}, Result: "ok"}},
		Metadata:  ConversationMetadata{},
	}
	v := NewLLMJudgeToolCallsConversationValidator()
	res := v.ValidateConversation(context.Background(), conv, map[string]interface{}{"criteria": "test"})
	assert.False(t, res.Passed)
	assert.Contains(t, res.Message, "judge_targets")
}

func TestFilterConversationToolCalls_NoFilter(t *testing.T) {
	records := []ToolCallRecord{
		{ToolName: "a", Result: "r1"},
		{ToolName: "b", Result: "r2"},
	}
	views := filterConversationToolCalls(records, nil)
	require.Len(t, views, 2)
}

func TestFilterConversationToolCalls_WithFilter(t *testing.T) {
	records := []ToolCallRecord{
		{ToolName: "search", Result: "r1"},
		{ToolName: "delete", Result: "r2"},
		{ToolName: "search", Result: "r3"},
	}
	views := filterConversationToolCalls(records, []string{"search"})
	require.Len(t, views, 2)
	assert.Equal(t, "search", views[0].Name)
	assert.Equal(t, "search", views[1].Name)
}

func TestFormatToolCallViewsForJudge(t *testing.T) {
	views := []ToolCallView{
		{Name: "search", Args: map[string]interface{}{"q": "test"}, Result: "found", Index: 0},
		{Name: "book", Args: map[string]interface{}{}, Result: "", Error: "unavailable", Index: 1},
	}
	text := formatToolCallViewsForJudge(views)
	assert.Contains(t, text, "TOOL CALL 1 (turn 0):")
	assert.Contains(t, text, "Tool: search")
	assert.Contains(t, text, "Result: found")
	assert.Contains(t, text, "TOOL CALL 2 (turn 1):")
	assert.Contains(t, text, "Error: unavailable")
}
