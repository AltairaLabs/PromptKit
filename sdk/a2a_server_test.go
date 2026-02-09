package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// --- mock conversation for A2A server tests ---

type mockA2AConv struct {
	sendFunc func(ctx context.Context, message any, opts ...SendOption) (*Response, error)
	closed   atomic.Bool
}

func (m *mockA2AConv) Send(ctx context.Context, message any, opts ...SendOption) (*Response, error) {
	return m.sendFunc(ctx, message, opts...)
}

func (m *mockA2AConv) Close() error {
	m.closed.Store(true)
	return nil
}

// --- helpers ---

func serverTextPtr(s string) *string { return &s }

func newA2ATestServer(opener A2AConversationOpener, opts ...A2AServerOption) (*A2AServer, *httptest.Server) {
	srv := NewA2AServer(opener, opts...)
	ts := httptest.NewServer(srv.Handler())
	return srv, ts
}

func a2aRPCRequest(t *testing.T, ts *httptest.Server, method string, params any) *a2a.JSONRPCResponse {
	t.Helper()
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	body, err := json.Marshal(a2a.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  method,
		Params:  paramsJSON,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	resp, err := http.Post(ts.URL+"/a2a", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /a2a: %v", err)
	}
	defer resp.Body.Close()

	var rpcResp a2a.JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return &rpcResp
}

func a2aRPCRequestTask(t *testing.T, ts *httptest.Server, method string, params any) *a2a.Task {
	t.Helper()
	resp := a2aRPCRequest(t, ts, method, params)
	if resp.Error != nil {
		t.Fatalf("unexpected RPC error: %d %s", resp.Error.Code, resp.Error.Message)
	}
	var task a2a.Task
	if err := json.Unmarshal(resp.Result, &task); err != nil {
		t.Fatalf("unmarshal task: %v", err)
	}
	return &task
}

func a2aSendMessage(t *testing.T, ts *httptest.Server, contextID, text string) *a2a.Task {
	t.Helper()
	return a2aRPCRequestTask(t, ts, a2a.MethodSendMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: contextID,
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr(text)}},
		},
		Configuration: &a2a.SendMessageConfiguration{Blocking: true},
	})
}

func nopA2AOpener(string) (a2aConv, error) {
	return nil, errors.New("should not be called")
}

func completingA2AMock() *mockA2AConv {
	return &mockA2AConv{
		sendFunc: func(_ context.Context, _ any, _ ...SendOption) (*Response, error) {
			return &Response{
				message: &types.Message{
					Role:  "assistant",
					Parts: []types.ContentPart{types.NewTextPart("ok")},
				},
			}, nil
		},
	}
}

// --- tests ---

func TestA2AServer_AgentCardDiscovery(t *testing.T) {
	card := a2a.AgentCard{
		Name:        "test-agent",
		Description: "A test agent",
		Version:     "1.0",
	}
	_, ts := newA2ATestServer(nopA2AOpener, WithA2ACard(&card))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.well-known/agent.json")
	if err != nil {
		t.Fatalf("GET agent.json: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q, want application/json", ct)
	}

	var got a2a.AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Name != card.Name || got.Description != card.Description || got.Version != card.Version {
		t.Fatalf("got %+v, want %+v", got, card)
	}
}

