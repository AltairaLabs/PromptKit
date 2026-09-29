package integration

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
	sdk "github.com/AltairaLabs/PromptKit/sdk/v2"
	a2aserver "github.com/AltairaLabs/PromptKit/server/a2a/v2"
)

// TestWithToolExecutor_OverA2AHTTP drives the real thing: an A2A server whose
// conversations come from A2AOpener, reached over HTTP with a JSON-RPC
// message/send. This is the shape #2019 is actually about — the embedder never
// touches the conversation, so the executor has to arrive as an option or not
// at all.
func TestWithToolExecutor_OverA2AHTTP(t *testing.T) {
	executor := &recordingExecutor{}
	packPath := writePackFile(t, toolsPackWithAllowedToolsJSON)

	opener := sdk.A2AOpener(packPath, "chat",
		sdk.WithProvider(weatherToolProvider(t)),
		sdk.WithToolExecutor("get_weather", executor),
		sdk.WithSkipSchemaValidation(),
	)

	srv := a2aserver.NewServer(a2aserver.ConversationOpener(opener))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	task := a2aSend(t, ts, "ctx-http-1", "What is the weather in Paris?")

	require.Equal(t, int64(1), executor.calls.Load(),
		"the executor given as an option never ran; A2A tool calls bypassed it")
	assert.Equal(t, a2a.TaskStateCompleted, task.Status.State)
	require.NotEmpty(t, task.Artifacts)
	require.NotEmpty(t, task.Artifacts[0].Parts)
	require.NotNil(t, task.Artifacts[0].Parts[0].Text)
	assert.Contains(t, *task.Artifacts[0].Parts[0].Text, "sunny")
}

// a2aSend sends a blocking message through the runtime A2A client and returns
// the resulting task.
func a2aSend(t *testing.T, ts *httptest.Server, contextID, text string) *a2a.Task {
	t.Helper()

	task, err := a2a.NewClient(ts.URL).SendMessage(context.Background(), &a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: contextID,
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: &text}},
		},
		Configuration: &a2a.SendMessageConfiguration{Blocking: true},
	})
	require.NoError(t, err, "SendMessage")
	return task
}
