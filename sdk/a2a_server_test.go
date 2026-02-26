package sdk

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/AltairaLabs/PromptKit/runtime/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/telemetry"
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

func TestA2AServer_SendMessage_MultimodalArtifacts(t *testing.T) {
	imgURL := "https://example.com/result.png"
	mock := &mockA2AConv{
		sendFunc: func(_ context.Context, _ any, _ ...SendOption) (*Response, error) {
			return &Response{
				message: &types.Message{
					Role: "assistant",
					Parts: []types.ContentPart{
						types.NewTextPart("Here is the image"),
						{
							Type: types.ContentTypeImage,
							Media: &types.MediaContent{
								URL:      &imgURL,
								MIMEType: "image/png",
							},
						},
					},
				},
			}, nil
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	task := a2aSendMessage(t, ts, "ctx-multimodal-out", "Generate an image")

	if task.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("state = %q, want completed", task.Status.State)
	}
	if len(task.Artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(task.Artifacts))
	}
	parts := task.Artifacts[0].Parts
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts in artifact, got %d", len(parts))
	}
	if parts[0].Text == nil || *parts[0].Text != "Here is the image" {
		t.Fatalf("part[0] text = %v, want %q", parts[0].Text, "Here is the image")
	}
	if parts[1].URL == nil || *parts[1].URL != imgURL {
		t.Fatalf("part[1] URL = %v, want %q", parts[1].URL, imgURL)
	}
	if parts[1].MediaType != "image/png" {
		t.Fatalf("part[1] mediaType = %q, want %q", parts[1].MediaType, "image/png")
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

func TestTraceContextPropagation_SendMessage(t *testing.T) {
	// Set up OTel propagation and tracer provider.
	origProp := otel.GetTextMapPropagator()
	defer otel.SetTextMapPropagator(origProp)
	telemetry.SetupPropagation()

	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	// Create a parent span to generate a valid traceparent.
	tracer := tp.Tracer("test")
	parentCtx, parentSpan := tracer.Start(context.Background(), "test-parent")
	parentSC := trace.SpanContextFromContext(parentCtx)
	parentSpan.End()

	// Inject trace headers using OTel propagator.
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(parentCtx, carrier)
	wantTP := carrier.Get("traceparent")

	var gotSC trace.SpanContext
	mock := &mockA2AConv{
		sendFunc: func(ctx context.Context, _ any, _ ...SendOption) (*Response, error) {
			gotSC = trace.SpanContextFromContext(ctx)
			return &Response{
				message: &types.Message{
					Role:  "assistant",
					Parts: []types.ContentPart{types.NewTextPart("ok")},
				},
			}, nil
		},
	}

	srv := NewA2AServer(func(string) (a2aConv, error) { return mock, nil })
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Build a request with trace headers.
	paramsJSON, _ := json.Marshal(a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-trace",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("Hello")}},
		},
		Configuration: &a2a.SendMessageConfiguration{Blocking: true},
	})
	body, _ := json.Marshal(a2a.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  a2a.MethodSendMessage,
		Params:  paramsJSON,
	})

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
		ts.URL+"/a2a", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("traceparent", wantTP)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if gotSC.TraceID() != parentSC.TraceID() {
		t.Errorf("TraceID = %q, want %q", gotSC.TraceID(), parentSC.TraceID())
	}
}

func TestTraceContextPropagation_StreamMessage(t *testing.T) {
	// Set up OTel propagation and tracer provider.
	origProp := otel.GetTextMapPropagator()
	defer otel.SetTextMapPropagator(origProp)
	telemetry.SetupPropagation()

	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	// Create a parent span to generate a valid traceparent.
	tracer := tp.Tracer("test")
	parentCtx, parentSpan := tracer.Start(context.Background(), "test-stream-parent")
	parentSC := trace.SpanContextFromContext(parentCtx)
	parentSpan.End()

	// Inject trace headers using OTel propagator.
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(parentCtx, carrier)
	wantTP := carrier.Get("traceparent")

	var gotSC trace.SpanContext
	mock := &mockA2AStreamConv{
		mockA2AConv: mockA2AConv{
			sendFunc: func(_ context.Context, _ any, _ ...SendOption) (*Response, error) {
				return nil, errors.New("should not be called")
			},
		},
		streamFunc: func(ctx context.Context, _ any, _ ...SendOption) <-chan StreamChunk {
			gotSC = trace.SpanContextFromContext(ctx)
			ch := make(chan StreamChunk, 1)
			ch <- StreamChunk{Type: ChunkDone}
			close(ch)
			return ch
		},
	}

	srv := NewA2AServer(func(string) (a2aConv, error) { return mock, nil })
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	paramsJSON, _ := json.Marshal(a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-trace-stream",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("Hello")}},
		},
	})
	body, _ := json.Marshal(a2a.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  a2a.MethodSendStreamingMessage,
		Params:  paramsJSON,
	})

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
		ts.URL+"/a2a", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("traceparent", wantTP)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	// Drain the SSE stream to let the handler complete.
	var buf [4096]byte
	for {
		_, readErr := resp.Body.Read(buf[:])
		if readErr != nil {
			break
		}
	}

	if gotSC.TraceID() != parentSC.TraceID() {
		t.Errorf("TraceID = %q, want %q", gotSC.TraceID(), parentSC.TraceID())
	}
}

