package a2aserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
)

// Conformance gaps from #2101, checked on the wire.

// --- message.taskId continues a task (A2A 1.0 §3.4.2, §3.4.3, §3.1.1) ---

// clientToolConv asks for a client tool on Send, and completes with
// "resumed" once the result comes back.
func clientToolConv() *mockResumableStreamConv {
	return &mockResumableStreamConv{mockStreamConv: mockStreamConv{
		mockConv: mockConv{sendFunc: func(context.Context, any) (SendResult, error) {
			return &mockSendResult{
				hasPending: true, hasPendingClient: true,
				pendingClientTools: []PendingClientToolInfo{{CallID: "call-1", ToolName: "get_location"}},
			}, nil
		}},
		streamFunc: func(context.Context, any) <-chan StreamEvent {
			ch := make(chan StreamEvent, 1)
			ch <- StreamEvent{Kind: EventClientTool, ClientTool: &PendingClientToolInfo{CallID: "call-1", ToolName: "get_location"}}
			close(ch)
			return ch
		},
	}, resumeStreamFn: func(context.Context) <-chan StreamEvent {
		ch := make(chan StreamEvent, 2)
		ch <- StreamEvent{Kind: EventText, Text: "resumed"}
		ch <- StreamEvent{Kind: EventDone}
		close(ch)
		return ch
	}}
}

// toolResultMessage answers call-1 on taskID.
func toolResultMessage(contextID, taskID string) a2a.Message {
	return a2a.Message{
		MessageID: "m2", ContextID: contextID, TaskID: taskID, Role: a2a.RoleUser,
		Parts: []a2a.Part{{
			Data:     map[string]any{"lat": 40.7},
			Metadata: map[string]any{"tool_call_id": "call-1", "tool_result": map[string]any{"lat": 40.7}},
		}},
	}
}

func TestContinuation_ToolResultResumesTheInputRequiredTask(t *testing.T) {
	conv := clientToolConv()
	srv, ts := newTestServer(func(string) (Conversation, error) { return conv, nil })
	defer ts.Close()

	first := decodeTaskResult(t, rawRPCResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-resume")})))
	require.Equal(t, a2a.TaskStateInputRequired, first.Status.State)

	// Only the task id: the context is inferred from the task.
	second := decodeTaskResult(t, rawRPCResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Message: toolResultMessage("", first.ID)})))
	assert.Equal(t, first.ID, second.ID, "the same task continues")
	assert.Equal(t, "ctx-resume", second.ContextID)
	assert.Equal(t, a2a.TaskStateCompleted, second.Status.State)

	page, err := queryTasks(srv.taskStore, TaskQuery{ContextID: "ctx-resume"})
	require.NoError(t, err)
	assert.Equal(t, 1, page.Total, "no second task is created, and none is left input-required")
}

func TestContinuation_StreamResumesTheInputRequiredTask(t *testing.T) {
	conv := clientToolConv()
	_, ts := newTestServer(func(string) (Conversation, error) { return conv, nil })
	defer ts.Close()

	_, first := rawStream(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendStreamingMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-stream-resume")}))
	taskID := first[0]["task"].(map[string]any)["id"].(string)
	last := first[len(first)-1]["statusUpdate"].(map[string]any)
	require.Equal(t, "TASK_STATE_INPUT_REQUIRED", last["status"].(map[string]any)["state"])

	_, second := rawStream(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendStreamingMessage,
		a2a.SendMessageRequest{Message: toolResultMessage("ctx-stream-resume", taskID)}))
	assert.Equal(t, taskID, second[0]["task"].(map[string]any)["id"], "the stream opens with the continued task")
	end := second[len(second)-1]["statusUpdate"].(map[string]any)
	assert.Equal(t, taskID, end["taskId"])
	assert.Equal(t, "TASK_STATE_COMPLETED", end["status"].(map[string]any)["state"])
}

func TestContinuation_StatelessHandlerGetsTheSameTask(t *testing.T) {
	h := &resumableHandler{}
	h.events = []StreamEvent{{Kind: EventClientTool, ClientTool: &PendingClientToolInfo{CallID: "call-1"}}}
	_, ts := statelessServer(t, h)
	defer ts.Close()

	first := decodeTaskResult(t, rawRPCResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-stateless-resume")})))
	require.Equal(t, a2a.TaskStateInputRequired, first.Status.State)

	second := decodeTaskResult(t, rawRPCResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Message: toolResultMessage("", first.ID)})))
	assert.Equal(t, first.ID, second.ID)
	h.mu.Lock()
	defer h.mu.Unlock()
	require.Len(t, h.toolResults, 1)
	assert.Equal(t, first.ID, h.toolResults[0].TaskID, "the handler resumes the task it suspended")
	assert.Equal(t, "ctx-stateless-resume", h.toolResults[0].ContextID)
}

