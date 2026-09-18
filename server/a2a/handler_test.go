package a2aserver

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
)

// Stateless mode: the server holds nothing, and each message goes to the
// embedder's handler with the context of the request it arrived on. These tests
// are mostly about the two things a handler cannot verify for itself — that the
// server keeps no state, and that a client sees the same protocol either way.

// recordingHandler answers with a scripted stream and remembers what it was
// asked, including what was on the context.
type recordingHandler struct {
	mu       sync.Mutex
	calls    int
	requests []MessageRequest
	values   []any
	events   []StreamEvent

	toolCalls   int
	toolResults []ToolResultRequest
}

func (h *recordingHandler) Handle(ctx context.Context, req MessageRequest) <-chan StreamEvent {
	h.mu.Lock()
	h.calls++
	h.requests = append(h.requests, req)
	h.values = append(h.values, ctx.Value(callerValueKey{}))
	events := h.events
	h.mu.Unlock()

	ch := make(chan StreamEvent, len(events)+1)
	for _, e := range events {
		ch <- e
	}
	ch <- StreamEvent{Kind: EventDone}
	close(ch)
	return ch
}

func (h *recordingHandler) snapshot() (int, []MessageRequest, []any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls, append([]MessageRequest{}, h.requests...), append([]any{}, h.values...)
}

// resumableHandler adds the client-tool half.
type resumableHandler struct{ recordingHandler }

func (h *resumableHandler) HandleToolResult(_ context.Context, req ToolResultRequest) <-chan StreamEvent {
	h.mu.Lock()
	h.toolCalls++
	h.toolResults = append(h.toolResults, req)
	h.mu.Unlock()

	ch := make(chan StreamEvent, 2)
	ch <- StreamEvent{Kind: EventText, Text: "resumed"}
	ch <- StreamEvent{Kind: EventDone}
	close(ch)
	return ch
}

func statelessServer(t *testing.T, h MessageHandler) (*Server, *httptest.Server) {
	t.Helper()
	srv := NewStatelessServer(h)
	return srv, httptest.NewServer(srv.Handler())
}

func TestStateless_SendReachesTheHandlerAndCompletes(t *testing.T) {
	h := &recordingHandler{events: []StreamEvent{{Kind: EventText, Text: "hello back"}}}
	_, ts := statelessServer(t, h)
	defer ts.Close()

	task := a2aSendMessage(t, ts, "ctx-stateless", "hello")

	calls, reqs, _ := h.snapshot()
	require.Equal(t, 1, calls)
	assert.Equal(t, "ctx-stateless", reqs[0].ContextID)
	assert.NotEmpty(t, reqs[0].TaskID, "the handler must be told which task it is serving")
	assert.Equal(t, a2a.TaskStateCompleted, task.Status.State)
	require.NotEmpty(t, task.Artifacts)
	require.NotNil(t, task.Artifacts[0].Parts[0].Text)
	assert.Equal(t, "hello back", *task.Artifacts[0].Parts[0].Text)
}

// The point of the mode: nothing is kept, so the same context id is a fresh
// call every time and the embedder decides what, if anything, it shares.
func TestStateless_HoldsNothingBetweenRequests(t *testing.T) {
	h := &recordingHandler{events: []StreamEvent{{Kind: EventText, Text: "ok"}}}
	srv, ts := statelessServer(t, h)
	defer ts.Close()

	a2aSendMessage(t, ts, "same-ctx", "one")
	a2aSendMessage(t, ts, "same-ctx", "two")

	calls, reqs, _ := h.snapshot()
	assert.Equal(t, 2, calls, "a repeated context id must reach the handler again, not a cache")
	assert.Equal(t, "same-ctx", reqs[0].ContextID)
	assert.NotEqual(t, reqs[0].TaskID, reqs[1].TaskID, "each message is its own task")

	srv.convsMu.RLock()
	defer srv.convsMu.RUnlock()
	assert.Empty(t, srv.convs, "stateless mode must not populate the conversation cache")
}

// The gap ConversationOpener could not close: the handler sees the request, so
// caller identity put on the context by the embedder's middleware is readable
// where the work is decided.
func TestStateless_HandlerSeesTheRequestContext(t *testing.T) {
	h := &recordingHandler{events: []StreamEvent{{Kind: EventText, Text: "ok"}}}
	srv := NewStatelessServer(h)
	ts := httptest.NewServer(withCallerValue(srv.Handler(), "user-42"))
	defer ts.Close()

	a2aSendMessage(t, ts, "ctx-identity", "hello")

	_, _, values := h.snapshot()
	require.Len(t, values, 1)
	assert.Equal(t, "user-42", values[0],
		"the handler could not read what the caller's middleware put on the request")
}

func TestStateless_StreamReachesTheHandler(t *testing.T) {
	h := &recordingHandler{events: []StreamEvent{
		{Kind: EventText, Text: "one"},
		{Kind: EventText, Text: "two"},
	}}
	srv, ts := statelessServer(t, h)
	defer ts.Close()

	events := readSSEEvents(t, ts, a2a.MethodSendStreamingMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-stateless-stream",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("hi")}},
		},
	})

	calls, _, _ := h.snapshot()
	assert.Equal(t, 1, calls)

	task, err := srv.taskStore.Get(taskIDFromEvents(t, events))
	require.NoError(t, err)
	assert.Equal(t, a2a.TaskStateCompleted, task.Status.State)
	assert.Len(t, task.Artifacts, 2, "streamed artifacts are stored in this mode too")
}

