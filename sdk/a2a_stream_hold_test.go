package sdk

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
	a2aserver "github.com/AltairaLabs/PromptKit/server/a2a/v2"
)

// holdingA2AClient serves, over A2A, a conversation whose model calls
// send_message, a tool an OnToolAsync check holds for approval. It returns the
// client and the conversations the server opened.
func holdingA2AClient(t *testing.T, turns ...mock.Turn) (*a2a.Client, *[]*Conversation) {
	t.Helper()
	opened := &[]*Conversation{}
	opener := func(string) (a2aserver.Conversation, error) {
		provider := mock.NewToolProviderWithRepository("mock", "mock-model", false, &sequenceRepo{turns: turns})
		conv, err := Open(writeWorkflowTestPack(t, roundsPackJSON), "chat", WithProvider(provider), WithSkipSchemaValidation())
		if err != nil {
			return nil, err
		}
		conv.OnToolAsync("send_message",
			func(map[string]any) sdktools.PendingResult {
				return sdktools.PendingResult{Reason: "requires_approval", Message: "Approve send?"}
			},
			func(map[string]any) (any, error) { return "sent", nil },
		)
		*opened = append(*opened, conv)
		return &streamConvAdapter{convAdapter: convAdapter{c: conv}}, nil
	}
	ts := httptest.NewServer(NewA2AServer(opener).Handler())
	t.Cleanup(ts.Close)
	return a2a.NewClient(ts.URL), opened
}

// A streamed turn held for approval leaves the task input-required, as a sent
// one does, not completed; the hold is the conversation's to resolve, and the
// turn continues once it is.
func TestA2AServer_StreamedApprovalHoldIsInputRequired(t *testing.T) {
	client, opened := holdingA2AClient(t,
		mock.Turn{Type: "tool_calls", StreamChunks: []string{"Sending."},
			ToolCalls: []mock.ToolCall{{Name: "send_message", Arguments: map[string]any{"body": "hi"}}}},
		mock.Turn{Type: "text", StreamChunks: []string{"Sent."}},
	)

	events, err := client.SendMessageStream(context.Background(), &a2a.SendMessageRequest{
		Message: a2a.Message{Role: a2a.RoleUser, Parts: []a2a.Part{{Text: a2aTextPtr("send hi")}}},
	})
	require.NoError(t, err)
	var arts []a2a.Artifact
	var states []a2a.TaskState
	for ev := range events {
		require.NoError(t, ev.Error)
		if ev.ArtifactUpdate != nil {
			arts = append(arts, ev.ArtifactUpdate.Artifact)
		}
		if ev.StatusUpdate != nil {
			states = append(states, ev.StatusUpdate.Status.State)
		}
	}
	require.NotEmpty(t, states)
	assert.Equal(t, a2a.TaskStateInputRequired, states[len(states)-1], "states: %v", states)
	assert.Equal(t, "Sending.", orchArtifactText(arts))

	require.Len(t, *opened, 1)
	conv := (*opened)[0]
	held, err := conv.PendingTools(context.Background())
	require.NoError(t, err)
	require.Len(t, held, 1)
	assert.Equal(t, "call_0_send_message", held[0].ID)
	assert.Equal(t, "send_message", held[0].Name)

	_, err = conv.ResolveTool(context.Background(), held[0].ID)
	require.NoError(t, err)
	resp, err := conv.Continue(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "Sent.", resp.Text())
}
