package a2aserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Conformance with A2A 1.0 and 0.3 (#2088). These tests look at the wire
// bytes, not at what the lenient runtime decoder makes of them: a standard
// client parses exactly what is sent.

// rawRPC posts a JSON-RPC request with an optional A2A-Version header and
// returns the HTTP response.
func rawRPC(t *testing.T, ts *httptest.Server, version, method string, params any) *http.Response {
	t.Helper()
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)
	body, err := json.Marshal(a2a.JSONRPCRequest{JSONRPC: "2.0", ID: 7, Method: method, Params: paramsJSON})
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/a2a", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if version != "" {
		req.Header.Set(a2a.HeaderVersion, version)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

// rawResult returns a JSON-RPC response's result as a generic map, failing on
// an error response.
func rawResult(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	var out struct {
		Result map[string]any    `json:"result"`
		Error  *a2a.JSONRPCError `json:"error"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Nil(t, out.Error, "unexpected error: %+v", out.Error)
	return out.Result
}

// rawError returns a JSON-RPC response's error.
func rawError(t *testing.T, resp *http.Response) *a2a.JSONRPCError {
	t.Helper()
	defer resp.Body.Close()
	var out a2a.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.NotNil(t, out.Error, "expected an error, got result %s", out.Result)
	return out.Error
}

// rawStream reads an SSE response into its JSON-RPC results.
func rawStream(t *testing.T, resp *http.Response) (ids []any, results []map[string]any) {
	t.Helper()
	defer resp.Body.Close()
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var env struct {
			ID     any            `json:"id"`
			Result map[string]any `json:"result"`
		}
		require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &env))
		ids = append(ids, env.ID)
		results = append(results, env.Result)
	}
	return ids, results
}

func userMessage(contextID string, parts ...a2a.Part) a2a.Message {
	if len(parts) == 0 {
		parts = []a2a.Part{{Text: serverTextPtr("hi")}}
	}
	return a2a.Message{MessageID: "m1", ContextID: contextID, Role: a2a.RoleUser, Parts: parts}
}

func TestConformance_CardPathsAndVersions(t *testing.T) {
	card := &a2a.AgentCard{
		Name: "agent", Description: "d", Version: "1",
		SupportedInterfaces: []a2a.AgentInterface{{URL: "https://agent.example/a2a", ProtocolBinding: "jsonrpc+http"}},
		SecuritySchemes: map[string]a2a.SecurityScheme{
			"bearer": {HTTPAuth: &a2a.HTTPAuthSecurityScheme{Scheme: "Bearer"}},
		},
		SecurityRequirements: []a2a.SecurityRequirement{a2a.RequireScheme("bearer")},
	}
	_, ts := newTestServer(nopOpener, WithCard(card))
	defer ts.Close()

	get := func(path, version string) map[string]any {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+path, http.NoBody)
		if version != "" {
			req.Header.Set(a2a.HeaderVersion, version)
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode, path)
		var m map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&m))
		return m
	}

	v1 := get(a2a.AgentCardPath, "1.0")
	ifaces := v1["supportedInterfaces"].([]any)
	require.Len(t, ifaces, 2, "the declared interface for 1.0 plus its 0.3 twin")
	assert.Equal(t, map[string]any{"url": "https://agent.example/a2a", "protocolBinding": "JSONRPC", "protocolVersion": "1.0"}, ifaces[0])
	assert.Equal(t, "0.3", ifaces[1].(map[string]any)["protocolVersion"])
	assert.Contains(t, v1, "securitySchemes")
	assert.NotContains(t, v1, "url", "a 1.0 card has no 0.3 fields")

	for _, path := range []string{a2a.AgentCardPath, a2a.LegacyAgentCardPath} {
		v03 := get(path, "")
		assert.Equal(t, "0.3.0", v03["protocolVersion"], path)
		assert.Equal(t, "https://agent.example/a2a", v03["url"], path)
		assert.Equal(t, "JSONRPC", v03["preferredTransport"], path)
		assert.Equal(t, []any{map[string]any{"bearer": []any{}}}, v03["security"], path)
		assert.Contains(t, v03, "supportedInterfaces", path)
	}

	// The provider's card is not modified by serving it.
	assert.Equal(t, "jsonrpc+http", card.SupportedInterfaces[0].ProtocolBinding)
}

func TestConformance_CardWithoutInterfacesPointsAtThisServer(t *testing.T) {
	_, ts := newTestServer(nopOpener, WithCard(&a2a.AgentCard{Name: "bare"}))
	defer ts.Close()

	// Forwarded headers are caller-controlled; a card that trusted them could
	// be poisoned in a cache to send other callers elsewhere.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+a2a.AgentCardPath, http.NoBody)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "evil.example")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	var card a2a.AgentCard
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&card))
	require.Len(t, card.SupportedInterfaces, 2)
	assert.Equal(t, ts.URL+"/a2a", card.SupportedInterfaces[0].URL)
	assert.Equal(t, a2a.ProtocolVersion10, card.PreferredVersion())

	bad, err := http.NewRequest(http.MethodGet, ts.URL+a2a.AgentCardPath, http.NoBody)
	require.NoError(t, err)
	bad.Header.Set(a2a.HeaderVersion, "9.9")
	badResp, err := http.DefaultClient.Do(bad)
	require.NoError(t, err)
	badResp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, badResp.StatusCode)
}

func TestConformance_SendMessageShapes(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) { return completingMock(), nil })
	defer ts.Close()

	blocking := &a2a.SendMessageConfiguration{Blocking: true}

	// 1.0: result wrapped as {task}, ProtoJSON enums, no kind.
	v1 := rawResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-v1")}))
	task := v1["task"].(map[string]any)
	assert.Equal(t, "TASK_STATE_COMPLETED", task["status"].(map[string]any)["state"],
		"1.0 blocks by default, so the task is finished")
	assert.NotContains(t, task, "kind")

	// 0.3 by method name, no header: the bare, kind-tagged task.
	v03 := rawResult(t, rawRPC(t, ts, "", a2a.MethodV03SendMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-v03"), Configuration: blocking}))
	assert.Equal(t, "task", v03["kind"])
	assert.Equal(t, "completed", v03["status"].(map[string]any)["state"])
	part := v03["artifacts"].([]any)[0].(map[string]any)["parts"].([]any)[0].(map[string]any)
	assert.Equal(t, "text", part["kind"])

	// The header wins over the method name.
	forced := rawResult(t, rawRPC(t, ts, "0.3", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-forced"), Configuration: blocking}))
	assert.Equal(t, "task", forced["kind"])

	// An unsupported version is refused with VersionNotSupported.
	e := rawError(t, rawRPC(t, ts, "2.0", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-bad")}))
	assert.Equal(t, a2a.ErrCodeVersionNotSupported, e.Code)
}

func TestConformance_V03FilePartsAreAccepted(t *testing.T) {
	var got []types.ContentPart
	mock := &mockConv{sendFunc: func(_ context.Context, msg any) (SendResult, error) {
		got = msg.(*types.Message).Parts
		return &mockSendResult{text: "ok"}, nil
	}}
	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	body := `{"jsonrpc":"2.0","id":1,"method":"message/send","params":{"configuration":{"blocking":true},
		"message":{"kind":"message","messageId":"m","role":"user","parts":[
			{"kind":"text","text":"look"},
			{"kind":"file","file":{"bytes":"/9g=","mimeType":"image/jpeg","name":"x.jpg"}}]}}}`
	resp, err := http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	result := rawResult(t, resp)
	assert.Equal(t, "completed", result["status"].(map[string]any)["state"])
	require.Len(t, got, 2)
	require.NotNil(t, got[1].Media)
	assert.Equal(t, "image/jpeg", got[1].Media.MIMEType)
}

func TestConformance_SendMessageReturnImmediately(t *testing.T) {
	release := make(chan struct{})
	mock := &mockConv{sendFunc: func(context.Context, any) (SendResult, error) {
		<-release
		return &mockSendResult{text: "ok"}, nil
	}}
	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()
	defer close(release)

	result := rawResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage, a2a.SendMessageRequest{
		Message:       userMessage("ctx-now"),
		Configuration: &a2a.SendMessageConfiguration{ReturnImmediately: true},
	}))
	state := result["task"].(map[string]any)["status"].(map[string]any)["state"]
	assert.Contains(t, []any{"TASK_STATE_SUBMITTED", "TASK_STATE_WORKING"}, state)
}

// streamConv streams events, and answers Send with the same text.
func streamConv(events ...StreamEvent) *mockStreamConv {
	conv := streamingConvSaying(events...)
	var text strings.Builder
	for _, e := range events {
		if e.Kind == EventText {
			text.WriteString(e.Text)
		}
	}
	conv.sendFunc = func(context.Context, any) (SendResult, error) {
		return &mockSendResult{parts: []types.ContentPart{types.NewTextPart(text.String())}, text: text.String()}, nil
	}
	return conv
}

func TestConformance_StreamShapes(t *testing.T) {
	conv := func() *mockStreamConv {
		return streamConv(
			StreamEvent{Kind: EventText, Text: "a"},
			StreamEvent{Kind: EventText, Text: "b"},
		)
	}
	_, ts := newTestServer(func(string) (Conversation, error) { return conv(), nil })
	defer ts.Close()

	// 1.0: Task first, every result wrapped, no final flag.
	ids, v1 := rawStream(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendStreamingMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-s1")}))
	require.Len(t, v1, 4)
	assert.Contains(t, v1[0], "task", "the stream opens with the Task")
	assert.Equal(t, "TASK_STATE_WORKING", v1[0]["task"].(map[string]any)["status"].(map[string]any)["state"])
	first := v1[1]["artifactUpdate"].(map[string]any)
	second := v1[2]["artifactUpdate"].(map[string]any)
	assert.NotContains(t, first, "append")
	assert.Equal(t, true, second["append"])
	assert.Equal(t, true, second["lastChunk"])
	assert.Equal(t, first["artifact"].(map[string]any)["artifactId"], second["artifact"].(map[string]any)["artifactId"])
	final := v1[3]["statusUpdate"].(map[string]any)
	assert.Equal(t, "TASK_STATE_COMPLETED", final["status"].(map[string]any)["state"])
	assert.NotContains(t, final, "final")
	for _, id := range ids {
		assert.Equal(t, float64(7), id, "every event answers the caller's request id")
	}

	// 0.3: bare, kind-tagged, final on the last status.
	_, v03 := rawStream(t, rawRPC(t, ts, "", a2a.MethodV03SendStreamingMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-s03")}))
	require.Len(t, v03, 4)
	assert.Equal(t, "task", v03[0]["kind"])
	assert.Equal(t, "artifact-update", v03[1]["kind"])
	assert.Equal(t, "status-update", v03[3]["kind"])
	assert.Equal(t, true, v03[3]["final"])
	assert.Equal(t, "completed", v03[3]["status"].(map[string]any)["state"])
}

func TestConformance_StreamPendingIsInputRequired(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) {
		return streamConv(StreamEvent{Kind: EventPending, Text: "awaiting approval"}), nil
	})
	defer ts.Close()

	_, events := rawStream(t, rawRPC(t, ts, "", a2a.MethodV03SendStreamingMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-pending")}))
	last := events[len(events)-1]
	assert.Equal(t, "input-required", last["status"].(map[string]any)["state"],
		"a turn waiting on approval must not be reported completed")
	assert.Equal(t, true, last["final"])
}

func TestConformance_CancelTask(t *testing.T) {
	started := make(chan struct{})
	mock := &mockConv{sendFunc: func(ctx context.Context, _ any) (SendResult, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	srv, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	task := decodeTaskResult(t, a2aRPCRequest(t, ts, a2a.MethodV1SendMessage, a2a.SendMessageRequest{
		Message:       userMessage("ctx-cancel"),
		Configuration: &a2a.SendMessageConfiguration{ReturnImmediately: true},
	}).Result)
	<-started

	canceled := rawResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1CancelTask, a2a.CancelTaskRequest{ID: task.ID}))
	assert.Equal(t, "TASK_STATE_CANCELED", canceled["status"].(map[string]any)["state"])

	// Canceling it again: the task is terminal, which is TaskNotCancelable.
	e := rawError(t, rawRPC(t, ts, "1.0", a2a.MethodV1CancelTask, a2a.CancelTaskRequest{ID: task.ID}))
	assert.Equal(t, a2a.ErrCodeTaskNotCancelable, e.Code)

	e = rawError(t, rawRPC(t, ts, "1.0", a2a.MethodV1CancelTask, a2a.CancelTaskRequest{ID: "missing"}))
	assert.Equal(t, a2a.ErrCodeTaskNotFound, e.Code)

	// A failed cancel never reaches a registered turn.
	interrupted := false
	srv.registerCancel(task.ID, func() { interrupted = true })
	_ = rawError(t, rawRPC(t, ts, "1.0", a2a.MethodV1CancelTask, a2a.CancelTaskRequest{ID: task.ID}))
	assert.False(t, interrupted, "canceling a finished task must not interrupt anything")
}

func TestConformance_CancelDuringStreamTellsTheStreamer(t *testing.T) {
	streaming := make(chan struct{})
	mock := &mockStreamConv{streamFunc: func(ctx context.Context, _ any) <-chan StreamEvent {
		ch := make(chan StreamEvent)
		go func() {
			ch <- StreamEvent{Kind: EventText, Text: "partial"}
			close(streaming)
			<-ctx.Done()
		}()
		return ch
	}}
	srv, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	done := make(chan []map[string]any)
	go func() {
		_, events := rawStream(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendStreamingMessage,
			a2a.SendMessageRequest{Message: userMessage("ctx-stream-cancel")}))
		done <- events
	}()
	<-streaming

	var taskID string
	require.Eventually(t, func() bool {
		srv.cancelsMu.Lock()
		defer srv.cancelsMu.Unlock()
		for id := range srv.cancels {
			taskID = id
		}
		return taskID != ""
	}, time.Second, 5*time.Millisecond)
	_ = rawResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1CancelTask, a2a.CancelTaskRequest{ID: taskID}))

	select {
	case events := <-done:
		last := events[len(events)-1]["statusUpdate"].(map[string]any)
		assert.Equal(t, "TASK_STATE_CANCELED", last["status"].(map[string]any)["state"])
	case <-time.After(3 * time.Second):
		t.Fatal("stream did not end after its task was canceled")
	}
}

func TestConformance_UnsupportedOperations(t *testing.T) {
	_, ts := newTestServer(nopOpener)
	defer ts.Close()

	for _, method := range []string{
		a2a.MethodV1CreateTaskPushNotificationConfig, a2a.MethodV03SetPushNotificationConfig,
	} {
		e := rawError(t, rawRPC(t, ts, "", method, map[string]any{"taskId": "t"}))
		assert.Equal(t, a2a.ErrCodePushNotificationNotSupported, e.Code, method)
	}
	// The card does not declare an extended card, so the method is
	// unsupported (A2A 1.0 §3.3.4), not "not configured".
	e := rawError(t, rawRPC(t, ts, "", a2a.MethodV1GetExtendedAgentCard, map[string]any{}))
	assert.Equal(t, a2a.ErrCodeUnsupportedOperation, e.Code)
	e = rawError(t, rawRPC(t, ts, "", "tasks/frobnicate", map[string]any{}))
	assert.Equal(t, a2a.ErrCodeMethodNotFound, e.Code)
	e = rawError(t, rawRPC(t, ts, "", a2a.MethodV1GetTask, nil))
	assert.Equal(t, a2a.ErrCodeInvalidParams, e.Code)
}

func TestConformance_GetTaskHistoryLength(t *testing.T) {
	srv, ts := newTestServer(nopOpener)
	defer ts.Close()

	_, err := srv.taskStore.Create("t", "c")
	require.NoError(t, err)
	store := srv.taskStore.(*InMemoryTaskStore)
	store.mu.Lock()
	store.tasks["t"].History = []a2a.Message{
		{MessageID: "1", Role: a2a.RoleUser}, {MessageID: "2", Role: a2a.RoleAgent}, {MessageID: "3", Role: a2a.RoleUser},
	}
	store.mu.Unlock()

	get := func(n *int) []any {
		result := rawResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1GetTask, a2a.GetTaskRequest{ID: "t", HistoryLength: n}))
		h, _ := result["history"].([]any)
		return h
	}
	zero, two := 0, 2
	assert.Len(t, get(nil), 3)
	assert.Empty(t, get(&zero))
	last := get(&two)
	require.Len(t, last, 2)
	assert.Equal(t, "2", last[0].(map[string]any)["messageId"])
}

func TestConformance_ListTasksPaging(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) { return completingMock(), nil })
	defer ts.Close()
	for i := 0; i < 3; i++ {
		a2aSendMessage(t, ts, "ctx-page", "hi")
	}

	e := rawError(t, rawRPC(t, ts, "1.0", a2a.MethodV1ListTasks, a2a.ListTasksRequest{}))
	assert.Equal(t, a2a.ErrCodeInvalidParams, e.Code, "listing without a context is refused")

	seen := map[string]bool{}
	token := ""
	for page := 0; ; page++ {
		require.Less(t, page, 3, "paging did not terminate")
		result := rawResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1ListTasks,
			a2a.ListTasksRequest{ContextID: "ctx-page", PageSize: 2, PageToken: token}))
		assert.Equal(t, float64(3), result["totalSize"])
		assert.Equal(t, float64(2), result["pageSize"])
		require.Contains(t, result, "nextPageToken", "nextPageToken is always present")
		for _, raw := range result["tasks"].([]any) {
			task := raw.(map[string]any)
			assert.NotContains(t, task, "artifacts", "artifacts are omitted unless asked for")
			seen[task["id"].(string)] = true
		}
		token = result["nextPageToken"].(string)
		if token == "" {
			break
		}
	}
	assert.Len(t, seen, 3)

	withArtifacts := rawResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1ListTasks,
		a2a.ListTasksRequest{ContextID: "ctx-page", IncludeArtifacts: true}))
	assert.Contains(t, withArtifacts["tasks"].([]any)[0], "artifacts")

	e = rawError(t, rawRPC(t, ts, "1.0", a2a.MethodV1ListTasks,
		a2a.ListTasksRequest{ContextID: "ctx-page", PageToken: "garbage!"}))
	assert.Equal(t, a2a.ErrCodeInvalidParams, e.Code)
}

// newListTasksServer serves n submitted tasks in one context, put straight into
// the store so a test can hold more than a page of them cheaply.
func newListTasksServer(t *testing.T, contextID string, n int) *httptest.Server {
	t.Helper()
	store := NewInMemoryTaskStore()
	for i := range n {
		_, err := store.Create(fmt.Sprintf("task-%03d", i), contextID)
		require.NoError(t, err)
	}
	_, ts := newTestServer(nopOpener, WithTaskStore(store))
	t.Cleanup(ts.Close)
	return ts
}

// The 1.0 proto: "If unspecified, at most 50 tasks will be returned ... The
// maximum value is 100."
func TestConformance_ListTasksPageSizeDefaultAndMax(t *testing.T) {
	ts := newListTasksServer(t, "ctx-many", 120)

	unset := rawResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1ListTasks, a2a.ListTasksRequest{ContextID: "ctx-many"}))
	assert.Len(t, unset["tasks"], 50)
	assert.Equal(t, float64(50), unset["pageSize"])
	assert.Equal(t, float64(120), unset["totalSize"])

	tooBig := rawResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1ListTasks,
		a2a.ListTasksRequest{ContextID: "ctx-many", PageSize: 500}))
	assert.Len(t, tooBig["tasks"], 100)
	assert.Equal(t, float64(100), tooBig["pageSize"])
}

// TASK_STATE_UNSPECIFIED is the proto's zero value for status, so it means no
// filter rather than "tasks in no state", which would match nothing.
func TestConformance_ListTasksUnspecifiedStatusIsNoFilter(t *testing.T) {
	ts := newListTasksServer(t, "ctx-unspecified", 3)

	result := rawResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1ListTasks,
		map[string]any{"contextId": "ctx-unspecified", "status": "TASK_STATE_UNSPECIFIED"}))
	assert.Len(t, result["tasks"], 3)
	assert.Equal(t, float64(3), result["totalSize"])
}

// The 1.0 proto does not govern PromptKit's legacy 0.3 tasks/list: it keeps
// its default page of 100, and "unknown" is a real 0.3 state that filters.
func TestConformance_LegacyListTasksKeepsItsBehavior(t *testing.T) {
	ts := newListTasksServer(t, "ctx-legacy", 120)

	unset := rawResult(t, rawRPC(t, ts, "", a2a.MethodLegacyListTasks, a2a.ListTasksRequest{ContextID: "ctx-legacy"}))
	assert.Len(t, unset["tasks"], 100)

	unknown := rawResult(t, rawRPC(t, ts, "", a2a.MethodLegacyListTasks,
		map[string]any{"contextId": "ctx-legacy", "status": "unknown"}))
	assert.Empty(t, unknown["tasks"])
	assert.Equal(t, float64(0), unknown["totalSize"])
}

// The client pages through every task with ListTasksPage's NextPageToken.
func TestConformance_ClientPagesThroughListTasks(t *testing.T) {
	ts := newListTasksServer(t, "ctx-client-page", 7)
	client := a2a.NewClient(ts.URL, a2a.WithProtocolVersion(a2a.ProtocolVersion10))

	seen := map[string]bool{}
	params := &a2a.ListTasksRequest{ContextID: "ctx-client-page", PageSize: 3}
	for page := 0; ; page++ {
		require.Less(t, page, 4, "paging did not terminate")
		resp, err := client.ListTasksPage(context.Background(), params)
		require.NoError(t, err)
		assert.Equal(t, 7, resp.TotalSize)
		for i := range resp.Tasks {
			seen[resp.Tasks[i].ID] = true
		}
		if resp.NextPageToken == "" {
			break
		}
		params.PageToken = resp.NextPageToken
	}
	assert.Len(t, seen, 7)
}

func TestConformance_SubscribeOpensWithTheTask(t *testing.T) {
	release := make(chan struct{})
	mock := &mockConv{sendFunc: func(context.Context, any) (SendResult, error) {
		<-release
		return &mockSendResult{parts: []types.ContentPart{types.NewTextPart("done")}, text: "done"}, nil
	}}
	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	// A task from SendMessage — not a stream — can be subscribed to, too.
	task := decodeTaskResult(t, a2aRPCRequest(t, ts, a2a.MethodV1SendMessage, a2a.SendMessageRequest{
		Message:       userMessage("ctx-sub"),
		Configuration: &a2a.SendMessageConfiguration{ReturnImmediately: true},
	}).Result)

	got := make(chan []map[string]any)
	go func() {
		_, events := rawStream(t, rawRPC(t, ts, "", a2a.MethodV03Resubscribe, a2a.SubscribeTaskRequest{ID: task.ID}))
		got <- events
	}()
	time.Sleep(50 * time.Millisecond)
	close(release)

	select {
	case events := <-got:
		require.GreaterOrEqual(t, len(events), 2)
		assert.Equal(t, "task", events[0]["kind"], "the subscription opens with the task")
		last := events[len(events)-1]
		assert.Equal(t, "status-update", last["kind"])
		assert.Equal(t, "completed", last["status"].(map[string]any)["state"])
		assert.Equal(t, true, last["final"])
		var sawArtifact bool
		for _, e := range events {
			sawArtifact = sawArtifact || e["kind"] == "artifact-update"
		}
		assert.True(t, sawArtifact, "the subscriber gets the result before the completion")
	case <-time.After(3 * time.Second):
		t.Fatal("subscription did not end when the task completed")
	}
}

// The runtime client, negotiating, interoperates with the server in both
// versions — the pairing a PromptKit-to-PromptKit deployment depends on.
func TestConformance_RuntimeClientInterop(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) {
		return streamConv(StreamEvent{Kind: EventText, Text: "hello"}), nil
	})
	defer ts.Close()

	for _, v := range []a2a.ProtocolVersion{a2a.ProtocolVersion10, a2a.ProtocolVersion03} {
		client := a2a.NewClient(ts.URL, a2a.WithProtocolVersion(v))
		task, err := client.SendMessage(context.Background(), &a2a.SendMessageRequest{
			Message:       userMessage("ctx-interop-" + string(v)),
			Configuration: &a2a.SendMessageConfiguration{Blocking: true},
		})
		require.NoError(t, err, v)
		assert.Equal(t, a2a.TaskStateCompleted, task.Status.State, v)
		assert.Equal(t, "hello", a2a.ExtractResponseText(task), v)

		events, err := client.SendMessageStream(context.Background(), &a2a.SendMessageRequest{
			Message: userMessage("ctx-interop-stream-" + string(v)),
		})
		require.NoError(t, err, v)
		var sawTask, sawCompleted bool
		for e := range events {
			sawTask = sawTask || e.Task != nil
			sawCompleted = sawCompleted || (e.StatusUpdate != nil && e.StatusUpdate.Status.State == a2a.TaskStateCompleted)
		}
		assert.True(t, sawTask, v)
		assert.True(t, sawCompleted, v)

		got, err := client.GetTask(context.Background(), task.ID)
		require.NoError(t, err, v)
		assert.Equal(t, task.ID, got.ID, v)
	}

	// Unpinned, the client discovers the card and speaks 1.0.
	client := a2a.NewClient(ts.URL)
	card, err := client.Discover(context.Background())
	require.NoError(t, err)
	assert.Equal(t, a2a.ProtocolVersion10, card.PreferredVersion())
	assert.Equal(t, a2a.ProtocolVersion10, client.ProtocolVersion())
}