// An error event fails the task rather than completing it emptily.
func TestStateless_ErrorEventFailsTheTask(t *testing.T) {
	h := &recordingHandler{events: []StreamEvent{
		{Kind: EventText, Text: "partial"},
		{Error: assertError("upstream exploded")},
	}}
	_, ts := statelessServer(t, h)
	defer ts.Close()

	task := a2aSendMessage(t, ts, "ctx-stateless-err", "hello")

	assert.Equal(t, a2a.TaskStateFailed, task.Status.State)
	assert.Empty(t, task.Artifacts, "a failed turn has no result")
}

// EventPending is why HasPendingTools is not inferred: a handler paused on
// something the server cannot see must be able to say so, or its turn is
// reported as completed while it waits.
func TestStateless_PendingEventHoldsTheTaskOpen(t *testing.T) {
	h := &recordingHandler{events: []StreamEvent{
		{Kind: EventPending, Text: "waiting for human approval"},
	}}
	_, ts := statelessServer(t, h)
	defer ts.Close()

	task := a2aSendMessage(t, ts, "ctx-stateless-pending", "do something risky")

	assert.Equal(t, a2a.TaskStateInputRequired, task.Status.State,
		"a paused turn reported as completed is a control that did not run reporting clean")
}

func TestStateless_ClientToolAsksTheCallerToAct(t *testing.T) {
	h := &resumableHandler{recordingHandler{events: []StreamEvent{
		{Kind: EventClientTool, ClientTool: &PendingClientToolInfo{
			CallID: "call-1", ToolName: "get_location",
		}},
	}}}
	_, ts := statelessServer(t, h)
	defer ts.Close()

	task := a2aSendMessage(t, ts, "ctx-stateless-tool", "where am i")

	require.Equal(t, a2a.TaskStateInputRequired, task.Status.State)
	require.NotNil(t, task.Status.Message)
	require.NotEmpty(t, task.Status.Message.Parts)
	assert.Equal(t, "call-1", task.Status.Message.Parts[0].Metadata["tool_call_id"])
}

func TestStateless_ToolResultsReachTheHandler(t *testing.T) {
	h := &resumableHandler{recordingHandler{events: []StreamEvent{{Kind: EventText, Text: "ok"}}}}
	_, ts := statelessServer(t, h)
	defer ts.Close()

	task := a2aRPCRequestTask(t, ts, a2a.MethodSendMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-stateless-resume",
			Role:      a2a.RoleUser,
			Parts: []a2a.Part{{
				Metadata: map[string]any{
					"tool_call_id": "call-1",
					"tool_result":  map[string]any{"lat": 40.7},
				},
			}},
		},
		Configuration: &a2a.SendMessageConfiguration{Blocking: true},
	})

	h.mu.Lock()
	toolCalls, results := h.toolCalls, append([]ToolResultRequest{}, h.toolResults...)
	h.mu.Unlock()

	require.Equal(t, 1, toolCalls, "the tool result never reached the handler")
	require.Len(t, results[0].Results, 1)
	assert.Equal(t, "call-1", results[0].Results[0].CallID)
	assert.Equal(t, a2a.TaskStateCompleted, task.Status.State)
}

// A handler that never asked for client tools is not pretended to be resumable:
// the server holds nothing to resume, so it says so instead of inventing a turn.
func TestStateless_ToolResultsRefusedWithoutAToolResultHandler(t *testing.T) {
	h := &recordingHandler{events: []StreamEvent{{Kind: EventText, Text: "ok"}}}
	_, ts := statelessServer(t, h)
	defer ts.Close()

	resp := a2aRPCRequest(t, ts, a2a.MethodSendMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-stateless-noresume",
			Role:      a2a.RoleUser,
			Parts: []a2a.Part{{
				Metadata: map[string]any{"tool_call_id": "call-1", "tool_result": "x"},
			}},
		},
	})

	require.NotNil(t, resp.Error, "a server with nothing to resume must refuse, not fabricate")
	calls, _, _ := h.snapshot()
	assert.Zero(t, calls, "the message must not be delivered as if it were a fresh turn")
}

// The conversation-backed mode is untouched: same constructor, same pooling.
func TestStateless_OpenerModeStillPools(t *testing.T) {
	var opened int
	mock := completingMock()
	srv, ts := newTestServer(func(string) (Conversation, error) {
		opened++
		return mock, nil
	})
	defer ts.Close()

	a2aSendMessage(t, ts, "pooled-ctx", "one")
	a2aSendMessage(t, ts, "pooled-ctx", "two")

	assert.Equal(t, 1, opened, "the opener mode must still reuse a conversation per context id")
	srv.convsMu.RLock()
	defer srv.convsMu.RUnlock()
	assert.Len(t, srv.convs, 1)
}
