package assertions

import (
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mockJudgeParams(verdict string, toolCalls []TurnToolCall) map[string]interface{} {
	repo := mock.NewInMemoryMockRepository(verdict)
	spec := providers.ProviderSpec{
		ID:               "mock-judge",
		Type:             "mock",
		Model:            "judge-model",
		AdditionalConfig: map[string]interface{}{"repository": repo},
	}

	// Build turn messages from tool calls
	var msgs []types.Message
	if len(toolCalls) > 0 {
		var mtcs []types.MessageToolCall
		for _, tc := range toolCalls {
			rawArgs, _ := json.Marshal(tc.Args)
			mtcs = append(mtcs, types.MessageToolCall{ID: tc.CallID, Name: tc.Name, Args: rawArgs})
		}
		msgs = append(msgs, types.Message{Role: "assistant", ToolCalls: mtcs})
		for _, tc := range toolCalls {
			msgs = append(msgs, types.Message{
				Role:       "tool",
				ToolResult: &types.MessageToolResult{ID: tc.CallID, Name: tc.Name, Content: tc.Result, Error: tc.Error},
			})
		}
	}

	return map[string]interface{}{
		"criteria":         "evaluate tool usage",
		"_turn_messages":   msgs,
		"_metadata":        map[string]interface{}{"judge_targets": map[string]providers.ProviderSpec{"default": spec}},
	}
}

func TestLLMJudgeToolCalls_BasicPass(t *testing.T) {
	params := mockJudgeParams(
		`{"passed":true,"score":0.9,"reasoning":"good tool usage"}`,
		[]TurnToolCall{{CallID: "1", Name: "search", Args: map[string]interface{}{"query": "test"}, Result: "found"}},
	)
	v := NewLLMJudgeToolCallsValidator(nil)
	result := v.Validate("", params)
	assert.True(t, result.Passed)
	assert.Equal(t, 1, result.Details.(map[string]interface{})["tool_calls_sent"])
}

func TestLLMJudgeToolCalls_BasicFail(t *testing.T) {
	params := mockJudgeParams(
		`{"passed":false,"score":0.2,"reasoning":"bad tool usage"}`,
		[]TurnToolCall{{CallID: "1", Name: "search", Args: map[string]interface{}{"query": ""}, Result: "error"}},
	)
	v := NewLLMJudgeToolCallsValidator(nil)
	result := v.Validate("", params)
	assert.False(t, result.Passed)
}

func TestLLMJudgeToolCalls_SkippedWhenNoTrace(t *testing.T) {
	v := NewLLMJudgeToolCallsValidator(nil)
	result := v.Validate("", map[string]interface{}{"criteria": "test"})
	assert.True(t, result.Passed)
	assert.True(t, result.Details.(map[string]interface{})["skipped"].(bool))
}

func TestLLMJudgeToolCalls_SkippedWhenNoMatchingCalls(t *testing.T) {
	params := mockJudgeParams(
		`{"passed":true,"score":1.0,"reasoning":"ok"}`,
		[]TurnToolCall{{CallID: "1", Name: "delete", Args: map[string]interface{}{}, Result: "ok"}},
	)
	params["tools"] = []interface{}{"search", "lookup"}
	v := NewLLMJudgeToolCallsValidator(nil)
	result := v.Validate("", params)
	assert.True(t, result.Passed)
	assert.True(t, result.Details.(map[string]interface{})["skipped"].(bool))
}

func TestLLMJudgeToolCalls_FilterByToolNames(t *testing.T) {
	params := mockJudgeParams(
		`{"passed":true,"score":0.8,"reasoning":"ok"}`,
		[]TurnToolCall{
			{CallID: "1", Name: "search", Args: map[string]interface{}{"q": "a"}, Result: "found"},
			{CallID: "2", Name: "delete", Args: map[string]interface{}{}, Result: "ok"},
		},
	)
	params["tools"] = []interface{}{"search"}
	v := NewLLMJudgeToolCallsValidator(nil)
	result := v.Validate("", params)
	assert.True(t, result.Passed)
	assert.Equal(t, 1, result.Details.(map[string]interface{})["tool_calls_sent"])
}

func TestLLMJudgeToolCalls_FilterByRoundIndex(t *testing.T) {
	// Build turn messages with two assistant messages to produce different round indices.
	repo := mock.NewInMemoryMockRepository(`{"passed":true,"score":0.8,"reasoning":"ok"}`)
	spec := providers.ProviderSpec{
		ID:               "mock-judge",
		Type:             "mock",
		Model:            "judge-model",
		AdditionalConfig: map[string]interface{}{"repository": repo},
	}
	msgs := []types.Message{
		// Round 0: first assistant with tool call
		{Role: "assistant", ToolCalls: []types.MessageToolCall{
			{ID: "1", Name: "search", Args: json.RawMessage(`{}`)},
		}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "1", Name: "search", Content: "r1"}},
		// Round 1: second assistant with tool call (prevWasToolRole triggers round increment)
		{Role: "assistant", ToolCalls: []types.MessageToolCall{
			{ID: "2", Name: "lookup", Args: json.RawMessage(`{}`)},
		}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "2", Name: "lookup", Content: "r2"}},
	}
	params := map[string]interface{}{
		"criteria":       "evaluate tool usage",
		"_turn_messages": msgs,
		"round_index":    0,
		"_metadata":      map[string]interface{}{"judge_targets": map[string]providers.ProviderSpec{"default": spec}},
	}
	v := NewLLMJudgeToolCallsValidator(nil)
	result := v.Validate("", params)
	assert.True(t, result.Passed)
	assert.Equal(t, 1, result.Details.(map[string]interface{})["tool_calls_sent"])
}

