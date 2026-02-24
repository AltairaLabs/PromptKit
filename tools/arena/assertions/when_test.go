package assertions

import (
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
)

func buildWhenParams(toolCalls ...TurnToolCall) map[string]interface{} {
	var msgs []types.Message
	if len(toolCalls) > 0 {
		// Build assistant message with tool calls
		var mtcs []types.MessageToolCall
		for _, tc := range toolCalls {
			rawArgs, _ := json.Marshal(tc.Args)
			mtcs = append(mtcs, types.MessageToolCall{
				ID:   tc.CallID,
				Name: tc.Name,
				Args: rawArgs,
			})
		}
		msgs = append(msgs, types.Message{Role: "assistant", ToolCalls: mtcs})
		// Build tool result messages
		for _, tc := range toolCalls {
			msgs = append(msgs, types.Message{
				Role: "tool",
				ToolResult: &types.MessageToolResult{
					ID:      tc.CallID,
					Name:    tc.Name,
					Content: tc.Result,
					Error:   tc.Error,
				},
			})
		}
	} else {
		// Empty assistant message (no tool calls)
		msgs = append(msgs, types.Message{Role: "assistant", Content: "Hello"})
	}
	return map[string]interface{}{
		"_turn_messages": msgs,
	}
}

func TestAssertionWhen_ToolCalled_Match(t *testing.T) {
	w := &AssertionWhen{ToolCalled: "search"}
	params := buildWhenParams(TurnToolCall{CallID: "1", Name: "search", Result: "ok"})
	shouldRun, reason := w.ShouldRun(params)
	assert.True(t, shouldRun)
	assert.Empty(t, reason)
}

func TestAssertionWhen_ToolCalled_NoMatch(t *testing.T) {
	w := &AssertionWhen{ToolCalled: "search"}
	params := buildWhenParams(TurnToolCall{CallID: "1", Name: "lookup", Result: "ok"})
	shouldRun, reason := w.ShouldRun(params)
	assert.False(t, shouldRun)
	assert.Contains(t, reason, `"search"`)
}

func TestAssertionWhen_ToolCalledPattern_Match(t *testing.T) {
	w := &AssertionWhen{ToolCalledPattern: "^(search|lookup)$"}
	params := buildWhenParams(TurnToolCall{CallID: "1", Name: "lookup", Result: "ok"})
	shouldRun, reason := w.ShouldRun(params)
	assert.True(t, shouldRun)
	assert.Empty(t, reason)
}

func TestAssertionWhen_ToolCalledPattern_NoMatch(t *testing.T) {
	w := &AssertionWhen{ToolCalledPattern: "^(search|lookup)$"}
	params := buildWhenParams(TurnToolCall{CallID: "1", Name: "delete", Result: "ok"})
	shouldRun, reason := w.ShouldRun(params)
	assert.False(t, shouldRun)
	assert.Contains(t, reason, "no tool matching pattern")
}

func TestAssertionWhen_ToolCalledPattern_InvalidRegex(t *testing.T) {
	w := &AssertionWhen{ToolCalledPattern: "[invalid"}
	params := buildWhenParams(TurnToolCall{CallID: "1", Name: "search", Result: "ok"})
	shouldRun, reason := w.ShouldRun(params)
	assert.False(t, shouldRun)
	assert.Contains(t, reason, "invalid tool_called_pattern")
}

func TestAssertionWhen_AnyToolCalled_WithTools(t *testing.T) {
	w := &AssertionWhen{AnyToolCalled: true}
	params := buildWhenParams(TurnToolCall{CallID: "1", Name: "search", Result: "ok"})
	shouldRun, reason := w.ShouldRun(params)
	assert.True(t, shouldRun)
	assert.Empty(t, reason)
}

func TestAssertionWhen_AnyToolCalled_NoTools(t *testing.T) {
	w := &AssertionWhen{AnyToolCalled: true}
	params := buildWhenParams() // no tool calls
	shouldRun, reason := w.ShouldRun(params)
	assert.False(t, shouldRun)
	assert.Contains(t, reason, "no tool calls")
}

func TestAssertionWhen_MinToolCalls_Met(t *testing.T) {
	w := &AssertionWhen{MinToolCalls: 2}
	params := buildWhenParams(
		TurnToolCall{CallID: "1", Name: "search", Result: "ok"},
		TurnToolCall{CallID: "2", Name: "lookup", Result: "ok"},
	)
	shouldRun, reason := w.ShouldRun(params)
	assert.True(t, shouldRun)
	assert.Empty(t, reason)
}

func TestAssertionWhen_MinToolCalls_NotMet(t *testing.T) {
	w := &AssertionWhen{MinToolCalls: 3}
	params := buildWhenParams(TurnToolCall{CallID: "1", Name: "search", Result: "ok"})
	shouldRun, reason := w.ShouldRun(params)
	assert.False(t, shouldRun)
	assert.Contains(t, reason, "only 1 tool call(s), need 3")
}

func TestAssertionWhen_MultipleCombined_AllMet(t *testing.T) {
	w := &AssertionWhen{
		ToolCalled:    "search",
		AnyToolCalled: true,
		MinToolCalls:  1,
	}
	params := buildWhenParams(TurnToolCall{CallID: "1", Name: "search", Result: "ok"})
	shouldRun, reason := w.ShouldRun(params)
	assert.True(t, shouldRun)
	assert.Empty(t, reason)
}

func TestAssertionWhen_MultipleCombined_OneFails(t *testing.T) {
	w := &AssertionWhen{
		ToolCalled:   "search",
		MinToolCalls: 3,
	}
	params := buildWhenParams(TurnToolCall{CallID: "1", Name: "search", Result: "ok"})
	shouldRun, reason := w.ShouldRun(params)
	assert.False(t, shouldRun)
	assert.Contains(t, reason, "need 3")
}

func TestAssertionWhen_NoTraceAvailable(t *testing.T) {
	w := &AssertionWhen{ToolCalled: "search"}
	params := map[string]interface{}{} // no _turn_messages
	shouldRun, reason := w.ShouldRun(params)
	assert.True(t, shouldRun, "should return true when trace not available")
	assert.Empty(t, reason)
}