func TestA2AServer_SendMessage_Completed(t *testing.T) {
	replyText := "Hello from the agent"
	mock := &mockA2AConv{
		sendFunc: func(_ context.Context, _ any, _ ...SendOption) (*Response, error) {
			return &Response{
				message: &types.Message{
					Role:  "assistant",
					Parts: []types.ContentPart{types.NewTextPart(replyText)},
				},
			}, nil
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	task := a2aSendMessage(t, ts, "ctx-1", "Hello")

	if task.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("state = %q, want completed", task.Status.State)
	}
	if len(task.Artifacts) == 0 {
		t.Fatal("expected artifacts")
	}
	if task.Artifacts[0].Parts[0].Text == nil || *task.Artifacts[0].Parts[0].Text != replyText {
		t.Fatalf("artifact text = %v, want %q", task.Artifacts[0].Parts[0].Text, replyText)
	}
}

func TestA2AServer_SendMessage_Failed(t *testing.T) {
	mock := &mockA2AConv{
		sendFunc: func(_ context.Context, _ any, _ ...SendOption) (*Response, error) {
			return nil, errors.New("provider error")
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	task := a2aSendMessage(t, ts, "ctx-fail", "Hello")

	if task.Status.State != a2a.TaskStateFailed {
		t.Fatalf("state = %q, want failed", task.Status.State)
	}
	if task.Status.Message == nil {
		t.Fatal("expected status message on failure")
	}
}

func TestA2AServer_SendMessage_InputRequired(t *testing.T) {
	mock := &mockA2AConv{
		sendFunc: func(_ context.Context, _ any, _ ...SendOption) (*Response, error) {
			return &Response{
				message:      &types.Message{Role: "assistant"},
				pendingTools: []PendingTool{{ID: "tool-1", Name: "approve"}},
			}, nil
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	task := a2aSendMessage(t, ts, "ctx-tools", "Hello")

	if task.Status.State != a2a.TaskStateInputRequired {
		t.Fatalf("state = %q, want input_required", task.Status.State)
	}
}

func TestA2AServer_SendMessage_Multimodal(t *testing.T) {
	mock := &mockA2AConv{
		sendFunc: func(_ context.Context, message any, _ ...SendOption) (*Response, error) {
			msg, ok := message.(*types.Message)
			if !ok {
				return nil, errors.New("expected *types.Message")
			}
			if len(msg.Parts) != 2 {
				return nil, errors.New("expected 2 parts")
			}
			if msg.Parts[0].Type != types.ContentTypeText {
				return nil, errors.New("expected text part first")
			}
			if msg.Parts[1].Type != types.ContentTypeImage {
				return nil, errors.New("expected image part second")
			}
			return &Response{
				message: &types.Message{
					Role:  "assistant",
					Parts: []types.ContentPart{types.NewTextPart("got it")},
				},
			}, nil
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	imgURL := "https://example.com/image.png"
	task := a2aRPCRequestTask(t, ts, a2a.MethodSendMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-multi",
			Role:      a2a.RoleUser,
			Parts: []a2a.Part{
				{Text: serverTextPtr("Look at this image")},
				{URL: &imgURL, MediaType: "image/png"},
			},
		},
		Configuration: &a2a.SendMessageConfiguration{Blocking: true},
	})

	if task.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("state = %q, want completed", task.Status.State)
	}
}

func TestA2AServer_SendMessage_OpenerError(t *testing.T) {
	_, ts := newA2ATestServer(func(string) (a2aConv, error) {
		return nil, errors.New("opener failed")
	})
	defer ts.Close()

	resp := a2aRPCRequest(t, ts, a2a.MethodSendMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-err",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("Hello")}},
		},
	})
	if resp.Error == nil {
		t.Fatal("expected error when opener fails")
	}
	if resp.Error.Code != -32000 {
		t.Fatalf("error code = %d, want -32000", resp.Error.Code)
	}
}

func TestA2AServer_SendMessage_NoContextID(t *testing.T) {
	mock := completingA2AMock()
	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	task := a2aRPCRequestTask(t, ts, a2a.MethodSendMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			Role:  a2a.RoleUser,
			Parts: []a2a.Part{{Text: serverTextPtr("Hello")}},
		},
		Configuration: &a2a.SendMessageConfiguration{Blocking: true},
	})

	if task.ContextID == "" {
		t.Fatal("expected server to generate a context ID")
	}
	if task.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("state = %q, want completed", task.Status.State)
	}
}

func TestA2AServer_GetTask(t *testing.T) {
	mock := completingA2AMock()
	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	task := a2aSendMessage(t, ts, "ctx-get", "Hello")

	got := a2aRPCRequestTask(t, ts, a2a.MethodGetTask, a2a.GetTaskRequest{ID: task.ID})

	if got.ID != task.ID {
		t.Fatalf("got task ID %q, want %q", got.ID, task.ID)
	}
	if got.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("state = %q, want completed", got.Status.State)
	}
}

func TestA2AServer_GetTask_NotFound(t *testing.T) {
	_, ts := newA2ATestServer(nopA2AOpener)
	defer ts.Close()

	resp := a2aRPCRequest(t, ts, a2a.MethodGetTask, a2a.GetTaskRequest{ID: "nonexistent"})
	if resp.Error == nil {
		t.Fatal("expected error for nonexistent task")
	}
	if resp.Error.Code != -32001 {
		t.Fatalf("error code = %d, want -32001", resp.Error.Code)
	}
}

func TestA2AServer_CancelTask(t *testing.T) {
	sendStarted := make(chan struct{})
	mock := &mockA2AConv{
		sendFunc: func(ctx context.Context, _ any, _ ...SendOption) (*Response, error) {
			close(sendStarted)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	resp := a2aRPCRequest(t, ts, a2a.MethodSendMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-cancel",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("Hello")}},
		},
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	var task a2a.Task
	if err := json.Unmarshal(resp.Result, &task); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	<-sendStarted

	cancelResp := a2aRPCRequest(t, ts, a2a.MethodCancelTask, a2a.CancelTaskRequest{ID: task.ID})
	if cancelResp.Error != nil {
		t.Fatalf("cancel error: %d %s", cancelResp.Error.Code, cancelResp.Error.Message)
	}

	var cancelledTask a2a.Task
	if err := json.Unmarshal(cancelResp.Result, &cancelledTask); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cancelledTask.Status.State != a2a.TaskStateCanceled {
		t.Fatalf("state = %q, want canceled", cancelledTask.Status.State)
	}
}

func TestA2AServer_CancelTask_NotFound(t *testing.T) {
	_, ts := newA2ATestServer(nopA2AOpener)
	defer ts.Close()

	resp := a2aRPCRequest(t, ts, a2a.MethodCancelTask, a2a.CancelTaskRequest{ID: "nonexistent"})
	if resp.Error == nil {
		t.Fatal("expected error for nonexistent task")
	}
	if resp.Error.Code != -32001 {
		t.Fatalf("error code = %d, want -32001", resp.Error.Code)
	}
}

func TestA2AServer_UnknownMethod(t *testing.T) {
	_, ts := newA2ATestServer(nopA2AOpener)
	defer ts.Close()

	resp := a2aRPCRequest(t, ts, "bogus/method", nil)
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Fatalf("error code = %d, want -32601", resp.Error.Code)
	}
}

func TestA2AServer_InvalidJSON(t *testing.T) {
	_, ts := newA2ATestServer(nopA2AOpener)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/a2a", "application/json", bytes.NewReader([]byte("not json")))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	var rpcResp a2a.JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected parse error")
	}
	if rpcResp.Error.Code != -32700 {
		t.Fatalf("error code = %d, want -32700", rpcResp.Error.Code)
	}
}

