package sdk

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// A held call counts against ToolPolicy.MaxCallsPerMinute when a human approves
// it, in the conversation's window, and a rejected one never counts. The probe
// stage shares the conversation's window, as its pipelines' stages do, with a
// limit of 2: one approved call leaves exactly one slot.
func TestResolveTool_AdmitsApprovedCallIntoCallRate(t *testing.T) {
	repo := newGateTurnRepo()
	call := mock.ToolCall{Name: "send_message", Arguments: map[string]any{"body": "hi"}}
	repo.add(1, mock.Turn{Type: "tool_calls", ToolCalls: []mock.ToolCall{call}})
	repo.add(2, mock.Turn{Type: "tool_calls", ToolCalls: []mock.ToolCall{call}})
	provider := mock.NewToolProviderWithRepository("mock", "mock-model", false, repo)

	conv, err := Open(writeGatePack(t), "assist", WithProvider(provider), WithSkipSchemaValidation())
	require.NoError(t, err)
	defer func() { _ = conv.Close() }()
	conv.OnToolAsync("send_message",
		func(map[string]any) sdktools.PendingResult {
			return sdktools.PendingResult{Reason: "requires_approval"}
		},
		func(args map[string]any) (any, error) { return args, nil },
	)

	ctx := context.Background()
	resp, err := conv.Send(ctx, "send one")
	require.NoError(t, err)
	require.Len(t, resp.PendingTools(), 1)
	_, err = conv.RejectTool(ctx, resp.PendingTools()[0].ID, "no")
	require.NoError(t, err)

	resp, err = conv.Send(ctx, "send another")
	require.NoError(t, err)
	require.Len(t, resp.PendingTools(), 1)
	_, err = conv.ResolveTool(ctx, resp.PendingTools()[0].ID)
	require.NoError(t, err)

	require.NotNil(t, conv.callRate, "the conversation's pipelines share its window")
	assert.Equal(t, 1, probeRefusals(t, conv, 2), "the approved call takes one slot; the rejected one takes none")
}

// probeRefusals runs one turn of a provider stage that shares conv's call-rate
// window under a limit of limit calls per minute, in which the model calls
// send_message limit times, and returns how many of those calls were refused.
func probeRefusals(t *testing.T, conv *Conversation, limit int) int {
	t.Helper()
	repo := newGateTurnRepo()
	calls := make([]mock.ToolCall, limit)
	for i := range calls {
		calls[i] = mock.ToolCall{Name: "send_message", Arguments: map[string]any{"n": i}}
	}
	repo.add(1, mock.Turn{Type: "tool_calls", ToolCalls: calls})
	repo.add(2, mock.Turn{Type: "text", Content: "done"})

	registry := tools.NewRegistry()
	require.NoError(t, registry.Register(&tools.ToolDescriptor{
		Name: "send_message", Description: "send", InputSchema: json.RawMessage(`{"type":"object"}`), Mode: "mock",
	}))
	ts := stage.NewTurnState()
	ts.ConversationID = conv.ID()
	ts.SystemPrompt = "You help."
	ts.AllowedTools = []string{"send_message"}
	probe := stage.NewProviderStageWithTurnState(
		mock.NewToolProviderWithRepository("p", "m", false, repo), registry,
		&rtpipeline.ToolPolicy{MaxCallsPerMinute: limit}, &stage.ProviderConfig{CallRate: conv.callRate}, nil, nil, ts)

	in := make(chan stage.StreamElement, 1)
	out := make(chan stage.StreamElement, 16)
	msg := types.Message{Role: "user", Content: "go"}
	in <- stage.StreamElement{Message: &msg}
	close(in)
	require.NoError(t, probe.Process(context.Background(), in, out))

	refused := 0
	for elem := range out {
		if elem.Message == nil || elem.Message.ToolResult == nil {
			continue
		}
		if elem.Message.ToolResult.Error != "" {
			refused++
		}
	}
	return refused
}
