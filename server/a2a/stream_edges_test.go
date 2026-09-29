package a2aserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
)

// plainWriter is a ResponseWriter that cannot flush, like a middleware
// wrapper that forgets to pass http.Flusher through.
type plainWriter struct {
	header http.Header
	body   bytes.Buffer
}

func (w *plainWriter) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}
func (w *plainWriter) Write(b []byte) (int, error) { return w.body.Write(b) }
func (w *plainWriter) WriteHeader(int)             {}

// callDirect runs one JSON-RPC request through handleRPC on w.
func callDirect(t *testing.T, srv *Server, w http.ResponseWriter, method string, params any) {
	t.Helper()
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)
	body, err := json.Marshal(a2a.JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: paramsJSON})
	require.NoError(t, err)
	r := httptest.NewRequest(http.MethodPost, "/a2a", bytes.NewReader(body))
	srv.handleRPC(w, r)
}

func decodeError(t *testing.T, body []byte) *a2a.JSONRPCError {
	t.Helper()
	var resp a2a.JSONRPCResponse
	require.NoError(t, json.Unmarshal(body, &resp))
	require.NotNil(t, resp.Error, "expected an error, got %s", body)
	return resp.Error
}

func TestStream_RefusedWithoutFlusher(t *testing.T) {
	srv := NewServer(func(string) (Conversation, error) { return streamConv(), nil })
	defer func() { _ = srv.Shutdown(context.Background()) }()

	for _, method := range []string{a2a.MethodV1SendStreamingMessage, a2a.MethodV1SubscribeToTask} {
		_, err := srv.taskStore.Create("t-"+method, "c")
		require.NoError(t, err)
		w := &plainWriter{}
		var params any = a2a.SendMessageRequest{Message: userMessage("ctx-noflush")}
		if method == a2a.MethodV1SubscribeToTask {
			params = a2a.SubscribeTaskRequest{ID: "t-" + method}
		}
		callDirect(t, srv, w, method, params)
		assert.Equal(t, a2a.ErrCodeUnsupportedOperation, decodeError(t, w.body.Bytes()).Code, method)
	}
}

// failingStore fails the calls a test chooses, and is otherwise in-memory.
type failingStore struct {
	*InMemoryTaskStore
	failCreate  bool
	failGet     bool
	failStateTo a2a.TaskState
}

func (f *failingStore) Create(taskID, contextID string) (*a2a.Task, error) {
	if f.failCreate {
		return nil, errors.New("store down")
	}
	return f.InMemoryTaskStore.Create(taskID, contextID)
}

func (f *failingStore) Get(taskID string) (*a2a.Task, error) {
	if f.failGet {
		return nil, ErrTaskNotFound
	}
	return f.InMemoryTaskStore.Get(taskID)
}

func (f *failingStore) SetState(taskID string, state a2a.TaskState, msg *a2a.Message) error {
	if state == f.failStateTo {
		return ErrInvalidTransition
	}
	return f.InMemoryTaskStore.SetState(taskID, state, msg)
}