// --- helper for sending custom A2A Parts ---

func a2aSendMessageWithParts(t *testing.T, ts *httptest.Server, contextID string, parts []a2a.Part) *a2a.Task {
	t.Helper()
	return a2aRPCRequestTask(t, ts, a2a.MethodSendMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: contextID,
			Role:      a2a.RoleUser,
			Parts:     parts,
		},
		Configuration: &a2a.SendMessageConfiguration{Blocking: true},
	})
}

// --- Group A: End-to-End Data Propagation (P0) ---

func TestA2AServer_SendMessage_Base64DataArtifact(t *testing.T) {
	// Mock returns ContentPart with base64-encoded image data.
	// Verify artifact Part has Raw bytes matching original + correct MediaType.
	originalBytes := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A} // PNG header
	b64 := base64.StdEncoding.EncodeToString(originalBytes)

	mock := &mockA2AConv{
		sendFunc: func(_ context.Context, _ any, _ ...SendOption) (*Response, error) {
			return &Response{
				message: &types.Message{
					Role: "assistant",
					Parts: []types.ContentPart{
						{
							Type: types.ContentTypeImage,
							Media: &types.MediaContent{
								Data:     &b64,
								MIMEType: "image/png",
							},
						},
					},
				},
			}, nil
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	task := a2aSendMessage(t, ts, "ctx-base64", "Generate image")

	if task.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("state = %q, want completed", task.Status.State)
	}
	if len(task.Artifacts) == 0 {
		t.Fatal("expected artifacts")
	}
	parts := task.Artifacts[0].Parts
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].MediaType != "image/png" {
		t.Errorf("mediaType = %q, want image/png", parts[0].MediaType)
	}
	if !bytes.Equal(parts[0].Raw, originalBytes) {
		t.Errorf("Raw bytes mismatch: got %x, want %x", parts[0].Raw, originalBytes)
	}
}

