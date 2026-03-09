package evals

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildEvalContext_ExtractsCurrentOutput(t *testing.T) {
	messages := []types.Message{
		types.NewUserMessage("hello"),
		types.NewAssistantMessage("first"),
		types.NewUserMessage("followup"),
		types.NewAssistantMessage("second"),
	}

	ctx := BuildEvalContext(messages, 3, "sess-1", "chat", nil)

	assert.Equal(t, "second", ctx.CurrentOutput)
	assert.Equal(t, 3, ctx.TurnIndex)
	assert.Equal(t, "sess-1", ctx.SessionID)
	assert.Equal(t, "chat", ctx.PromptID)
	assert.Len(t, ctx.Messages, 4)
}

func TestBuildEvalContext_NoMessages(t *testing.T) {
	ctx := BuildEvalContext(nil, 0, "sess-1", "chat", nil)

	assert.Empty(t, ctx.CurrentOutput)
	assert.Empty(t, ctx.ToolCalls)
	assert.Nil(t, ctx.Extras)
}

func TestBuildEvalContext_WithMetadata(t *testing.T) {
	metadata := map[string]any{"judge_provider": "mock"}
	ctx := BuildEvalContext(nil, 0, "s1", "p1", metadata)

	assert.Equal(t, "mock", ctx.Metadata["judge_provider"])
}

func TestExtractToolCalls_MatchesResults(t *testing.T) {
	messages := []types.Message{
		types.NewUserMessage("search"),
		{
			Role:    "assistant",
			Content: "searching...",
			ToolCalls: []types.MessageToolCall{
				{ID: "tc-1", Name: "search", Args: []byte(`{"q":"cats"}`)},
			},
		},
		{
			Role:    "tool",
			Content: "found 3",
			ToolResult: &types.MessageToolResult{
				ID: "tc-1",
			},
		},
	}

	calls := ExtractToolCalls(messages)
	require.Len(t, calls, 1)
	assert.Equal(t, "search", calls[0].ToolName)
	assert.Equal(t, 1, calls[0].TurnIndex)
	assert.Equal(t, "cats", calls[0].Arguments["q"])
	assert.Equal(t, "found 3", calls[0].Result)
}

func TestExtractToolCalls_WithError(t *testing.T) {
	messages := []types.Message{
		{
			Role: "assistant",
			ToolCalls: []types.MessageToolCall{
				{ID: "tc-1", Name: "fail"},
			},
		},
		{
			Role:       "tool",
			ToolResult: &types.MessageToolResult{ID: "tc-1", Error: "boom"},
		},
	}

	calls := ExtractToolCalls(messages)
	require.Len(t, calls, 1)
	assert.Equal(t, "boom", calls[0].Error)
}

func TestExtractToolCalls_NoResult(t *testing.T) {
	messages := []types.Message{
		{
			Role: "assistant",
			ToolCalls: []types.MessageToolCall{
				{ID: "tc-1", Name: "search"},
			},
		},
	}

	calls := ExtractToolCalls(messages)
	require.Len(t, calls, 1)
	assert.Nil(t, calls[0].Result)
}

func TestExtractToolCalls_MultipartResult(t *testing.T) {
	txt := "generated image"
	messages := []types.Message{
		{
			Role: "assistant",
			ToolCalls: []types.MessageToolCall{
				{ID: "tc-1", Name: "image_gen"},
			},
		},
		{
			Role: "tool",
			ToolResult: &types.MessageToolResult{
				ID: "tc-1",
				Parts: []types.ContentPart{
					{Type: "text", Text: &txt},
				},
			},
		},
	}

	calls := ExtractToolCalls(messages)
	require.Len(t, calls, 1)
	parts, ok := calls[0].Result.([]types.ContentPart)
	require.True(t, ok)
	assert.Equal(t, &txt, parts[0].Text)
}

func TestExtractToolCalls_Empty(t *testing.T) {
	calls := ExtractToolCalls(nil)
	assert.Empty(t, calls)

	calls = ExtractToolCalls([]types.Message{types.NewUserMessage("hi")})
	assert.Empty(t, calls)
}

func TestExtractWorkflowExtras_AllFields(t *testing.T) {
	messages := []types.Message{
		{
			Role: "assistant",
			Meta: map[string]any{
				"_workflow_state":       "greeting",
				"_workflow_transitions": []string{"init", "greeting"},
				"_workflow_complete":    true,
			},
		},
	}

	extras := ExtractWorkflowExtras(messages)
	require.NotNil(t, extras)
	assert.Equal(t, "greeting", extras["workflow_state"])
	assert.Equal(t, true, extras["workflow_complete"])
}

func TestExtractWorkflowExtras_NoWorkflow(t *testing.T) {
	messages := []types.Message{
		{Role: "assistant", Meta: map[string]any{"other": "data"}},
	}
	assert.Nil(t, ExtractWorkflowExtras(messages))
}

func TestExtractWorkflowExtras_NilMeta(t *testing.T) {
	messages := []types.Message{{Role: "assistant"}}
	assert.Nil(t, ExtractWorkflowExtras(messages))
}

func TestExtractWorkflowExtras_Empty(t *testing.T) {
	assert.Nil(t, ExtractWorkflowExtras(nil))
}

func TestParseJSONArgs_Valid(t *testing.T) {
	result := parseJSONArgs([]byte(`{"key":"value","num":42}`))
	assert.Equal(t, "value", result["key"])
	assert.Equal(t, float64(42), result["num"])
}

func TestParseJSONArgs_Invalid(t *testing.T) {
	assert.Nil(t, parseJSONArgs([]byte(`not json`)))
}

func TestParseJSONArgs_Empty(t *testing.T) {
	result := parseJSONArgs([]byte(`{}`))
	assert.NotNil(t, result)
	assert.Empty(t, result)
}
