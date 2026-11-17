package claude

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

var noCache = providers.ProviderDefaults{DisablePromptCaching: true}

func plainMessages(t *testing.T, defaults providers.ProviderDefaults, msgs []types.Message,
	setup func(p *ToolProvider)) string {
	t.Helper()
	return string(captureMessages(t, false, defaults, func(p *ToolProvider) {
		if setup != nil {
			setup(p)
		}
		_, err := p.Predict(context.Background(), providers.PredictionRequest{Messages: msgs})
		require.NoError(t, err)
	}))
}

func toolPathMessages(t *testing.T, defaults providers.ProviderDefaults, msgs []types.Message,
	setup func(p *ToolProvider)) string {
	t.Helper()
	return string(captureMessages(t, false, defaults, func(p *ToolProvider) {
		if setup != nil {
			setup(p)
		}
		tools, err := p.BuildTooling([]*providers.ToolDescriptor{{
			Name: "f", Description: "f", InputSchema: json.RawMessage(`{"type":"object"}`),
		}})
		require.NoError(t, err)
		_, _, err = p.PredictWithTools(context.Background(), providers.PredictionRequest{Messages: msgs}, tools, "auto")
		require.NoError(t, err)
	}))
}

// A tool call with no result right after it (held for approval, or a turn cut
// short) is not sent as tool_use, which the API refuses without a tool_result:
// the assistant's text goes out alone, as before tool linkage. Answered calls
// keep their linkage, on both paths.
func TestUnpairedToolCallsAreNotSentAsToolUse(t *testing.T) {
	msgs := []types.Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", Content: "Checking.", ToolCalls: []types.MessageToolCall{
			{ID: "a", Name: "f", Args: json.RawMessage(`{}`)},
			{ID: "held", Name: "f", Args: json.RawMessage(`{}`)},
		}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "a", Name: "f", Parts: []types.ContentPart{types.NewTextPart("1")}}},
		{Role: "user", Content: "and?"},
		{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "pending", Name: "f", Args: json.RawMessage(`{}`)}}},
	}
	want := `[
		{"role":"user","content":[{"type":"text","text":"q"}]},
		{"role":"assistant","content":[{"type":"text","text":"Checking."},
			{"type":"tool_use","id":"a","name":"f","input":{}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"a","content":"1"},{"type":"text","text":"and?"}]}
	]`
	assert.JSONEq(t, want, plainMessages(t, noCache, msgs, nil), "plain path")
	assert.JSONEq(t, want, toolPathMessages(t, noCache, msgs, nil), "tool path")
}

// A tool result that answers no call in the assistant message before it is
// sent as the user's text, since a tool_result naming an unknown tool_use is
// refused.
func TestOrphanToolResultIsSentAsText(t *testing.T) {
	msgs := []types.Message{
		{Role: "user", Content: "q"},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "gone", Name: "f", Parts: []types.ContentPart{types.NewTextPart("42")}}},
	}
	assert.JSONEq(t, `[
		{"role":"user","content":[{"type":"text","text":"q"}]},
		{"role":"user","content":[{"type":"text","text":"42"}]}
	]`, plainMessages(t, noCache, msgs, nil))
}

// A role=tool message without a ToolResult is sent as the user's text instead
// of being dereferenced.
func TestToolMessageWithoutResultIsText(t *testing.T) {
	msgs := []types.Message{
		{Role: "user", Content: "q"},
		{Role: "tool", Content: "stray output"},
	}
	want := `[
		{"role":"user","content":[{"type":"text","text":"q"}]},
		{"role":"user","content":[{"type":"text","text":"stray output"}]}
	]`
	assert.JSONEq(t, want, plainMessages(t, noCache, msgs, nil), "plain path")
	assert.JSONEq(t, want, toolPathMessages(t, noCache, msgs, nil), "tool path")
}

func thinkingHistory() []types.Message {
	return []types.Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", Reasoning: &types.ReasoningTrace{Text: "need the tool", Opaque: []types.OpaqueReasoning{
			{Provider: "claude", Kind: "thinking_signature", Data: "sig-1"},
			{Provider: "claude", Kind: "redacted_thinking", Data: "blob-1"},
		}}, ToolCalls: []types.MessageToolCall{{ID: "a", Name: "f", Args: json.RawMessage(`{}`)}}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "a", Name: "f", Parts: []types.ContentPart{types.NewTextPart("1")}}},
	}
}