func TestA2AServer_SendMessage_URLMediaRoundTrip(t *testing.T) {
	// Send URL-based A2A Part inbound → mock receives ContentPart with Media.URL
	// → mock echoes it back → artifact has same URL and MediaType.
	imgURL := "https://example.com/photo.jpg"
	var receivedParts []types.ContentPart

	mock := &mockA2AConv{
		sendFunc: func(_ context.Context, message any, _ ...SendOption) (*Response, error) {
			msg := message.(*types.Message)
			receivedParts = msg.Parts
			// Echo the media back.
			return &Response{
				message: &types.Message{
					Role:  "assistant",
					Parts: msg.Parts,
				},
			}, nil
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	task := a2aSendMessageWithParts(t, ts, "ctx-url-rt", []a2a.Part{
		{URL: &imgURL, MediaType: "image/jpeg"},
	})

	// Verify inbound conversion.
	if len(receivedParts) != 1 {
		t.Fatalf("mock received %d parts, want 1", len(receivedParts))
	}
	if receivedParts[0].Media == nil || receivedParts[0].Media.URL == nil {
		t.Fatal("mock did not receive URL media")
	}
	if *receivedParts[0].Media.URL != imgURL {
		t.Errorf("inbound URL = %q, want %q", *receivedParts[0].Media.URL, imgURL)
	}

	// Verify outbound artifact.
	if task.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("state = %q, want completed", task.Status.State)
	}
	if len(task.Artifacts) == 0 || len(task.Artifacts[0].Parts) == 0 {
		t.Fatal("expected artifact with parts")
	}
	p := task.Artifacts[0].Parts[0]
	if p.URL == nil || *p.URL != imgURL {
		t.Errorf("artifact URL = %v, want %q", p.URL, imgURL)
	}
	if p.MediaType != "image/jpeg" {
		t.Errorf("artifact mediaType = %q, want image/jpeg", p.MediaType)
	}
}

func TestA2AServer_SendMessage_MixedPartsPreserved(t *testing.T) {
	// Send 3 parts (text + base64 image + URL audio). Mock returns 3 parts.
	// Artifact contains all 3 in order with correct types and data.
	imgBytes := []byte{0xFF, 0xD8, 0xFF, 0xE0} // JPEG header
	imgB64 := base64.StdEncoding.EncodeToString(imgBytes)
	audioURL := "https://example.com/audio.wav"

	mock := &mockA2AConv{
		sendFunc: func(_ context.Context, _ any, _ ...SendOption) (*Response, error) {
			return &Response{
				message: &types.Message{
					Role: "assistant",
					Parts: []types.ContentPart{
						types.NewTextPart("Here is the result"),
						{
							Type: types.ContentTypeImage,
							Media: &types.MediaContent{
								Data:     &imgB64,
								MIMEType: "image/jpeg",
							},
						},
						{
							Type: types.ContentTypeAudio,
							Media: &types.MediaContent{
								URL:      &audioURL,
								MIMEType: "audio/wav",
							},
						},
					},
				},
			}, nil
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	task := a2aSendMessage(t, ts, "ctx-mixed", "Do all the things")

	if task.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("state = %q, want completed", task.Status.State)
	}
	if len(task.Artifacts) == 0 {
		t.Fatal("expected artifacts")
	}
	parts := task.Artifacts[0].Parts
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(parts))
	}

	// Part 0: text
	if parts[0].Text == nil || *parts[0].Text != "Here is the result" {
		t.Errorf("part[0] text = %v, want 'Here is the result'", parts[0].Text)
	}
	// Part 1: base64 image
	if !bytes.Equal(parts[1].Raw, imgBytes) {
		t.Errorf("part[1] Raw mismatch: got %x, want %x", parts[1].Raw, imgBytes)
	}
	if parts[1].MediaType != "image/jpeg" {
		t.Errorf("part[1] mediaType = %q, want image/jpeg", parts[1].MediaType)
	}
	// Part 2: URL audio
	if parts[2].URL == nil || *parts[2].URL != audioURL {
		t.Errorf("part[2] URL = %v, want %q", parts[2].URL, audioURL)
	}
	if parts[2].MediaType != "audio/wav" {
		t.Errorf("part[2] mediaType = %q, want audio/wav", parts[2].MediaType)
	}
}

func TestA2AServer_SendMessage_RawBinaryRoundTrip(t *testing.T) {
	// Send raw binary A2A Part inbound, mock echoes media data back.
	// Verify bytes survive both inbound and outbound conversion.
	rawData := []byte{0x00, 0x01, 0x02, 0xFE, 0xFF}

	mock := &mockA2AConv{
		sendFunc: func(_ context.Context, message any, _ ...SendOption) (*Response, error) {
			msg := message.(*types.Message)
			// Echo back the received parts.
			return &Response{
				message: &types.Message{
					Role:  "assistant",
					Parts: msg.Parts,
				},
			}, nil
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	task := a2aSendMessageWithParts(t, ts, "ctx-raw-rt", []a2a.Part{
		{Raw: rawData, MediaType: "image/png"},
	})

	if task.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("state = %q, want completed", task.Status.State)
	}
	if len(task.Artifacts) == 0 || len(task.Artifacts[0].Parts) == 0 {
		t.Fatal("expected artifact with parts")
	}
	p := task.Artifacts[0].Parts[0]
	if !bytes.Equal(p.Raw, rawData) {
		t.Errorf("Raw bytes mismatch: got %x, want %x", p.Raw, rawData)
	}
	if p.MediaType != "image/png" {
		t.Errorf("mediaType = %q, want image/png", p.MediaType)
	}
}

func TestA2AServer_SendMessage_AudioVideoDocTypes(t *testing.T) {
	// Tests audio/wav, video/mp4, application/pdf through full pipeline.
	audioURL := "https://example.com/sound.wav"
	videoURL := "https://example.com/clip.mp4"
	pdfURL := "https://example.com/doc.pdf"

	mock := &mockA2AConv{
		sendFunc: func(_ context.Context, _ any, _ ...SendOption) (*Response, error) {
			return &Response{
				message: &types.Message{
					Role: "assistant",
					Parts: []types.ContentPart{
						{Type: types.ContentTypeAudio, Media: &types.MediaContent{URL: &audioURL, MIMEType: "audio/wav"}},
						{Type: types.ContentTypeVideo, Media: &types.MediaContent{URL: &videoURL, MIMEType: "video/mp4"}},
						{Type: types.ContentTypeDocument, Media: &types.MediaContent{URL: &pdfURL, MIMEType: "application/pdf"}},
					},
				},
			}, nil
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	task := a2aSendMessage(t, ts, "ctx-media-types", "Give me all types")

	if task.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("state = %q, want completed", task.Status.State)
	}
	if len(task.Artifacts) == 0 {
		t.Fatal("expected artifacts")
	}
	parts := task.Artifacts[0].Parts
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(parts))
	}

	wantURLs := []string{audioURL, videoURL, pdfURL}
	wantTypes := []string{"audio/wav", "video/mp4", "application/pdf"}
	for i, p := range parts {
		if p.URL == nil || *p.URL != wantURLs[i] {
			t.Errorf("part[%d] URL = %v, want %q", i, p.URL, wantURLs[i])
		}
		if p.MediaType != wantTypes[i] {
			t.Errorf("part[%d] mediaType = %q, want %q", i, p.MediaType, wantTypes[i])
		}
	}
}

