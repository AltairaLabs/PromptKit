package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

const finalText = "You are in the city your device reported."

// locationScript is a mock model that calls get_location first, then answers
// in text once the tool's result is in the history.
type locationScript struct{}

func (s locationScript) GetResponse(ctx context.Context, p mock.ResponseParams) (string, error) {
	turn, err := s.GetTurn(ctx, p)
	return turn.Content, err
}

func (locationScript) GetTurn(_ context.Context, p mock.ResponseParams) (*mock.Turn, error) {
	if p.TurnNumber <= 1 {
		return &mock.Turn{Type: "tool_calls", ToolCalls: []mock.ToolCall{
			{Name: "get_location", Arguments: map[string]any{"accuracy": "high"}},
		}}, nil
	}
	return &mock.Turn{Type: "text", Content: finalText}, nil
}

func openConv(t *testing.T) *sdk.Conversation {
	t.Helper()
	provider := mock.NewToolProviderWithRepository("mock", "mock-model", false, locationScript{})
	conv, err := sdk.Open("client-tools.pack.json", "assistant",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	return conv
}

func TestSmoke_Sync(t *testing.T) {
	conv := openConv(t)
	var calls []sdk.ClientToolRequest
	conv.OnClientTool("get_location", func(_ context.Context, req sdk.ClientToolRequest) (any, error) {
		calls = append(calls, req)
		return map[string]string{"lat": "37.7749", "lng": "-122.4194"}, nil
	})

	resp, err := conv.Send(context.Background(), "Where am I?")
	require.NoError(t, err)

	require.Len(t, calls, 1, "the model's get_location call must reach the client handler")
	assert.Equal(t, "high", calls[0].Args["accuracy"])
	assert.False(t, resp.HasPendingClientTools())
	assert.Equal(t, finalText, resp.Text())
}

func TestSmoke_Deferred(t *testing.T) {
	conv := openConv(t)
	conv.DeferClientTool("get_location")
	ctx := context.Background()

	resp, err := conv.Send(ctx, "Where am I?")
	require.NoError(t, err)
	require.True(t, resp.HasPendingClientTools(), "the turn must suspend on the client tool")
	pending := resp.ClientTools()
	require.Len(t, pending, 1)
	assert.Equal(t, "get_location", pending[0].ToolName)

	require.NoError(t, conv.SendToolResult(ctx, pending[0].CallID, map[string]any{"city": "San Francisco"}))
	resp, err = conv.Resume(ctx)
	require.NoError(t, err)
	assert.Equal(t, finalText, resp.Text())
}

func TestSmoke_StreamDeferred(t *testing.T) {
	conv := openConv(t)
	conv.DeferClientTool("get_location")
	ctx := context.Background()

	var pending *sdk.PendingClientTool
	for chunk := range conv.Stream(ctx, "Where am I?") {
		require.NoError(t, chunk.Error)
		if chunk.Type == sdk.ChunkClientTool {
			pending = chunk.ClientTool
		}
	}
	require.NotNil(t, pending, "the stream must surface the client tool call")

	require.NoError(t, conv.SendToolResult(ctx, pending.CallID, map[string]any{"city": "London"}))
	var text string
	for chunk := range conv.ResumeStream(ctx) {
		require.NoError(t, chunk.Error)
		if chunk.Type == sdk.ChunkText {
			text += chunk.Text
		}
	}
	assert.Equal(t, finalText, text)
}