// With extended thinking on, an assistant tool round is replayed with its
// thinking and redacted_thinking blocks, signatures intact, ahead of its
// tool_use, as the API requires; on both paths.
func TestThinkingBlocksAreReplayedBeforeToolUse(t *testing.T) {
	budget := 2048
	on := func(p *ToolProvider) { p.thinkingBudget = &budget }
	want := `[
		{"role":"user","content":[{"type":"text","text":"q"}]},
		{"role":"assistant","content":[
			{"type":"thinking","thinking":"need the tool","signature":"sig-1"},
			{"type":"redacted_thinking","data":"blob-1"},
			{"type":"tool_use","id":"a","name":"f","input":{}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"a","content":"1"}]}
	]`
	assert.JSONEq(t, want, plainMessages(t, noCache, thinkingHistory(), on), "plain path")
	assert.JSONEq(t, want, toolPathMessages(t, noCache, thinkingHistory(), on), "tool path")
}

// Without extended thinking the reasoning is not replayed: the API takes
// thinking blocks only when thinking is enabled.
func TestThinkingBlocksAreNotReplayedWithThinkingOff(t *testing.T) {
	assert.JSONEq(t, `[
		{"role":"user","content":[{"type":"text","text":"q"}]},
		{"role":"assistant","content":[{"type":"tool_use","id":"a","name":"f","input":{}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"a","content":"1"}]}
	]`, plainMessages(t, noCache, thinkingHistory(), nil))
}

// A plain request whose history ends in a tool result caches the prefix
// through that result.
func TestPlainPath_HistoryEndingInToolResultGetsBreakpoint(t *testing.T) {
	assert.JSONEq(t, `[
		{"role":"user","content":[{"type":"text","text":"q"}]},
		{"role":"assistant","content":[{"type":"tool_use","id":"a","name":"f","input":{}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"a","content":"1",
			"cache_control":{"type":"ephemeral"}}]}
	]`, plainMessages(t, providers.ProviderDefaults{}, thinkingHistory(), nil))
}

// The run of results that answers an assistant's calls ends at the first tool
// message that answers none of them; later results are text, and the calls
// they would have answered are left out.
func TestAnsweredCallsEndAtTheFirstForeignResult(t *testing.T) {
	msgs := []types.Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "a", Name: "f", Args: json.RawMessage(`{}`)}}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "x", Name: "f", Parts: []types.ContentPart{types.NewTextPart("foreign")}}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "a", Name: "f", Parts: []types.ContentPart{types.NewTextPart("1")}}},
	}
	assert.JSONEq(t, `[
		{"role":"user","content":[{"type":"text","text":"q"}]},
		{"role":"user","content":[{"type":"text","text":"foreign"}]},
		{"role":"user","content":[{"type":"text","text":"1"}]}
	]`, plainMessages(t, noCache, msgs, nil))
}

// Only Claude's own, non-empty reasoning entries are replayed, and only the
// first thinking signature, which is the one the trace's text belongs to.
func TestThinkingBlocks_ReplaysOnlyWhatMatchesTheText(t *testing.T) {
	got := thinkingBlocks(&types.ReasoningTrace{Text: "t", Opaque: []types.OpaqueReasoning{
		{Provider: "gemini", Kind: "thinking_signature", Data: "foreign"},
		{Provider: "claude", Kind: "thinking_signature", Data: ""},
		{Provider: "claude", Kind: "redacted_thinking", Data: "r1"},
		{Provider: "claude", Kind: "thinking_signature", Data: "s1"},
		{Provider: "claude", Kind: "thinking_signature", Data: "s2"},
		{Provider: "claude", Kind: "other", Data: "o"},
	}})
	assert.Equal(t, []any{
		claudeRedactedThinkingBlock{Type: "redacted_thinking", Data: "r1"},
		claudeThinkingBlock{Type: "thinking", Thinking: "t", Signature: "s1"},
	}, got)
	assert.Nil(t, thinkingBlocks(nil))
}

// Only a role=tool message answers a call: a user message that happens to
// carry a ToolResult does not, so the call it names is left out.
func TestAnsweredCalls_OnlyToolMessagesAnswer(t *testing.T) {
	msgs := []types.Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", Content: "Checking.", ToolCalls: []types.MessageToolCall{{ID: "a", Name: "f", Args: json.RawMessage(`{}`)}}},
		{Role: "user", Content: "hi", ToolResult: &types.MessageToolResult{ID: "a", Name: "f"}},
	}
	assert.JSONEq(t, `[
		{"role":"user","content":[{"type":"text","text":"q"}]},
		{"role":"assistant","content":[{"type":"text","text":"Checking."}]},
		{"role":"user","content":[{"type":"text","text":"hi"}]}
	]`, plainMessages(t, noCache, msgs, nil))
}