func TestStream_TaskCreateFailureIsAnInternalError(t *testing.T) {
	store := &failingStore{InMemoryTaskStore: NewInMemoryTaskStore(), failCreate: true}
	_, ts := newTestServer(func(string) (Conversation, error) { return streamConv(), nil }, WithTaskStore(store))
	defer ts.Close()

	e := rawError(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendStreamingMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-create-fail")}))
	assert.Equal(t, a2a.ErrCodeInternal, e.Code)
	assert.NotContains(t, e.Message, "store down")
}

// When the final state cannot be written — CancelTask won the race — the
// streamer is told the state the task really has, not the one it tried.
func TestStream_ReportsTheStoredStateWhenFinishLosesTheRace(t *testing.T) {
	store := &failingStore{InMemoryTaskStore: NewInMemoryTaskStore(), failStateTo: a2a.TaskStateCompleted}
	_, ts := newTestServer(func(string) (Conversation, error) {
		return streamConv(StreamEvent{Kind: EventText, Text: "x"}), nil
	}, WithTaskStore(store))
	defer ts.Close()

	_, events := rawStream(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendStreamingMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-race")}))
	last := events[len(events)-1]["statusUpdate"].(map[string]any)
	assert.Equal(t, "TASK_STATE_WORKING", last["status"].(map[string]any)["state"])
}

func TestStream_InvalidParams(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) { return streamConv(), nil })
	defer ts.Close()

	for _, method := range []string{a2a.MethodV1SendStreamingMessage, a2a.MethodV1SubscribeToTask} {
		resp, err := http.Post(ts.URL+"/a2a", "application/json",
			bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"`+method+`","params":"nope"}`)))
		require.NoError(t, err)
		assert.Equal(t, a2a.ErrCodeInvalidParams, rawError(t, resp).Code, method)
	}
}

func TestSubscribe_InterruptedTaskEndsAfterTheSnapshot(t *testing.T) {
	srv, ts := newTestServer(nopOpener)
	defer ts.Close()

	_, err := srv.taskStore.Create("waiting", "c")
	require.NoError(t, err)
	require.NoError(t, srv.taskStore.SetState("waiting", a2a.TaskStateWorking, nil))
	require.NoError(t, srv.taskStore.SetState("waiting", a2a.TaskStateInputRequired, nil))

	_, events := rawStream(t, rawRPC(t, ts, "1.0", a2a.MethodV1SubscribeToTask, a2a.SubscribeTaskRequest{ID: "waiting"}))
	require.Len(t, events, 1, "an interrupted task gets its snapshot and nothing to wait for")
	assert.Equal(t, "TASK_STATE_INPUT_REQUIRED",
		events[0]["task"].(map[string]any)["status"].(map[string]any)["state"])
}

func TestSubscribe_TooManySubscribers(t *testing.T) {
	srv, ts := newTestServer(nopOpener)
	defer ts.Close()

	_, err := srv.taskStore.Create("busy", "c")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for i := 0; i < maxSubscribers; i++ {
		_, subErr := srv.events.Subscribe(ctx, "busy")
		require.NoError(t, subErr)
	}

	e := rawError(t, rawRPC(t, ts, "1.0", a2a.MethodV1SubscribeToTask, a2a.SubscribeTaskRequest{ID: "busy"}))
	assert.Equal(t, a2a.ErrCodeInternal, e.Code)
}

func TestRelayEvents_StopsWhenTheCallerLeaves(t *testing.T) {
	rec := httptest.NewRecorder()
	out := &streamWriter{w: rec, flusher: rec, id: 1, v: a2a.ProtocolVersion10}
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan TaskEvent)
	done := make(chan struct{})
	go func() {
		relayEvents(ctx, out, events)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("relayEvents did not return when its context ended")
	}

	closed := make(chan TaskEvent)
	close(closed)
	relayEvents(context.Background(), out, closed)
}

func TestStateless_StreamToolResults(t *testing.T) {
	toolPart := a2a.Part{Metadata: map[string]any{"tool_call_id": "call-1", "tool_result": "42"}}

	// A handler without the client-tool half refuses them.
	_, plain := statelessServer(t, &recordingHandler{})
	defer plain.Close()
	e := rawError(t, rawRPC(t, plain, "1.0", a2a.MethodV1SendStreamingMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-tools", toolPart)}))
	assert.Equal(t, a2a.ErrCodeUnsupportedOperation, e.Code)

	// One that has it resumes, and the stream carries the result.
	h := &resumableHandler{}
	_, ts := statelessServer(t, h)
	defer ts.Close()
	_, events := rawStream(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendStreamingMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-tools", toolPart)}))
	last := events[len(events)-1]["statusUpdate"].(map[string]any)
	assert.Equal(t, "TASK_STATE_COMPLETED", last["status"].(map[string]any)["state"])
	h.mu.Lock()
	defer h.mu.Unlock()
	require.Len(t, h.toolResults, 1)
	assert.Equal(t, "call-1", h.toolResults[0].Results[0].CallID)
}

func TestStream_ConversationToolResultsNeedAResumableConversation(t *testing.T) {
	toolPart := a2a.Part{Metadata: map[string]any{"tool_call_id": "call-1", "tool_result": "42"}}
	_, ts := newTestServer(func(string) (Conversation, error) { return streamConv(), nil })
	defer ts.Close()

	e := rawError(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendStreamingMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-conv-tools", toolPart)}))
	assert.Equal(t, a2a.ErrCodeUnsupportedOperation, e.Code)
}

func TestStream_OpenerFailureIsAnInternalError(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) { return nil, errors.New("no conversation") })
	defer ts.Close()

	e := rawError(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendStreamingMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-open-fail")}))
	assert.Equal(t, a2a.ErrCodeInternal, e.Code)
}

// Tool calls stay hidden, a media event without media is skipped, and a client
// tool interrupts the task with the call it needs.
func TestStream_MediaAndToolCallEvents(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) {
		return streamConv(
			StreamEvent{Kind: EventToolCall},
			StreamEvent{Kind: EventMedia},
			StreamEvent{Kind: EventClientTool, ClientTool: &PendingClientToolInfo{CallID: "c1", ToolName: "locate"}},
		), nil
	})
	defer ts.Close()

	_, events := rawStream(t, rawRPC(t, ts, "", a2a.MethodV03SendStreamingMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-mixed")}))
	last := events[len(events)-1]
	assert.Equal(t, "input-required", last["status"].(map[string]any)["state"])
	part := last["status"].(map[string]any)["message"].(map[string]any)["parts"].([]any)[0].(map[string]any)
	assert.Equal(t, "c1", part["metadata"].(map[string]any)["tool_call_id"])
}

// A streaming caller that disconnects ends its turn. The task must say so —
// not sit "working" forever — and a subscriber must get the event that ends
// its stream.
func TestStream_DisconnectCancelsTheTaskAndReleasesSubscribers(t *testing.T) {
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

	params, _ := json.Marshal(a2a.SendMessageRequest{Message: userMessage("ctx-leave")})
	body, _ := json.Marshal(a2a.JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: a2a.MethodV1SendStreamingMessage, Params: params})
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/a2a", bytes.NewReader(body))
	req.Header.Set(a2a.HeaderVersion, "1.0")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
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

	subscribed := make(chan []map[string]any)
	go func() {
		_, events := rawStream(t, rawRPC(t, ts, "1.0", a2a.MethodV1SubscribeToTask, a2a.SubscribeTaskRequest{ID: taskID}))
		subscribed <- events
	}()
	require.Eventually(t, func() bool {
		local := srv.events.(*localTaskEvents)
		local.mu.Lock()
		defer local.mu.Unlock()
		return len(local.subs[taskID]) == 1
	}, time.Second, 5*time.Millisecond)

	cancel()

	select {
	case events := <-subscribed:
		last := events[len(events)-1]["statusUpdate"].(map[string]any)
		assert.Equal(t, "TASK_STATE_CANCELED", last["status"].(map[string]any)["state"])
	case <-time.After(3 * time.Second):
		t.Fatal("the subscriber was left waiting after the streaming caller left")
	}
	task, err := srv.taskStore.Get(taskID)
	require.NoError(t, err)
	assert.Equal(t, a2a.TaskStateCanceled, task.Status.State)
}
