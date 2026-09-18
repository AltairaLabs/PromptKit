package a2aserver

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A completed task has to carry its result whichever RPC produced it. The
// streaming path emitted artifacts as SSE events and stored none, so a client
// that streamed and later re-read the task — another process, a reconnect, an
// audit — found a completed task with nothing in it (#2032).

func streamingConvSaying(parts ...StreamEvent) *mockStreamConv {
	return &mockStreamConv{
		streamFunc: func(_ context.Context, _ any) <-chan StreamEvent {
			ch := make(chan StreamEvent, len(parts)+1)
			for _, p := range parts {
				ch <- p
			}
			ch <- StreamEvent{Kind: EventDone}
			close(ch)
			return ch
		},
	}
}

func TestStream_StoresArtifactsOnTheCompletedTask(t *testing.T) {
	mock := streamingConvSaying(
		StreamEvent{Kind: EventText, Text: "Hello "},
		StreamEvent{Kind: EventText, Text: "world"},
	)
	srv, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	events := readSSEEvents(t, ts, a2a.MethodSendStreamingMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-artifacts",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("hi")}},
		},
	})
	require.NotEmpty(t, events)

	taskID := taskIDFromEvents(t, events)
	task, err := srv.taskStore.Get(taskID)
	require.NoError(t, err)

	require.Equal(t, a2a.TaskStateCompleted, task.Status.State)
	require.Len(t, task.Artifacts, 2,
		"the streamed artifacts were emitted but never stored; tasks/get returns an empty task")
	require.NotNil(t, task.Artifacts[0].Parts[0].Text)
	assert.Equal(t, "Hello ", *task.Artifacts[0].Parts[0].Text)
	require.NotNil(t, task.Artifacts[1].Parts[0].Text)
	assert.Equal(t, "world", *task.Artifacts[1].Parts[0].Text)
}

// What is stored is what was sent: same ids, same parts, same order.
func TestStream_StoredArtifactsMatchWhatWasEmitted(t *testing.T) {
	mock := streamingConvSaying(
		StreamEvent{Kind: EventText, Text: "one"},
		StreamEvent{Kind: EventMedia, Media: &types.MediaContent{
			MIMEType: "image/png",
			URL:      serverTextPtr("https://example.test/i.png"),
		}},
	)
	srv, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	events := readSSEEvents(t, ts, a2a.MethodSendStreamingMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-artifacts-match",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("hi")}},
		},
	})

	var emitted []a2a.Artifact
	for _, evt := range events {
		if evt.ArtifactUpdate != nil {
			emitted = append(emitted, evt.ArtifactUpdate.Artifact)
		}
	}
	require.Len(t, emitted, 2, "expected one artifact event per streamed part")

	task, err := srv.taskStore.Get(taskIDFromEvents(t, events))
	require.NoError(t, err)

	require.Len(t, task.Artifacts, len(emitted))
	for i := range emitted {
		assert.Equal(t, emitted[i].ArtifactID, task.Artifacts[i].ArtifactID,
			"stored artifact %d does not match the one the client received", i)
		assert.Equal(t, emitted[i].Parts, task.Artifacts[i].Parts)
	}
}

// A failed stream stores no artifacts and says it failed — the fix must not
// quietly complete a turn that errored.
func TestStream_FailedTurnStoresNothing(t *testing.T) {
	mock := &mockStreamConv{
		streamFunc: func(_ context.Context, _ any) <-chan StreamEvent {
			ch := make(chan StreamEvent, 2)
			ch <- StreamEvent{Kind: EventText, Text: "partial"}
			ch <- StreamEvent{Error: assertError("provider exploded")}
			close(ch)
			return ch
		},
	}
	srv, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	events := readSSEEvents(t, ts, a2a.MethodSendStreamingMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-artifacts-fail",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("hi")}},
		},
	})

	task, err := srv.taskStore.Get(taskIDFromEvents(t, events))
	require.NoError(t, err)
	assert.Equal(t, a2a.TaskStateFailed, task.Status.State)
	assert.Empty(t, task.Artifacts, "a failed turn must not leave a partial result behind as its output")
}

// taskIDFromEvents pulls the task id out of whichever event carries one.
func taskIDFromEvents(t *testing.T, events []a2a.StreamEvent) string {
	t.Helper()
	for _, evt := range events {
		if evt.ArtifactUpdate != nil && evt.ArtifactUpdate.TaskID != "" {
			return evt.ArtifactUpdate.TaskID
		}
		if evt.StatusUpdate != nil && evt.StatusUpdate.TaskID != "" {
			return evt.StatusUpdate.TaskID
		}
	}
	t.Fatal("no event carried a task id")
	return ""
}

type stringError string

func (e stringError) Error() string { return string(e) }

func assertError(msg string) error { return stringError(msg) }
