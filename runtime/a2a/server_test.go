package a2a

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

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// --- mock conversation ---

type mockConversation struct {
	sendFunc func(ctx context.Context, msg *types.Message) (*ConversationResult, error)
	closed   atomic.Bool
}

func (m *mockConversation) Send(ctx context.Context, msg *types.Message) (*ConversationResult, error) {
	return m.sendFunc(ctx, msg)
}

func (m *mockConversation) Close() error {
	m.closed.Store(true)
	return nil
}

// --- helpers ---

func textPtr(s string) *string { return &s }

func newTestServer(opener ConversationOpener, opts ...ServerOption) (*Server, *httptest.Server) {
	srv := NewServer(opener, opts...)
	ts := httptest.NewServer(srv.Handler())
	return srv, ts
}

func rpcRequest(t *testing.T, ts *httptest.Server, method string, params any) *JSONRPCResponse {
	t.Helper()
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	body, err := json.Marshal(JSONRPCRequest{
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

	var rpcResp JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return &rpcResp
}

func rpcRequestTask(t *testing.T, ts *httptest.Server, method string, params any) *Task {
	t.Helper()
	resp := rpcRequest(t, ts, method, params)
	if resp.Error != nil {
		t.Fatalf("unexpected RPC error: %d %s", resp.Error.Code, resp.Error.Message)
	}
	var task Task
	if err := json.Unmarshal(resp.Result, &task); err != nil {
		t.Fatalf("unmarshal task: %v", err)
	}
	return &task
}

func sendMessage(t *testing.T, ts *httptest.Server, contextID, text string) *Task {
	t.Helper()
	return rpcRequestTask(t, ts, MethodSendMessage, SendMessageRequest{
		Message: Message{
			ContextID: contextID,
			Role:      RoleUser,
			Parts:     []Part{{Text: textPtr(text)}},
		},
		Configuration: &SendMessageConfiguration{Blocking: true},
	})
}

func nopOpener(string) (Conversation, error) {
	return nil, errors.New("should not be called")
}

func completingMock() *mockConversation {
	return &mockConversation{
		sendFunc: func(_ context.Context, _ *types.Message) (*ConversationResult, error) {
			return &ConversationResult{
				Parts: []types.ContentPart{types.NewTextPart("ok")},
			}, nil
		},
	}
}

// --- tests ---

func TestAgentCardDiscovery(t *testing.T) {
	card := AgentCard{
		Name:        "test-agent",
		Description: "A test agent",
		Version:     "1.0",
	}
	_, ts := newTestServer(nopOpener, WithCard(&card))
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

	var got AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Name != card.Name || got.Description != card.Description || got.Version != card.Version {
		t.Fatalf("got %+v, want %+v", got, card)
	}
}

func TestSendMessage_Completed(t *testing.T) {
	replyText := "Hello from the agent"
	mock := &mockConversation{
		sendFunc: func(_ context.Context, _ *types.Message) (*ConversationResult, error) {
			return &ConversationResult{
				Parts: []types.ContentPart{types.NewTextPart(replyText)},
			}, nil
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	task := sendMessage(t, ts, "ctx-1", "Hello")

	if task.Status.State != TaskStateCompleted {
		t.Fatalf("state = %q, want completed", task.Status.State)
	}
	if len(task.Artifacts) == 0 {
		t.Fatal("expected artifacts")
	}
	if task.Artifacts[0].Parts[0].Text == nil || *task.Artifacts[0].Parts[0].Text != replyText {
		t.Fatalf("artifact text = %v, want %q", task.Artifacts[0].Parts[0].Text, replyText)
	}
}

func TestSendMessage_Failed(t *testing.T) {
	mock := &mockConversation{
		sendFunc: func(_ context.Context, _ *types.Message) (*ConversationResult, error) {
			return nil, errors.New("provider error")
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	task := sendMessage(t, ts, "ctx-fail", "Hello")

	if task.Status.State != TaskStateFailed {
		t.Fatalf("state = %q, want failed", task.Status.State)
	}
	if task.Status.Message == nil {
		t.Fatal("expected status message on failure")
	}
}

func TestSendMessage_InputRequired(t *testing.T) {
	mock := &mockConversation{
		sendFunc: func(_ context.Context, _ *types.Message) (*ConversationResult, error) {
			return &ConversationResult{PendingTools: true}, nil
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	task := sendMessage(t, ts, "ctx-tools", "Hello")

	if task.Status.State != TaskStateInputRequired {
		t.Fatalf("state = %q, want input_required", task.Status.State)
	}
}

func TestSendMessage_Multimodal(t *testing.T) {
	mock := &mockConversation{
		sendFunc: func(_ context.Context, msg *types.Message) (*ConversationResult, error) {
			if len(msg.Parts) != 2 {
				return nil, errors.New("expected 2 parts")
			}
			if msg.Parts[0].Type != types.ContentTypeText {
				return nil, errors.New("expected text part first")
			}
			if msg.Parts[1].Type != types.ContentTypeImage {
				return nil, errors.New("expected image part second")
			}
			return &ConversationResult{
				Parts: []types.ContentPart{types.NewTextPart("got it")},
			}, nil
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	imgURL := "https://example.com/image.png"
	task := rpcRequestTask(t, ts, MethodSendMessage, SendMessageRequest{
		Message: Message{
			ContextID: "ctx-multi",
			Role:      RoleUser,
			Parts: []Part{
				{Text: textPtr("Look at this image")},
				{URL: &imgURL, MediaType: "image/png"},
			},
		},
		Configuration: &SendMessageConfiguration{Blocking: true},
	})

	if task.Status.State != TaskStateCompleted {
		t.Fatalf("state = %q, want completed", task.Status.State)
	}
}

func TestSendMessage_OpenerError(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) {
		return nil, errors.New("opener failed")
	})
	defer ts.Close()

	resp := rpcRequest(t, ts, MethodSendMessage, SendMessageRequest{
		Message: Message{
			ContextID: "ctx-err",
			Role:      RoleUser,
			Parts:     []Part{{Text: textPtr("Hello")}},
		},
	})
	if resp.Error == nil {
		t.Fatal("expected error when opener fails")
	}
	if resp.Error.Code != -32000 {
		t.Fatalf("error code = %d, want -32000", resp.Error.Code)
	}
}

func TestSendMessage_NoContextID(t *testing.T) {
	mock := completingMock()
	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	task := rpcRequestTask(t, ts, MethodSendMessage, SendMessageRequest{
		Message: Message{
			Role:  RoleUser,
			Parts: []Part{{Text: textPtr("Hello")}},
		},
		Configuration: &SendMessageConfiguration{Blocking: true},
	})

	if task.ContextID == "" {
		t.Fatal("expected server to generate a context ID")
	}
	if task.Status.State != TaskStateCompleted {
		t.Fatalf("state = %q, want completed", task.Status.State)
	}
}

func TestServerGetTask(t *testing.T) {
	mock := completingMock()
	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	task := sendMessage(t, ts, "ctx-get", "Hello")

	got := rpcRequestTask(t, ts, MethodGetTask, GetTaskRequest{ID: task.ID})

	if got.ID != task.ID {
		t.Fatalf("got task ID %q, want %q", got.ID, task.ID)
	}
	if got.Status.State != TaskStateCompleted {
		t.Fatalf("state = %q, want completed", got.Status.State)
	}
}

func TestServerGetTask_NotFound(t *testing.T) {
	_, ts := newTestServer(nopOpener)
	defer ts.Close()

	resp := rpcRequest(t, ts, MethodGetTask, GetTaskRequest{ID: "nonexistent"})
	if resp.Error == nil {
		t.Fatal("expected error for nonexistent task")
	}
	if resp.Error.Code != -32001 {
		t.Fatalf("error code = %d, want -32001", resp.Error.Code)
	}
}

func TestServerCancelTask(t *testing.T) {
	sendStarted := make(chan struct{})
	mock := &mockConversation{
		sendFunc: func(ctx context.Context, _ *types.Message) (*ConversationResult, error) {
			close(sendStarted)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	resp := rpcRequest(t, ts, MethodSendMessage, SendMessageRequest{
		Message: Message{
			ContextID: "ctx-cancel",
			Role:      RoleUser,
			Parts:     []Part{{Text: textPtr("Hello")}},
		},
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	var task Task
	if err := json.Unmarshal(resp.Result, &task); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	<-sendStarted

	cancelResp := rpcRequest(t, ts, MethodCancelTask, CancelTaskRequest{ID: task.ID})
	if cancelResp.Error != nil {
		t.Fatalf("cancel error: %d %s", cancelResp.Error.Code, cancelResp.Error.Message)
	}

	var cancelledTask Task
	if err := json.Unmarshal(cancelResp.Result, &cancelledTask); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cancelledTask.Status.State != TaskStateCanceled {
		t.Fatalf("state = %q, want canceled", cancelledTask.Status.State)
	}
}

func TestServerCancelTask_NotFound(t *testing.T) {
	_, ts := newTestServer(nopOpener)
	defer ts.Close()

	resp := rpcRequest(t, ts, MethodCancelTask, CancelTaskRequest{ID: "nonexistent"})
	if resp.Error == nil {
		t.Fatal("expected error for nonexistent task")
	}
	if resp.Error.Code != -32001 {
		t.Fatalf("error code = %d, want -32001", resp.Error.Code)
	}
}

func TestUnknownMethod(t *testing.T) {
	_, ts := newTestServer(nopOpener)
	defer ts.Close()

	resp := rpcRequest(t, ts, "bogus/method", nil)
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Fatalf("error code = %d, want -32601", resp.Error.Code)
	}
}

func TestInvalidJSON(t *testing.T) {
	_, ts := newTestServer(nopOpener)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/a2a", "application/json", bytes.NewReader([]byte("not json")))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	var rpcResp JSONRPCResponse
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

func TestConversationReuse(t *testing.T) {
	var openCount atomic.Int32
	mock := completingMock()

	_, ts := newTestServer(func(string) (Conversation, error) {
		openCount.Add(1)
		return mock, nil
	})
	defer ts.Close()

	sendMessage(t, ts, "ctx-reuse", "msg1")
	sendMessage(t, ts, "ctx-reuse", "msg2")

	if got := openCount.Load(); got != 1 {
		t.Fatalf("opener called %d times, want 1", got)
	}
}

func TestConcurrentRequests(t *testing.T) {
	var openCount atomic.Int32
	_, ts := newTestServer(func(string) (Conversation, error) {
		openCount.Add(1)
		return completingMock(), nil
	})
	defer ts.Close()

	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)
	errs := make(chan error, n)

	for i := range n {
		go func(i int) {
			defer wg.Done()
			task := sendMessage(t, ts, "ctx-concurrent-"+string(rune('A'+i)), "Hello")
			if task.Status.State != TaskStateCompleted {
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

func TestHITL(t *testing.T) {
	var callCount atomic.Int32
	mock := &mockConversation{
		sendFunc: func(_ context.Context, _ *types.Message) (*ConversationResult, error) {
			n := callCount.Add(1)
			if n == 1 {
				return &ConversationResult{PendingTools: true}, nil
			}
			return &ConversationResult{
				Parts: []types.ContentPart{types.NewTextPart("final answer")},
			}, nil
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	task1 := sendMessage(t, ts, "ctx-hitl", "start")
	if task1.Status.State != TaskStateInputRequired {
		t.Fatalf("state = %q, want input_required", task1.Status.State)
	}

	task2 := sendMessage(t, ts, "ctx-hitl", "tool result")
	if task2.Status.State != TaskStateCompleted {
		t.Fatalf("state = %q, want completed", task2.Status.State)
	}
}

func TestServerListTasks(t *testing.T) {
	mock := completingMock()
	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	sendMessage(t, ts, "ctx-list", "msg1")
	sendMessage(t, ts, "ctx-list", "msg2")

	resp := rpcRequest(t, ts, MethodListTasks, ListTasksRequest{
		ContextID: "ctx-list",
	})
	if resp.Error != nil {
		t.Fatalf("list error: %d %s", resp.Error.Code, resp.Error.Message)
	}

	var listResp ListTasksResponse
	if err := json.Unmarshal(resp.Result, &listResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(listResp.Tasks) != 2 {
		t.Fatalf("got %d tasks, want 2", len(listResp.Tasks))
	}
}

func TestShutdown(t *testing.T) {
	mock := completingMock()
	srv, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	sendMessage(t, ts, "ctx-shutdown", "Hello")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	if !mock.closed.Load() {
		t.Fatal("conversation was not closed during shutdown")
	}
}

func TestWithTaskStore(t *testing.T) {
	store := NewInMemoryTaskStore()
	mock := completingMock()
	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil }, WithTaskStore(store))
	defer ts.Close()

	task := sendMessage(t, ts, "ctx-store", "Hello")

	got, err := store.Get(task.ID)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if got.Status.State != TaskStateCompleted {
		t.Fatalf("state = %q, want completed", got.Status.State)
	}
}

func TestWithPort(t *testing.T) {
	srv := NewServer(nopOpener, WithPort(0))
	if srv.port != 0 {
		t.Fatalf("port = %d, want 0", srv.port)
	}
}
