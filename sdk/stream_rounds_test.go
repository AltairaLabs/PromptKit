package sdk

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

const roundsPackJSON = `{
	"id": "stream-rounds", "version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "{{variable}}"},
	"prompts": {"chat": {"id": "chat", "name": "Chat", "version": "1.0.0",
		"system_template": "You help.", "tools": ["lookup", "send_message"]}},
	"tools": {
		"lookup": {"name": "lookup", "description": "Look up an order",
			"parameters": {"type": "object", "properties": {"id": {"type": "string"}}}},
		"send_message": {"name": "send_message", "description": "Send a message",
			"parameters": {"type": "object", "properties": {"body": {"type": "string"}}}}
	}
}`

func openRoundsConv(t *testing.T, turns ...mock.Turn) *Conversation {
	t.Helper()
	provider := mock.NewToolProviderWithRepository("mock", "mock-model", false, &sequenceRepo{turns: turns})
	conv, err := Open(writeWorkflowTestPack(t, roundsPackJSON), "chat", WithProvider(provider), WithSkipSchemaValidation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	return conv
}

// summarizeStream drains a stream into one line per chunk, and returns its
// final response.
func summarizeStream(t *testing.T, ch <-chan StreamChunk) ([]string, *Response) {
	t.Helper()
	var got []string
	var resp *Response
	for chunk := range ch {
		require.NoError(t, chunk.Error)
		switch chunk.Type {
		case ChunkText:
			got = append(got, "text "+chunk.Text)
		case ChunkToolCall:
			got = append(got, fmt.Sprintf("call %d %s %s", chunk.Round, chunk.ToolCall.ID, chunk.ToolCall.Args))
		case ChunkToolResult:
			got = append(got, fmt.Sprintf("result %d %s %s", chunk.Round, chunk.ToolResult.ID, chunk.ToolResult.GetTextContent()))
		case ChunkClientTool:
			got = append(got, "client "+chunk.ClientTool.CallID)
		case ChunkDone:
			got = append(got, "done")
			resp = chunk.Message
		}
	}
	return got, resp
}

// Stream reports each tool round as it happens: its text, its call once the
// text has streamed, the call's result once it ran, then the next round.
func TestStream_ReportsEachToolRound(t *testing.T) {
	conv := openRoundsConv(t,
		mock.Turn{Type: "tool_calls", StreamChunks: []string{"Let me ", "look."},
			ToolCalls: []mock.ToolCall{{Name: "lookup", Arguments: map[string]any{"id": "1"}}}},
		mock.Turn{Type: "tool_calls", ToolCalls: []mock.ToolCall{{Name: "lookup", Arguments: map[string]any{"id": "2"}}}},
		mock.Turn{Type: "text", StreamChunks: []string{"Both ", "shipped."}},
	)
	conv.OnTool("lookup", func(args map[string]any) (any, error) { return args["id"], nil })

	got, resp := summarizeStream(t, conv.Stream(context.Background(), "check 1 and 2"))

	assert.Equal(t, []string{
		"text Let me ", "text look.",
		`call 0 call_0_lookup {"id":"1"}`, "result 0 call_0_lookup 1",
		`call 1 call_0_lookup {"id":"2"}`, "result 1 call_0_lookup 2",
		"text Both ", "text shipped.",
		"done",
	}, got)
	require.NotNil(t, resp)
	assert.Equal(t, "Both shipped.", resp.Text())
}

// A call an OnToolAsync check holds for approval is a pending tool of the
// streamed response, as it is of a sent one, and not a client tool for the
// caller to answer.
func TestStream_HoldsForApprovalAsSendDoes(t *testing.T) {
	conv := openRoundsConv(t,
		mock.Turn{Type: "tool_calls", StreamChunks: []string{"Sending."},
			ToolCalls: []mock.ToolCall{{Name: "send_message", Arguments: map[string]any{"body": "hi"}}}},
		mock.Turn{Type: "text", StreamChunks: []string{"Sent."}},
	)
	conv.OnToolAsync("send_message",
		func(map[string]any) sdktools.PendingResult {
			return sdktools.PendingResult{Reason: "requires_approval", Message: "Approve send?"}
		},
		func(map[string]any) (any, error) { return "sent", nil },
	)

	got, resp := summarizeStream(t, conv.Stream(context.Background(), "send hi"))

	assert.Equal(t, []string{"text Sending.", `call 0 call_0_send_message {"body":"hi"}`, "done"}, got)
	require.NotNil(t, resp)
	assert.Empty(t, resp.ClientTools())
	require.Len(t, resp.PendingTools(), 1)
	held := resp.PendingTools()[0]
	assert.Equal(t, PendingTool{
		ID: "call_0_send_message", Name: "send_message", Arguments: map[string]any{"body": "hi"},
		Reason: "requires_approval", Message: "Approve send?",
	}, held)

	// The hold resolves as one held by Send does.
	_, err := conv.ResolveTool(context.Background(), held.ID)
	require.NoError(t, err)
	cont, err := conv.Continue(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "Sent.", cont.Text())
}

// StreamWithCallback reports a call held for approval as a ToolApprovalEvent
// before the done event, so a handler can tell a paused turn from a finished
// one.
func TestStreamWithCallback_ReportsAnApprovalHold(t *testing.T) {
	conv := openRoundsConv(t,
		mock.Turn{Type: "tool_calls", StreamChunks: []string{"Sending."},
			ToolCalls: []mock.ToolCall{{Name: "send_message", Arguments: map[string]any{"body": "hi"}}}},
	)
	conv.OnToolAsync("send_message",
		func(map[string]any) sdktools.PendingResult {
			return sdktools.PendingResult{Reason: "requires_approval", Message: "Approve send?"}
		},
		func(map[string]any) (any, error) { return "sent", nil },
	)
	var got []string
	conv.OnStreamEvent(func(e StreamEvent) {
		switch ev := e.(type) {
		case TextDeltaEvent:
			got = append(got, "text "+ev.Delta)
		case ClientToolRequestEvent:
			got = append(got, "client "+ev.CallID)
		case ToolApprovalEvent:
			got = append(got, "approval "+ev.Tool.ID+" "+ev.Tool.Reason+" "+ev.Tool.Message)
		case StreamDoneEvent:
			got = append(got, "done")
		}
	})

	resp, err := conv.StreamWithCallback(context.Background(), "send hi")
	require.NoError(t, err)

	assert.Equal(t, []string{
		"text Sending.", "approval call_0_send_message requires_approval Approve send?", "done",
	}, got)
	require.Len(t, resp.PendingTools(), 1)
}
