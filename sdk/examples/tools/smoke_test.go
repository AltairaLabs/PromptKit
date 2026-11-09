package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// scriptedTurns answers turn N with turns[N]: the model asks for get_time on
// turn 1 and answers in text once the tool result is back.
type scriptedTurns map[int]*mock.Turn

func (s scriptedTurns) GetResponse(ctx context.Context, p mock.ResponseParams) (string, error) {
	turn, err := s.GetTurn(ctx, p)
	return turn.Content, err
}

func (s scriptedTurns) GetTurn(_ context.Context, p mock.ResponseParams) (*mock.Turn, error) {
	if turn, ok := s[p.TurnNumber]; ok {
		return turn, nil
	}
	return &mock.Turn{Type: "text", Content: "unscripted turn"}, nil
}

// TestSmoke drives the example's tool loop: the model calls get_time, the
// OnTool handler receives the model's arguments, and its result goes back to
// the model as the tool message before the final answer.
func TestSmoke(t *testing.T) {
	provider := mock.NewToolProviderWithRepository("mock", "mock-model", false, scriptedTurns{
		1: {Type: "tool_calls", ToolCalls: []mock.ToolCall{{
			Name: "get_time", Arguments: map[string]any{"timezone": "Europe/London"},
		}}},
		2: {Type: "text", Content: "It is 12:00 in London."},
	})
	conv, err := sdk.Open("tools.pack.json", "assistant",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	defer conv.Close()

	var gotTimezone any
	conv.OnTool("get_time", func(args map[string]any) (any, error) {
		gotTimezone = args["timezone"]
		return map[string]string{"time": "12:00"}, nil
	})
	conv.OnTool("get_weather", func(args map[string]any) (any, error) {
		return map[string]string{"weather": "sunny"}, nil
	})

	resp, err := conv.Send(context.Background(), "What time is it?")
	require.NoError(t, err)
	require.Equal(t, "It is 12:00 in London.", resp.Text())
	require.Equal(t, "Europe/London", gotTimezone)

	var toolResults []string
	for _, m := range conv.Messages(context.Background()) {
		if m.Role == "tool" && m.ToolResult != nil {
			require.Equal(t, "get_time", m.ToolResult.Name)
			toolResults = append(toolResults, m.ToolResult.GetTextContent())
		}
	}
	require.Len(t, toolResults, 1, "one tool result goes back to the model")
	require.JSONEq(t, `{"time":"12:00"}`, toolResults[0])
}
