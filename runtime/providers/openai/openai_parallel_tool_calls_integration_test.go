package openai

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestParallelToolCalls_Live proves the additional_config key actually changes
// model behavior end-to-end through the provider, not just the request map.
//
// The prompt asks for two refunds, which gpt-4o-mini answers with two parallel
// tool calls by default. With parallel_tool_calls:false it must answer with
// exactly one.
//
// Skips without OPENAI_API_KEY; hits the paid API when it runs.
func TestParallelToolCalls_Live(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("no OPENAI_API_KEY, skipping live parallel_tool_calls test")
	}

	descriptors := []*providers.ToolDescriptor{{
		Name:        "approve_refund",
		Description: "Approve a customer refund request.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"order_id": {"type": "string"},
				"amount": {"type": "number"}
			},
			"required": ["order_id", "amount"]
		}`),
	}}

	req := providers.PredictionRequest{
		Messages: []types.Message{{
			Role: "user",
			Content: "Refund both of my orders: order A-1 is $30 and order B-2 is $45. " +
				"Approve each one.",
		}},
		MaxTokens: 200,
	}

	callsFor := func(t *testing.T, cfg map[string]any) int {
		t.Helper()
		p := NewToolProvider(
			"openai", "gpt-4o-mini", "https://api.openai.com/v1",
			providers.ProviderDefaults{MaxTokens: 200}, false, cfg, nil,
		)
		p.apiMode = APIModeCompletions
		tooling, err := p.BuildTooling(descriptors)
		require.NoError(t, err)
		_, toolCalls, err := p.PredictWithTools(context.Background(), req, tooling, toolChoiceAuto)
		require.NoError(t, err)
		return len(toolCalls)
	}

	// Control: establish that this prompt really does provoke parallel calls.
	// Without it the assertion below could pass vacuously on a model that only
	// ever emits one call.
	baseline := callsFor(t, nil)
	t.Logf("default (parallel_tool_calls omitted): %d tool call(s)", baseline)
	if baseline < 2 {
		t.Skipf("control did not reproduce parallel calling (got %d call(s)); "+
			"nothing to suppress, so the assertion would be vacuous", baseline)
	}

	suppressed := callsFor(t, map[string]any{"parallel_tool_calls": false})
	t.Logf("parallel_tool_calls=false: %d tool call(s)", suppressed)
	require.Equal(t, 1, suppressed,
		"parallel_tool_calls=false must reduce the turn to a single tool call")
}

// TestParallelToolCalls_NoToolsLive guards the interaction with #1735. Since
// that fix, a turn whose tool set was emptied still routes through the
// tool-aware path, reaching buildToolRequest with tools == nil. Emitting
// parallel_tool_calls there would 400 with "only allowed when 'tools' are
// specified" — reintroducing a crash on exactly the path #1735 repaired.
//
// Skips without OPENAI_API_KEY; hits the paid API when it runs.
func TestParallelToolCalls_NoToolsLive(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("no OPENAI_API_KEY, skipping live no-tools guard test")
	}

	// includeRawOutput exposes the built request on the response, so this can
	// assert what actually went on the wire rather than only that nothing blew up.
	p := NewToolProvider(
		"openai", "gpt-4o-mini", "https://api.openai.com/v1",
		providers.ProviderDefaults{MaxTokens: 64}, true,
		map[string]any{"parallel_tool_calls": false}, nil,
	)
	p.apiMode = APIModeCompletions

	denial := types.NewTextToolResult("call_A", "approve_refund", "denied by policy")
	denial.Error = "denied by policy"
	req := providers.PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "approve the refund"},
			{Role: "assistant", ToolCalls: []types.MessageToolCall{
				{ID: "call_A", Name: "approve_refund", Args: json.RawMessage(`{}`)},
			}},
			types.NewToolResultMessage(denial),
		},
		MaxTokens: 64,
	}

	// nil tooling: the emptied-tool-set case from #1735.
	resp, _, err := p.PredictWithTools(context.Background(), req, nil, "")
	require.NoError(t, err,
		"configured parallel_tool_calls must not leak into a request with no tools")

	// Assert on the request we actually sent, not merely on the absence of an
	// error: the key must be gone, and "tools" with it. Hoisting the assignment
	// out of the tools guard fails this here and 400s at the API.
	sent, ok := resp.RawRequest.(map[string]interface{})
	require.True(t, ok, "raw request should be captured for inspection")
	require.NotContains(t, sent, "parallel_tool_calls",
		"must be omitted when no tools are declared — OpenAI rejects the pair")
	require.NotContains(t, sent, "tools",
		"sanity: this is genuinely the no-tools case the guard protects")

	// And the round trip produced a real answer, so the request was well formed
	// rather than merely accepted.
	require.NotEmpty(t, resp.Content,
		"model should have replied in text after seeing the denied tool result")
}