func TestContinuation_Refusals(t *testing.T) {
	srv, ts := newTestServer(func(string) (Conversation, error) { return completingMock(), nil })
	defer ts.Close()

	done := decodeTaskResult(t, rawRPCResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-done")})))
	require.Equal(t, a2a.TaskStateCompleted, done.Status.State)
	_, err := srv.taskStore.Create("waiting", "ctx-waiting")
	require.NoError(t, err)
	require.NoError(t, srv.taskStore.SetState("waiting", a2a.TaskStateWorking, nil))
	require.NoError(t, srv.taskStore.SetState("waiting", a2a.TaskStateInputRequired, nil))

	for _, method := range []string{a2a.MethodV1SendMessage, a2a.MethodV1SendStreamingMessage} {
		msg := userMessage("")
		msg.TaskID = "no-such-task"
		e := rawError(t, rawRPC(t, ts, "1.0", method, a2a.SendMessageRequest{Message: msg}))
		assert.Equal(t, a2a.ErrCodeTaskNotFound, e.Code, "%s: an unknown taskId", method)

		msg.TaskID = done.ID
		e = rawError(t, rawRPC(t, ts, "1.0", method, a2a.SendMessageRequest{Message: msg}))
		assert.Equal(t, a2a.ErrCodeUnsupportedOperation, e.Code, "%s: a terminal task takes no messages", method)

		msg = userMessage("ctx-other")
		msg.TaskID = "waiting"
		e = rawError(t, rawRPC(t, ts, "1.0", method, a2a.SendMessageRequest{Message: msg}))
		assert.Equal(t, a2a.ErrCodeInvalidParams, e.Code, "%s: contextId must match the task's", method)
	}

	task, err := srv.taskStore.Get("waiting")
	require.NoError(t, err)
	assert.Equal(t, a2a.TaskStateInputRequired, task.Status.State, "a refused message leaves the task alone")
}

// A message for a task already running a turn is refused, and its tool
// results must not reach the conversation: the claim comes first.
func TestContinuation_RefusedClaimSubmitsNoToolResults(t *testing.T) {
	for _, method := range []string{a2a.MethodV1SendMessage, a2a.MethodV1SendStreamingMessage} {
		conv := clientToolConv()
		srv, ts := newTestServer(func(string) (Conversation, error) { return conv, nil })

		_, err := srv.taskStore.Create("busy", "ctx-busy")
		require.NoError(t, err)
		require.NoError(t, srv.taskStore.SetState("busy", a2a.TaskStateWorking, nil))

		e := rawError(t, rawRPC(t, ts, "1.0", method, a2a.SendMessageRequest{Message: toolResultMessage("", "busy")}))
		assert.Equal(t, a2a.ErrCodeUnsupportedOperation, e.Code, method)
		conv.mu.Lock()
		assert.Empty(t, conv.toolResults, "%s: a refused message submits nothing", method)
		conv.mu.Unlock()
		task, err := srv.taskStore.Get("busy")
		require.NoError(t, err)
		assert.Equal(t, a2a.TaskStateWorking, task.Status.State, method)
		ts.Close()
	}
}

// failingResumeConv refuses every tool result it is given.
type failingResumeConv struct{ *mockResumableStreamConv }

func (failingResumeConv) SendToolResult(string, any) error { return errors.New("unknown call") }

// When the results cannot be submitted, a continued task goes back to waiting
// for them, with the request that asked for them, rather than staying working.
func TestContinuation_SubmitFailureRestoresTheWaitingTask(t *testing.T) {
	for _, method := range []string{a2a.MethodV1SendMessage, a2a.MethodV1SendStreamingMessage} {
		inner := clientToolConv()
		conv := failingResumeConv{inner}
		srv, ts := newTestServer(func(string) (Conversation, error) { return conv, nil })

		first := decodeTaskResult(t, rawRPCResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
			a2a.SendMessageRequest{Message: userMessage("ctx-restore")})))
		require.Equal(t, a2a.TaskStateInputRequired, first.Status.State)

		e := rawError(t, rawRPC(t, ts, "1.0", method, a2a.SendMessageRequest{Message: toolResultMessage("", first.ID)}))
		assert.Equal(t, a2a.ErrCodeInternal, e.Code, method)
		task, err := srv.taskStore.Get(first.ID)
		require.NoError(t, err)
		assert.Equal(t, a2a.TaskStateInputRequired, task.Status.State, method)
		require.NotNil(t, task.Status.Message, method)
		assert.Equal(t, "call-1", task.Status.Message.Parts[0].Metadata["tool_call_id"], method)
		ts.Close()
	}
}

