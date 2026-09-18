package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
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

// a2aSend posts a blocking message/send and returns the resulting task.
func a2aSend(t *testing.T, ts *httptest.Server, contextID, text string) *a2a.Task {
	t.Helper()

	params, err := json.Marshal(a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: contextID,
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: &text}},
		},
		Configuration: &a2a.SendMessageConfiguration{Blocking: true},
	})
	require.NoError(t, err)

	body, err := json.Marshal(a2a.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  a2a.MethodSendMessage,
		Params:  params,
	})
	require.NoError(t, err)

	resp, err := http.Post(ts.URL+"/a2a", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	var rpcResp a2a.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rpcResp))
	require.Nil(t, rpcResp.Error, "RPC error from message/send")

	var task a2a.Task
	require.NoError(t, json.Unmarshal(rpcResp.Result, &task))
	return &task
}
