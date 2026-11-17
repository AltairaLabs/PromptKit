//go:build integration

package claude

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// TestPredict_PlainPathToolLinkage_Live proves the Messages API accepts a
// request without tools whose history holds a tool_use and its tool_result,
// and answers from the result.
func TestPredict_PlainPathToolLinkage_Live(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" && os.Getenv("CLAUDE_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
	p := NewProvider("live", "claude-haiku-4-5", "https://api.anthropic.com/v1",
		providers.ProviderDefaults{MaxTokens: 100}, false)
	hist := linkageHistory()
	hist[2].ToolResult.Parts = []types.ContentPart{types.NewTextPart("21.7C and overcast.")}
	hist[3].Content = "What temperature did the tool report?"
	resp, err := p.Predict(context.Background(), providers.PredictionRequest{Messages: hist})
	require.NoError(t, err)
	assert.True(t, strings.Contains(resp.Content, "21") || strings.Contains(resp.Content, "22"),
		"answer does not reflect the tool result: %q", resp.Content)
}

// TestThinkingReplay_Live runs a tool round with extended thinking on, then
// replays it, thinking blocks first, on the tool path and the plain path. The
// API refuses a tool-using assistant turn whose thinking is missing or altered.
func TestThinkingReplay_Live(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" && os.Getenv("CLAUDE_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
	budget := 1024
	p := NewToolProvider("live", "claude-sonnet-4-5", "https://api.anthropic.com/v1",
		providers.ProviderDefaults{MaxTokens: 2048}, false)
	p.thinkingBudget = &budget
	tools, err := p.BuildTooling([]*providers.ToolDescriptor{{
		Name: "get_weather", Description: "Current weather for a city",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
	}})
	require.NoError(t, err)
	req := providers.PredictionRequest{Messages: []types.Message{
		{Role: "user", Content: "Use the tool: what is the weather in Paris?"}}}
	resp, calls, err := p.PredictWithTools(context.Background(), req, tools, "auto")
	require.NoError(t, err)
	require.NotEmpty(t, calls, "model did not call the tool")
	require.NotNil(t, resp.Reasoning, "no thinking captured, so nothing to replay")
	require.NotEmpty(t, resp.Reasoning.Opaque, "no thinking signature captured")
	req.Messages = append(req.Messages,
		types.Message{Role: "assistant", Content: resp.Content, Reasoning: resp.Reasoning, ToolCalls: calls})
	for _, c := range calls {
		req.Messages = append(req.Messages, types.NewToolResultMessage(
			types.NewTextToolResult(c.ID, c.Name, "21.7C and overcast.")))
	}

	toolResp, _, err := p.PredictWithTools(context.Background(), req, tools, "auto")
	require.NoError(t, err, "tool path refused the replayed thinking")
	assert.Contains(t, toolResp.Content, "21")
	plainResp, err := p.Predict(context.Background(), req)
	require.NoError(t, err, "plain path refused the replayed thinking")
	assert.Contains(t, plainResp.Content, "21")
}