// getFailsWhileWorking is a store whose Get fails once a task is working —
// after the stream handler has claimed it.
type getFailsWhileWorking struct{ *InMemoryTaskStore }

func (g getFailsWhileWorking) Get(taskID string) (*a2a.Task, error) {
	task, err := g.InMemoryTaskStore.Get(taskID)
	if err == nil && task.Status.State == a2a.TaskStateWorking {
		return nil, errors.New("store unavailable")
	}
	return task, err
}

// A stream that cannot start after claiming its task gives the task back,
// rather than leaving it working with no turn behind it.
func TestContinuation_StreamReleasesTheTaskWhenItCannotStart(t *testing.T) {
	inner := NewInMemoryTaskStore()
	_, err := inner.Create("waiting", "ctx-release")
	require.NoError(t, err)
	require.NoError(t, inner.SetState("waiting", a2a.TaskStateWorking, nil))
	asked := &a2a.Message{MessageID: "q", Role: a2a.RoleAgent, Parts: []a2a.Part{{Text: serverTextPtr("which city?")}}}
	require.NoError(t, inner.SetState("waiting", a2a.TaskStateInputRequired, asked))

	conv := streamConv(StreamEvent{Kind: EventText, Text: "ok"})
	_, ts := newTestServer(func(string) (Conversation, error) { return conv, nil },
		WithTaskStore(getFailsWhileWorking{inner}))
	defer ts.Close()

	msg := userMessage("")
	msg.TaskID = "waiting"
	e := rawError(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendStreamingMessage, a2a.SendMessageRequest{Message: msg}))
	assert.Equal(t, a2a.ErrCodeInternal, e.Code)

	task, err := inner.Get("waiting")
	require.NoError(t, err)
	assert.Equal(t, a2a.TaskStateInputRequired, task.Status.State, "the claim is undone")
	require.NotNil(t, task.Status.Message)
	assert.Equal(t, "q", task.Status.Message.MessageID, "with the request it was waiting on")
}