func TestA2AServer_SendMessage_PartMetadataDocumented(t *testing.T) {
	// Documents that Part.Metadata and Part.Filename are silently dropped at
	// PartToContentPart (no error, just lost) — ContentPart has no fields for these.
	mock := &mockA2AConv{
		sendFunc: func(_ context.Context, message any, _ ...SendOption) (*Response, error) {
			msg := message.(*types.Message)
			// Verify the text was preserved even though metadata/filename were dropped.
			if len(msg.Parts) != 1 || msg.Parts[0].Text == nil {
				return nil, errors.New("expected 1 text part")
			}
			return &Response{
				message: &types.Message{
					Role:  "assistant",
					Parts: []types.ContentPart{types.NewTextPart("ok")},
				},
			}, nil
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	// Part with Metadata and Filename — these fields will be dropped.
	task := a2aSendMessageWithParts(t, ts, "ctx-meta", []a2a.Part{
		{
			Text:     serverTextPtr("hello"),
			Metadata: map[string]any{"key": "val"},
			Filename: "test.txt",
		},
	})

	if task.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("state = %q, want completed", task.Status.State)
	}
}

func TestA2AServer_SendMessage_MessageMetadata(t *testing.T) {
	// Sends A2A Message with Metadata: {"trace_id": "abc"}.
	// Mock asserts it receives types.Message with matching metadata.
	var gotMeta map[string]interface{}

	mock := &mockA2AConv{
		sendFunc: func(_ context.Context, message any, _ ...SendOption) (*Response, error) {
			msg := message.(*types.Message)
			gotMeta = msg.Meta
			return &Response{
				message: &types.Message{
					Role:  "assistant",
					Parts: []types.ContentPart{types.NewTextPart("ok")},
				},
			}, nil
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	a2aRPCRequestTask(t, ts, a2a.MethodSendMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-msg-meta",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("hello")}},
			Metadata:  map[string]any{"trace_id": "abc"},
		},
		Configuration: &a2a.SendMessageConfiguration{Blocking: true},
	})

	if gotMeta == nil {
		t.Fatal("expected message metadata to be propagated")
	}
	if gotMeta["trace_id"] != "abc" {
		t.Errorf("trace_id = %v, want 'abc'", gotMeta["trace_id"])
	}
}

func TestA2AServer_SendMessage_EmptyPartsTextFallback(t *testing.T) {
	// Mock returns Response with nil Parts but non-empty Text().
	// Verifies the GH-428 fallback path creates a text artifact.
	mock := &mockA2AConv{
		sendFunc: func(_ context.Context, _ any, _ ...SendOption) (*Response, error) {
			msg := &types.Message{Role: "assistant"}
			msg.Content = "fallback text"
			return &Response{message: msg}, nil
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	task := a2aSendMessage(t, ts, "ctx-fallback", "Hello")

	if task.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("state = %q, want completed", task.Status.State)
	}
	if len(task.Artifacts) == 0 {
		t.Fatal("expected fallback text artifact")
	}
	p := task.Artifacts[0].Parts[0]
	if p.Text == nil || *p.Text != "fallback text" {
		t.Errorf("fallback text = %v, want 'fallback text'", p.Text)
	}
}

func TestA2AServer_GetTask_ReturnsArtifacts(t *testing.T) {
	// Send blocking message with multimodal response, then tasks/get.
	// Verify artifacts survive the task store round-trip.
	imgURL := "https://example.com/stored.png"
	mock := &mockA2AConv{
		sendFunc: func(_ context.Context, _ any, _ ...SendOption) (*Response, error) {
			return &Response{
				message: &types.Message{
					Role: "assistant",
					Parts: []types.ContentPart{
						types.NewTextPart("See the image"),
						{Type: types.ContentTypeImage, Media: &types.MediaContent{URL: &imgURL, MIMEType: "image/png"}},
					},
				},
			}, nil
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	task := a2aSendMessage(t, ts, "ctx-get-art", "Show me")

	// Now retrieve via tasks/get.
	got := a2aRPCRequestTask(t, ts, a2a.MethodGetTask, a2a.GetTaskRequest{ID: task.ID})

	if len(got.Artifacts) == 0 {
		t.Fatal("tasks/get returned no artifacts")
	}
	parts := got.Artifacts[0].Parts
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	if parts[0].Text == nil || *parts[0].Text != "See the image" {
		t.Errorf("part[0] text = %v, want 'See the image'", parts[0].Text)
	}
	if parts[1].URL == nil || *parts[1].URL != imgURL {
		t.Errorf("part[1] URL = %v, want %q", parts[1].URL, imgURL)
	}
}

func TestA2AServer_SendMessage_LargeBinaryData(t *testing.T) {
	// 1MB raw binary through full pipeline. Guards against truncation or
	// corruption at JSON serialization boundaries.
	largeData := make([]byte, 1<<20) // 1 MB
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}
	b64 := base64.StdEncoding.EncodeToString(largeData)

	mock := &mockA2AConv{
		sendFunc: func(_ context.Context, _ any, _ ...SendOption) (*Response, error) {
			return &Response{
				message: &types.Message{
					Role: "assistant",
					Parts: []types.ContentPart{
						{
							Type: types.ContentTypeImage,
							Media: &types.MediaContent{
								Data:     &b64,
								MIMEType: "image/png",
							},
						},
					},
				},
			}, nil
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	task := a2aSendMessage(t, ts, "ctx-large", "Big data")

	if task.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("state = %q, want completed", task.Status.State)
	}
	if len(task.Artifacts) == 0 || len(task.Artifacts[0].Parts) == 0 {
		t.Fatal("expected artifact with parts")
	}
	p := task.Artifacts[0].Parts[0]
	if len(p.Raw) != len(largeData) {
		t.Fatalf("raw length = %d, want %d", len(p.Raw), len(largeData))
	}
	if !bytes.Equal(p.Raw, largeData) {
		t.Error("1MB binary data corrupted during round-trip")
	}
}

// --- Group C: Protocol Compliance ---

func TestA2AServer_SendMessage_NonBlocking(t *testing.T) {
	// Send without Blocking: true, mock takes 100ms. Response returns quickly
	// with task in submitted or working (not completed).
	mock := &mockA2AConv{
		sendFunc: func(_ context.Context, _ any, _ ...SendOption) (*Response, error) {
			time.Sleep(100 * time.Millisecond)
			return &Response{
				message: &types.Message{
					Role:  "assistant",
					Parts: []types.ContentPart{types.NewTextPart("done")},
				},
			}, nil
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	task := a2aRPCRequestTask(t, ts, a2a.MethodSendMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-nonblock",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("Hello")}},
		},
		// No Configuration — non-blocking by default.
	})

	// Should NOT be completed yet since mock takes 100ms and settle time is 5ms.
	if task.Status.State == a2a.TaskStateCompleted {
		t.Fatalf("state = completed, want submitted or working for non-blocking call")
	}
}

func TestA2AServer_ListTasks_PageSize(t *testing.T) {
	mock := completingA2AMock()
	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	// Create 5 tasks on the same context.
	for i := range 5 {
		a2aSendMessage(t, ts, "ctx-page", "msg"+string(rune('0'+i)))
	}

	resp := a2aRPCRequest(t, ts, a2a.MethodListTasks, a2a.ListTasksRequest{
		ContextID: "ctx-page",
		PageSize:  2,
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

func TestA2AServer_SendMessage_InvalidParams(t *testing.T) {
	_, ts := newA2ATestServer(func(string) (a2aConv, error) {
		return completingA2AMock(), nil
	})
	defer ts.Close()

	// Send valid JSON-RPC but with params that don't match SendMessageRequest schema.
	// "message" field is missing the required "role" and "parts".
	resp := a2aRPCRequest(t, ts, a2a.MethodSendMessage, map[string]any{
		"message": "not-a-message-object",
	})

	if resp.Error == nil {
		t.Fatal("expected error for invalid params")
	}
	if resp.Error.Code != -32602 {
		t.Fatalf("error code = %d, want -32602", resp.Error.Code)
	}
}

func TestA2AServer_SendMessage_InvalidPart(t *testing.T) {
	// Empty Part{} → PartToContentPart error → error -32602 from server.
	_, ts := newA2ATestServer(func(string) (a2aConv, error) {
		return completingA2AMock(), nil
	})
	defer ts.Close()

	resp := a2aRPCRequest(t, ts, a2a.MethodSendMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-invalid-part",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{}}, // Empty part.
		},
		Configuration: &a2a.SendMessageConfiguration{Blocking: true},
	})
	if resp.Error == nil {
		t.Fatal("expected error for empty part")
	}
	if resp.Error.Code != -32602 {
		t.Fatalf("error code = %d, want -32602", resp.Error.Code)
	}
}

func TestA2AServer_ListTasks_StatusFilterIgnored(t *testing.T) {
	// P2: Documents that Status filter param is accepted but currently ignored.
	mock := completingA2AMock()
	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	a2aSendMessage(t, ts, "ctx-filter", "msg1")
	a2aSendMessage(t, ts, "ctx-filter", "msg2")

	// Filter for "failed" — should still return all tasks since filter is ignored.
	failedState := a2a.TaskStateFailed
	resp := a2aRPCRequest(t, ts, a2a.MethodListTasks, a2a.ListTasksRequest{
		ContextID: "ctx-filter",
		Status:    &failedState,
	})
	if resp.Error != nil {
		t.Fatalf("list error: %d %s", resp.Error.Code, resp.Error.Message)
	}

	var listResp a2a.ListTasksResponse
	if err := json.Unmarshal(resp.Result, &listResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// All tasks returned because Status filter is not implemented.
	if len(listResp.Tasks) != 2 {
		t.Fatalf("got %d tasks, want 2 (status filter should be ignored)", len(listResp.Tasks))
	}
}

func TestA2AServer_GetTask_HistoryLengthIgnored(t *testing.T) {
	// P2: Documents that HistoryLength param is accepted but has no effect.
	mock := completingA2AMock()
	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	task := a2aSendMessage(t, ts, "ctx-histlen", "Hello")

	histLen := 0
	got := a2aRPCRequestTask(t, ts, a2a.MethodGetTask, a2a.GetTaskRequest{
		ID:            task.ID,
		HistoryLength: &histLen,
	})

	// HistoryLength=0 would mean "no history" if implemented, but it's ignored.
	if got.ID != task.ID {
		t.Fatalf("task ID = %q, want %q", got.ID, task.ID)
	}
	if got.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("state = %q, want completed", got.Status.State)
	}
}

// --- Group D: Error Handling at Boundaries ---

func TestA2AServer_SendMessage_FailedTaskErrorParts(t *testing.T) {
	// Mock returns error. Failed task's Status.Message has Parts[0].Text
	// with error string and role "agent".
	mock := &mockA2AConv{
		sendFunc: func(_ context.Context, _ any, _ ...SendOption) (*Response, error) {
			return nil, errors.New("provider crashed")
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	task := a2aSendMessage(t, ts, "ctx-err-parts", "Hello")

	if task.Status.State != a2a.TaskStateFailed {
		t.Fatalf("state = %q, want failed", task.Status.State)
	}
	if task.Status.Message == nil {
		t.Fatal("expected status message")
	}
	if task.Status.Message.Role != a2a.RoleAgent {
		t.Errorf("role = %q, want agent", task.Status.Message.Role)
	}
	if len(task.Status.Message.Parts) == 0 {
		t.Fatal("expected error parts")
	}
	if task.Status.Message.Parts[0].Text == nil || *task.Status.Message.Parts[0].Text != "provider crashed" {
		t.Errorf("error text = %v, want 'provider crashed'", task.Status.Message.Parts[0].Text)
	}
}

func TestA2AServer_SendMessage_CancelDuringProcessing(t *testing.T) {
	// Start slow message, cancel it. Task becomes "canceled", NOT "failed".
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

	// Non-blocking send.
	task := a2aRPCRequestTask(t, ts, a2a.MethodSendMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-cancel-processing",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("slow")}},
		},
	})

	// Wait for send to start.
	<-sendStarted

	// Cancel the task.
	cancelResp := a2aRPCRequest(t, ts, a2a.MethodCancelTask, a2a.CancelTaskRequest{ID: task.ID})
	if cancelResp.Error != nil {
		t.Fatalf("cancel error: %d %s", cancelResp.Error.Code, cancelResp.Error.Message)
	}

	// Wait a bit for the goroutine to notice cancellation.
	time.Sleep(50 * time.Millisecond)

	// Verify via tasks/get.
	got := a2aRPCRequestTask(t, ts, a2a.MethodGetTask, a2a.GetTaskRequest{ID: task.ID})
	if got.Status.State != a2a.TaskStateCanceled {
		t.Fatalf("state = %q, want canceled (not failed)", got.Status.State)
	}
}

func TestA2AServer_SendMessage_DataPartRejected(t *testing.T) {
	// Part with Data: {"key": "val"} (structured data). Error -32602 because
	// structured data parts are unsupported.
	_, ts := newA2ATestServer(func(string) (a2aConv, error) {
		return completingA2AMock(), nil
	})
	defer ts.Close()

	resp := a2aRPCRequest(t, ts, a2a.MethodSendMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-data-part",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Data: map[string]any{"key": "val"}}},
		},
		Configuration: &a2a.SendMessageConfiguration{Blocking: true},
	})
	if resp.Error == nil {
		t.Fatal("expected error for structured data part")
	}
	if resp.Error.Code != -32602 {
		t.Fatalf("error code = %d, want -32602", resp.Error.Code)
	}
}

func TestA2AServer_SendMessage_ConversationReuseMultimodal(t *testing.T) {
	// Two multimodal messages to same contextID. Both get correct responses.
	var callCount atomic.Int32
	imgURL := "https://example.com/img.png"

	mock := &mockA2AConv{
		sendFunc: func(_ context.Context, message any, _ ...SendOption) (*Response, error) {
			n := callCount.Add(1)
			msg := message.(*types.Message)
			// Verify each message has the expected parts.
			if len(msg.Parts) != 2 {
				return nil, fmt.Errorf("call %d: expected 2 parts, got %d", n, len(msg.Parts))
			}
			return &Response{
				message: &types.Message{
					Role: "assistant",
					Parts: []types.ContentPart{
						types.NewTextPart(fmt.Sprintf("response %d", n)),
						{Type: types.ContentTypeImage, Media: &types.MediaContent{URL: &imgURL, MIMEType: "image/png"}},
					},
				},
			}, nil
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	// First multimodal message.
	task1 := a2aSendMessageWithParts(t, ts, "ctx-reuse-multi", []a2a.Part{
		{Text: serverTextPtr("msg1")},
		{URL: &imgURL, MediaType: "image/png"},
	})
	if task1.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("task1 state = %q, want completed", task1.Status.State)
	}
	if len(task1.Artifacts) == 0 || len(task1.Artifacts[0].Parts) != 2 {
		t.Fatal("task1: expected 2 artifact parts")
	}

	// Second multimodal message to same context.
	task2 := a2aSendMessageWithParts(t, ts, "ctx-reuse-multi", []a2a.Part{
		{Text: serverTextPtr("msg2")},
		{URL: &imgURL, MediaType: "image/png"},
	})
	if task2.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("task2 state = %q, want completed", task2.Status.State)
	}
	if len(task2.Artifacts) == 0 || len(task2.Artifacts[0].Parts) != 2 {
		t.Fatal("task2: expected 2 artifact parts")
	}

	if callCount.Load() != 2 {
		t.Fatalf("expected 2 calls, got %d", callCount.Load())
	}
}

// --- timeout and body size option tests ---

func TestA2AServerDefaultTimeouts(t *testing.T) {
	srv := NewA2AServer(func(string) (a2aConv, error) {
		return nil, nil
	})

	if srv.readTimeout != defaultReadTimeout {
		t.Errorf("readTimeout = %v, want %v", srv.readTimeout, defaultReadTimeout)
	}
	if srv.writeTimeout != defaultWriteTimeout {
		t.Errorf("writeTimeout = %v, want %v", srv.writeTimeout, defaultWriteTimeout)
	}
	if srv.idleTimeout != defaultIdleTimeout {
		t.Errorf("idleTimeout = %v, want %v", srv.idleTimeout, defaultIdleTimeout)
	}
	if srv.maxBodySize != defaultMaxBodySize {
		t.Errorf("maxBodySize = %d, want %d", srv.maxBodySize, defaultMaxBodySize)
	}
}

func TestA2AServerCustomTimeouts(t *testing.T) {
	srv := NewA2AServer(
		func(string) (a2aConv, error) { return nil, nil },
		WithA2AReadTimeout(5*time.Second),
		WithA2AWriteTimeout(10*time.Second),
		WithA2AIdleTimeout(15*time.Second),
		WithA2AMaxBodySize(1024),
	)

	if srv.readTimeout != 5*time.Second {
		t.Errorf("readTimeout = %v, want 5s", srv.readTimeout)
	}
	if srv.writeTimeout != 10*time.Second {
		t.Errorf("writeTimeout = %v, want 10s", srv.writeTimeout)
	}
	if srv.idleTimeout != 15*time.Second {
		t.Errorf("idleTimeout = %v, want 15s", srv.idleTimeout)
	}
	if srv.maxBodySize != 1024 {
		t.Errorf("maxBodySize = %d, want 1024", srv.maxBodySize)
	}
}

func TestA2AServerMaxBodySizeRejectsOversizedRequest(t *testing.T) {
	opener := func(string) (a2aConv, error) {
		return &mockA2AConv{
			sendFunc: func(context.Context, any, ...SendOption) (*Response, error) {
				return &Response{
					message: &types.Message{
						Role:  "assistant",
						Parts: []types.ContentPart{types.NewTextPart("ok")},
					},
				}, nil
			},
		}, nil
	}

	// Set a very small body size limit.
	srv := NewA2AServer(opener, WithA2AMaxBodySize(16))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Build a valid JSON-RPC request that exceeds the 16-byte limit.
	params, _ := json.Marshal(a2a.SendMessageRequest{
		Message: a2a.Message{
			Role:      a2a.RoleUser,
			ContextID: "ctx-1",
			Parts:     []a2a.Part{{Text: serverTextPtr("This message is deliberately large enough to exceed the body limit")}},
		},
	})
	body, _ := json.Marshal(a2a.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  a2a.MethodSendMessage,
		Params:  params,
	})

	resp, err := http.Post(ts.URL+"/a2a", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /a2a: %v", err)
	}
	defer resp.Body.Close()

	var rpcResp a2a.JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if rpcResp.Error == nil {
		t.Fatal("expected JSON-RPC error for oversized body, got success")
	}
	if rpcResp.Error.Code != -32700 {
		t.Errorf("error code = %d, want -32700 (Parse error)", rpcResp.Error.Code)
	}
}

func TestA2AServerListenAndServeTimeouts(t *testing.T) {
	srv := NewA2AServer(
		func(string) (a2aConv, error) { return nil, nil },
		WithA2AReadTimeout(15*time.Second),
		WithA2AWriteTimeout(30*time.Second),
		WithA2AIdleTimeout(60*time.Second),
	)

	// Use a listener on :0 so we get a free port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	go func() { _ = srv.Serve(ln) }()

	// Wait for Serve to populate httpSrv.
	deadline := time.Now().Add(2 * time.Second)
	for srv.httpSrv == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if srv.httpSrv == nil {
		t.Fatal("httpSrv was not set within timeout")
	}

	if srv.httpSrv.ReadTimeout != 15*time.Second {
		t.Errorf("httpSrv.ReadTimeout = %v, want 15s", srv.httpSrv.ReadTimeout)
	}
	if srv.httpSrv.WriteTimeout != 30*time.Second {
		t.Errorf("httpSrv.WriteTimeout = %v, want 30s", srv.httpSrv.WriteTimeout)
	}
	if srv.httpSrv.IdleTimeout != 60*time.Second {
		t.Errorf("httpSrv.IdleTimeout = %v, want 60s", srv.httpSrv.IdleTimeout)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
