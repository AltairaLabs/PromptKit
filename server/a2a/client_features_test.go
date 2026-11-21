package a2aserver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
)

// The runtime client's SubscribeToTask and GetExtendedAgentCard (#2134),
// against a real PromptKit server.

// collectEvents reads ch until it closes, failing after a few seconds.
func collectEvents(t *testing.T, ch <-chan a2a.StreamEvent) []a2a.StreamEvent {
	t.Helper()
	var events []a2a.StreamEvent
	timeout := time.After(3 * time.Second)
	for {
		select {
		case evt, open := <-ch:
			if !open {
				return events
			}
			events = append(events, evt)
		case <-timeout:
			t.Fatalf("stream did not end; got %d events", len(events))
		}
	}
}

// heldTurn is a conversation whose turn answers "done" once release closes.
func heldTurn(release <-chan struct{}) *mockConv {
	return &mockConv{sendFunc: func(context.Context, any) (SendResult, error) {
		<-release
		return &mockSendResult{text: "done"}, nil
	}}
}

// SubscribeToTask opens with the task as it stands, carries its updates, and
// ends when the task finishes (A2A 1.0 §3.1.6), in either protocol version.
func TestClient_SubscribeToTaskFollowsARunningTask(t *testing.T) {
	for _, v := range []a2a.ProtocolVersion{a2a.ProtocolVersion10, a2a.ProtocolVersion03} {
		t.Run(string(v), func(t *testing.T) {
			release := make(chan struct{})
			srv, ts := newTestServer(func(string) (Conversation, error) { return heldTurn(release), nil })
			defer ts.Close()
			client := a2a.NewClient(ts.URL, a2a.WithProtocolVersion(v))
			ctx := context.Background()

			started, err := client.SendMessage(ctx, &a2a.SendMessageRequest{
				Message:       userMessage("ctx-subscribe"),
				Configuration: &a2a.SendMessageConfiguration{ReturnImmediately: true},
			})
			require.NoError(t, err)
			require.Equal(t, a2a.TaskStateWorking, started.Status.State)

			events, err := client.SubscribeToTask(ctx, started.ID)
			require.NoError(t, err)
			subscribed(t, srv, started.ID)
			close(release)

			got := collectEvents(t, events)
			require.Len(t, got, 3, "the task, its artifact and its completion")
			require.NotNil(t, got[0].Task)
			assert.Equal(t, started.ID, got[0].Task.ID)
			assert.Equal(t, a2a.TaskStateWorking, got[0].Task.Status.State)
			require.NotNil(t, got[1].ArtifactUpdate)
			assert.Equal(t, "done", *got[1].ArtifactUpdate.Artifact.Parts[0].Text)
			require.NotNil(t, got[2].StatusUpdate)
			assert.Equal(t, started.ID, got[2].StatusUpdate.TaskID)
			assert.Equal(t, a2a.TaskStateCompleted, got[2].StatusUpdate.Status.State)
		})
	}
}

// A refused subscription is an *RPCError from the call: an unknown task is
// TaskNotFoundError, a finished one UnsupportedOperationError.
func TestClient_SubscribeToTaskRefusals(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) { return completingMock(), nil })
	defer ts.Close()
	client := a2a.NewClient(ts.URL)
	ctx := context.Background()

	done, err := client.SendMessage(ctx, &a2a.SendMessageRequest{Message: userMessage("ctx-done")})
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateCompleted, done.Status.State)

	for id, code := range map[string]int{
		"no-such-task": a2a.ErrCodeTaskNotFound,
		done.ID:        a2a.ErrCodeUnsupportedOperation,
	} {
		ch, err := client.SubscribeToTask(ctx, id)
		assert.Nil(t, ch)
		var rpcErr *a2a.RPCError
		require.True(t, errors.As(err, &rpcErr), "%v", err)
		assert.Equal(t, code, rpcErr.Code, id)
	}
}

// GetExtendedAgentCard returns the card the server serves to an authenticated
// caller, and the client routes by it from then on (A2A 1.0 §3.1.11: clients
// SHOULD replace their cached public card with it).
func TestClient_GetExtendedAgentCard(t *testing.T) {
	for _, v := range []a2a.ProtocolVersion{a2a.ProtocolVersion10, a2a.ProtocolVersion03} {
		t.Run(string(v), func(t *testing.T) {
			_, ts := newTestServer(nopOpener,
				WithCard(&a2a.AgentCard{Name: "public", Description: "d", Version: "1"}),
				WithExtendedCardProvider(&StaticCard{Card: a2a.AgentCard{
					Name: "extended", Description: "d", Version: "1",
					Skills: []a2a.AgentSkill{{ID: "secret", Name: "Secret", Description: "s", Tags: []string{"x"}}},
				}}))
			defer ts.Close()
			client := a2a.NewClient(ts.URL, a2a.WithProtocolVersion(v))
			ctx := context.Background()

			public, err := client.Discover(ctx)
			require.NoError(t, err)
			assert.Equal(t, "public", public.Name)
			assert.True(t, public.Capabilities.ExtendedAgentCard, "the public card declares the extended one")

			extended, err := client.GetExtendedAgentCard(ctx)
			require.NoError(t, err)
			assert.Equal(t, "extended", extended.Name)
			require.Len(t, extended.Skills, 1)
			assert.Equal(t, "secret", extended.Skills[0].ID)
			assert.NotEmpty(t, extended.SupportedInterfaces, "the server completes the extended card as it does the public one")

			cached, err := client.Discover(ctx)
			require.NoError(t, err)
			assert.Equal(t, "extended", cached.Name, "the extended card replaces the cached public one")
		})
	}
}

// Without an extended card the call fails with UnsupportedOperationError.
func TestClient_GetExtendedAgentCardWhenNoneIsDeclared(t *testing.T) {
	_, ts := newTestServer(nopOpener)
	defer ts.Close()
	card, err := a2a.NewClient(ts.URL).GetExtendedAgentCard(context.Background())
	assert.Nil(t, card)
	var rpcErr *a2a.RPCError
	require.True(t, errors.As(err, &rpcErr), "%v", err)
	assert.Equal(t, a2a.ErrCodeUnsupportedOperation, rpcErr.Code)
}