func TestContinuation_AnotherCallersTaskIsNotFound(t *testing.T) {
	conv := clientToolConv()
	_, ts := newTestServer(func(string) (Conversation, error) { return conv, nil },
		WithTaskOwner(ownerFromHeader))
	defer ts.Close()

	first := decodeTaskResult(t, as(t, ts, "alice", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-alice")}).Result)
	require.Equal(t, a2a.TaskStateInputRequired, first.Status.State)

	resp := as(t, ts, "mallory", a2a.MethodV1SendMessage, a2a.SendMessageRequest{Message: toolResultMessage("", first.ID)})
	requireCode(t, resp, a2a.ErrCodeTaskNotFound, "continuing another caller's task")
}

// A turn that ends must not unregister the cancel func of the turn that
// continues its task: a continuation can claim the task, and register, before
// the ending turn's deferred unregister runs.
func TestCancelRegistration_EndingTurnLeavesTheNextTurnsCancel(t *testing.T) {
	srv, ts := newTestServer(nopOpener)
	defer ts.Close()

	first := srv.registerCancel("t", func() {})
	reached := false
	second := srv.registerCancel("t", func() { reached = true })
	srv.unregisterCancel("t", first) // turn N's deferred cleanup, running late
	srv.cancelLocal("t")
	assert.True(t, reached, "CancelTask must still reach the continued turn")

	srv.unregisterCancel("t", second)
	srv.cancelsMu.Lock()
	defer srv.cancelsMu.Unlock()
	assert.Empty(t, srv.cancels, "a turn's own registration is removed")
}

// --- Advertised capabilities match what is served (A2A 1.0 §3.3.4) ---

func TestCapabilities_ServedCardMatchesTheServer(t *testing.T) {
	_, ts := newTestServer(nopOpener, WithCard(&a2a.AgentCard{
		Name:         "a",
		Capabilities: a2a.AgentCapabilities{PushNotifications: true},
	}))
	defer ts.Close()

	for _, version := range []string{"1.0", ""} {
		req, err := http.NewRequest(http.MethodGet, ts.URL+a2a.AgentCardPath, http.NoBody)
		require.NoError(t, err)
		if version != "" {
			req.Header.Set(a2a.HeaderVersion, version)
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		var card struct {
			Capabilities map[string]any `json:"capabilities"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&card))
		resp.Body.Close()
		assert.Equal(t, true, card.Capabilities["streaming"], "version %q: streaming is served, so it is declared", version)
		assert.NotEqual(t, true, card.Capabilities["pushNotifications"],
			"version %q: every push method fails, so push is not declared", version)
	}
}

func TestCapabilities_ExtendedCardErrorFollowsTheCard(t *testing.T) {
	_, plain := newTestServer(nopOpener)
	defer plain.Close()
	e := rawError(t, rawRPC(t, plain, "1.0", a2a.MethodV1GetExtendedAgentCard, map[string]any{}))
	assert.Equal(t, a2a.ErrCodeUnsupportedOperation, e.Code, "not declared: UnsupportedOperation")

	_, declared := newTestServer(nopOpener, WithCard(&a2a.AgentCard{
		Name: "a", Capabilities: a2a.AgentCapabilities{ExtendedAgentCard: true},
	}))
	defer declared.Close()
	e = rawError(t, rawRPC(t, declared, "1.0", a2a.MethodV1GetExtendedAgentCard, map[string]any{}))
	assert.Equal(t, a2a.ErrCodeExtendedAgentCardNotConfigured, e.Code, "declared but absent: NotConfigured")
}

// --- Closing a stream does not touch the task (A2A 1.0 §3.5.2) ---

func TestStream_CallerDisconnectLeavesTheTaskRunning(t *testing.T) {
	release := make(chan struct{})
	conv := &mockStreamConv{streamFunc: func(ctx context.Context, _ any) <-chan StreamEvent {
		ch := make(chan StreamEvent)
		go func() {
			defer close(ch)
			select {
			case ch <- StreamEvent{Kind: EventText, Text: "first"}:
			case <-ctx.Done():
				return
			}
			select {
			case <-release:
			case <-ctx.Done():
				return
			}
			select {
			case ch <- StreamEvent{Kind: EventText, Text: " second"}:
			case <-ctx.Done():
			}
		}()
		return ch
	}}
	srv, ts := newTestServer(func(string) (Conversation, error) { return conv, nil })
	defer ts.Close()

	body, err := json.Marshal(a2a.JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: a2a.MethodV1SendStreamingMessage,
		Params: mustMarshal(t, a2a.SendMessageRequest{Message: userMessage("ctx-drop")})})
	require.NoError(t, err)
	ctx, disconnect := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/a2a", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set(a2a.HeaderVersion, "1.0")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	// Read the opening Task, then drop the connection.
	scanner := bufio.NewScanner(resp.Body)
	var taskID string
	for taskID == "" && scanner.Scan() {
		line, ok := strings.CutPrefix(scanner.Text(), "data: ")
		if !ok {
			continue
		}
		var env struct {
			Result a2a.StreamResponse `json:"result"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &env))
		if env.Result.Task != nil {
			taskID = env.Result.Task.ID
		}
	}
	require.NotEmpty(t, taskID)
	disconnect()
	resp.Body.Close()

	// The turn outlives its first stream: it finishes and records its result.
	time.Sleep(20 * time.Millisecond)
	close(release)
	require.Eventually(t, func() bool {
		task, getErr := srv.taskStore.Get(taskID)
		return getErr == nil && task.Status.State.IsTerminal()
	}, 2*time.Second, 5*time.Millisecond)
	task, err := srv.taskStore.Get(taskID)
	require.NoError(t, err)
	assert.Equal(t, a2a.TaskStateCompleted, task.Status.State, "closing a stream must not cancel its task")
	require.Len(t, task.Artifacts, 1)
	assert.Equal(t, "first second", *task.Artifacts[0].Parts[0].Text)
}

// --- Tool results may carry their value as the part's data ---

func TestExtractToolResults_DataPartCarriesTheResult(t *testing.T) {
	results := extractToolResults([]a2a.Part{
		{Data: map[string]any{"lat": 1.0}, Metadata: map[string]any{"tool_call_id": "a"}},
		{DataValue: []any{"x"}, Metadata: map[string]any{"tool_call_id": "b"}},
		{Data: map[string]any{"ignored": true}, Metadata: map[string]any{"tool_call_id": "c", "tool_result": "meta"}},
	})
	require.Len(t, results, 3)
	assert.Equal(t, map[string]any{"lat": 1.0}, results[0].Result)
	assert.Equal(t, []any{"x"}, results[1].Result)
	assert.Equal(t, "meta", results[2].Result, "an explicit tool_result wins")
}

func rawRPCResult(t *testing.T, resp *http.Response) json.RawMessage {
	t.Helper()
	defer resp.Body.Close()
	var out a2a.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Nil(t, out.Error, "unexpected error: %+v", out.Error)
	return out.Result
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}