func TestLLMJudgeToolCalls_MinScore(t *testing.T) {
	params := mockJudgeParams(
		`{"passed":true,"score":0.5,"reasoning":"ok"}`,
		[]TurnToolCall{{CallID: "1", Name: "search", Args: map[string]interface{}{}, Result: "ok"}},
	)
	params["min_score"] = 0.8
	v := NewLLMJudgeToolCallsValidator(nil)
	result := v.Validate("", params)
	assert.False(t, result.Passed, "should fail when score below min_score")
}

func TestLLMJudgeToolCalls_ConversationAware(t *testing.T) {
	params := mockJudgeParams(
		`{"passed":true,"score":0.9,"reasoning":"used context well"}`,
		[]TurnToolCall{{CallID: "1", Name: "search", Args: map[string]interface{}{"q": "x"}, Result: "found"}},
	)
	params["conversation_aware"] = true
	params["_execution_context_messages"] = []types.Message{
		{Role: "user", Content: "Find me restaurants"},
		{Role: "assistant", Content: "Let me search for that."},
	}
	v := NewLLMJudgeToolCallsValidator(nil)
	result := v.Validate("", params)
	assert.True(t, result.Passed)
}

func TestLLMJudgeToolCalls_MissingJudgeTargets(t *testing.T) {
	v := NewLLMJudgeToolCallsValidator(nil)
	// Build params with tool trace but no judge targets
	msgs := []types.Message{
		{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "1", Name: "search", Args: json.RawMessage(`{}`)}}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "1", Name: "search", Content: "ok"}},
	}
	result := v.Validate("", map[string]interface{}{
		"criteria":       "test",
		"_turn_messages": msgs,
	})
	assert.False(t, result.Passed)
	assert.Contains(t, result.Details.(map[string]interface{})["error"], "judge_targets")
}

func TestFormatToolCallsForJudge(t *testing.T) {
	calls := []TurnToolCall{
		{
			Name:       "search",
			Args:       map[string]interface{}{"query": "test"},
			Result:     "found it",
			Error:      "",
			RoundIndex: 0,
		},
		{
			Name:       "book",
			Args:       map[string]interface{}{"id": "123"},
			Result:     "",
			Error:      "not available",
			RoundIndex: 1,
		},
	}

	text := formatToolCallsForJudge(calls)

	assert.Contains(t, text, "TOOL CALL 1 (round 0):")
	assert.Contains(t, text, "Tool: search")
	assert.Contains(t, text, `"query":"test"`)
	assert.Contains(t, text, "Result: found it")
	assert.Contains(t, text, "Error: (none)")

	assert.Contains(t, text, "TOOL CALL 2 (round 1):")
	assert.Contains(t, text, "Tool: book")
	assert.Contains(t, text, "Result: (none)")
	assert.Contains(t, text, "Error: not available")
}

func TestFilterToolCalls_NoFilters(t *testing.T) {
	trace := []TurnToolCall{
		{Name: "a", RoundIndex: 0},
		{Name: "b", RoundIndex: 1},
	}
	filtered := filterToolCalls(trace, map[string]interface{}{})
	require.Len(t, filtered, 2)
}

func TestFilterToolCalls_ByTools(t *testing.T) {
	trace := []TurnToolCall{
		{Name: "search", RoundIndex: 0},
		{Name: "delete", RoundIndex: 0},
		{Name: "lookup", RoundIndex: 1},
	}
	filtered := filterToolCalls(trace, map[string]interface{}{
		"tools": []interface{}{"search", "lookup"},
	})
	require.Len(t, filtered, 2)
	assert.Equal(t, "search", filtered[0].Name)
	assert.Equal(t, "lookup", filtered[1].Name)
}

func TestFilterToolCalls_ByRoundIndex(t *testing.T) {
	trace := []TurnToolCall{
		{Name: "a", RoundIndex: 0},
		{Name: "b", RoundIndex: 1},
		{Name: "c", RoundIndex: 1},
	}
	filtered := filterToolCalls(trace, map[string]interface{}{
		"round_index": 1,
	})
	require.Len(t, filtered, 2)
	assert.Equal(t, "b", filtered[0].Name)
	assert.Equal(t, "c", filtered[1].Name)
}

func TestFilterToolCalls_Combined(t *testing.T) {
	trace := []TurnToolCall{
		{Name: "search", RoundIndex: 0},
		{Name: "search", RoundIndex: 1},
		{Name: "delete", RoundIndex: 0},
	}
	filtered := filterToolCalls(trace, map[string]interface{}{
		"tools":       []interface{}{"search"},
		"round_index": 0,
	})
	require.Len(t, filtered, 1)
	assert.Equal(t, "search", filtered[0].Name)
	assert.Equal(t, 0, filtered[0].RoundIndex)
}