func TestA2AServer_ConversationReuse(t *testing.T) {
	var openCount atomic.Int32
	mock := completingA2AMock()

	_, ts := newA2ATestServer(func(string) (a2aConv, error) {
		openCount.Add(1)
		return mock, nil
	})
	defer ts.Close()

	a2aSendMessage(t, ts, "ctx-reuse", "msg1")
	a2aSendMessage(t, ts, "ctx-reuse", "msg2")

	if got := openCount.Load(); got != 1 {
		t.Fatalf("opener called %d times, want 1", got)
	}
}

func TestA2AServer_ConcurrentRequests(t *testing.T) {
	var openCount atomic.Int32
	_, ts := newA2ATestServer(func(string) (a2aConv, error) {
		openCount.Add(1)
		return completingA2AMock(), nil
	})
	defer ts.Close()

	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)
	errs := make(chan error, n)

	for i := range n {
		go func(i int) {
			defer wg.Done()
			task := a2aSendMessage(t, ts, "ctx-concurrent-"+string(rune('A'+i)), "Hello")
			if task.Status.State != a2a.TaskStateCompleted {
				errs <- errors.New("task not completed: " + string(task.Status.State))
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}

	if got := openCount.Load(); got != n {
		t.Fatalf("opener called %d times, want %d", got, n)
	}
}

func TestA2AServer_HITL(t *testing.T) {
	var callCount atomic.Int32
	mock := &mockA2AConv{
		sendFunc: func(_ context.Context, _ any, _ ...SendOption) (*Response, error) {
			n := callCount.Add(1)
			if n == 1 {
				return &Response{
					message:      &types.Message{Role: "assistant"},
					pendingTools: []PendingTool{{ID: "tool-1", Name: "approve"}},
				}, nil
			}
			return &Response{
				message: &types.Message{
					Role:  "assistant",
					Parts: []types.ContentPart{types.NewTextPart("final answer")},
				},
			}, nil
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	task1 := a2aSendMessage(t, ts, "ctx-hitl", "start")
	if task1.Status.State != a2a.TaskStateInputRequired {
		t.Fatalf("state = %q, want input_required", task1.Status.State)
	}

	task2 := a2aSendMessage(t, ts, "ctx-hitl", "tool result")
	if task2.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("state = %q, want completed", task2.Status.State)
	}
}

func TestA2AServer_ListTasks(t *testing.T) {
	mock := completingA2AMock()
	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	a2aSendMessage(t, ts, "ctx-list", "msg1")
	a2aSendMessage(t, ts, "ctx-list", "msg2")

	resp := a2aRPCRequest(t, ts, a2a.MethodListTasks, a2a.ListTasksRequest{
		ContextID: "ctx-list",
	})
	if resp.Error != nil {
		t.Fatalf("list error: %d %s", resp.Error.Code, resp.Error.Message)
	}

	var listResp a2a.ListTasksResponse
	if err := json.Unmarshal(resp.Result, &listResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(listResp.Tasks) != 2 {
		t.Fatalf("got %d tasks, want 2", len(listResp.Tasks))
	}
}

func TestA2AServer_Shutdown(t *testing.T) {
	mock := completingA2AMock()
	srv, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	a2aSendMessage(t, ts, "ctx-shutdown", "Hello")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	if !mock.closed.Load() {
		t.Fatal("conversation was not closed during shutdown")
	}
}

func TestA2AServer_WithTaskStore(t *testing.T) {
	store := NewInMemoryA2ATaskStore()
	mock := completingA2AMock()
	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil }, WithA2ATaskStore(store))
	defer ts.Close()

	task := a2aSendMessage(t, ts, "ctx-store", "Hello")

	got, err := store.Get(task.ID)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if got.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("state = %q, want completed", got.Status.State)
	}
}

func TestA2AServer_WithPort(t *testing.T) {
	srv := NewA2AServer(nopA2AOpener, WithA2APort(0))
	if srv.port != 0 {
		t.Fatalf("port = %d, want 0", srv.port)
	}
}
