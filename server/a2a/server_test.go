package a2aserver

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

// --- mock SendResult ---

type mockSendResult struct {
	parts      []types.ContentPart
	text       string
	hasPending bool
}

func (r *mockSendResult) HasPendingTools() bool       { return r.hasPending }
func (r *mockSendResult) Parts() []types.ContentPart { return r.parts }
func (r *mockSendResult) Text() string               { return r.text }

// --- mock conversation for server tests ---

type mockConv struct {
	sendFunc func(ctx context.Context, message any) (SendResult, error)
	closed   atomic.Bool
}

func (m *mockConv) Send(ctx context.Context, message any) (SendResult, error) {
	return m.sendFunc(ctx, message)
}

func (m *mockConv) Close() error {
	m.closed.Store(true)
	return nil
}

// --- mock streaming conversation ---

type mockStreamConv struct {
	mockConv
	streamFunc func(ctx context.Context, message any) <-chan StreamEvent
}

func (m *mockStreamConv) Stream(ctx context.Context, message any) <-chan StreamEvent {
	return m.streamFunc(ctx, message)
}

// --- helpers ---

func serverTextPtr(s string) *string { return &s }

func newTestServer(opener ConversationOpener, opts ...Option) (*Server, *httptest.Server) {
	srv := NewServer(opener, opts...)
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

func nopOpener(string) (Conversation, error) {
	return nil, errors.New("should not be called")
}

func completingMock() *mockConv {
	return &mockConv{
		sendFunc: func(_ context.Context, _ any) (SendResult, error) {
			return &mockSendResult{
				parts: []types.ContentPart{types.NewTextPart("ok")},
				text:  "ok",
			}, nil
		},
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

// --- errorClosingConv is a mock conversation whose Close() returns an error ---

type errorClosingConv struct {
	closeErr error
}

func (e *errorClosingConv) Send(_ context.Context, _ any) (SendResult, error) {
	return &mockSendResult{
		parts: []types.ContentPart{types.NewTextPart("ok")},
		text:  "ok",
	}, nil
}

func (e *errorClosingConv) Close() error {
	return e.closeErr
}

// --- tests ---

func TestServer_AgentCardDiscovery(t *testing.T) {
	card := a2a.AgentCard{
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

	var got a2a.AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Name != card.Name || got.Description != card.Description || got.Version != card.Version {
		t.Fatalf("got %+v, want %+v", got, card)
	}
}

func TestServer_SendMessage_Completed(t *testing.T) {
	replyText := "Hello from the agent"
	mock := &mockConv{
		sendFunc: func(_ context.Context, _ any) (SendResult, error) {
			return &mockSendResult{
				parts: []types.ContentPart{types.NewTextPart(replyText)},
				text:  replyText,
			}, nil
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
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

func TestServer_SendMessage_Failed(t *testing.T) {
	mock := &mockConv{
		sendFunc: func(_ context.Context, _ any) (SendResult, error) {
			return nil, errors.New("provider error")
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	task := a2aSendMessage(t, ts, "ctx-fail", "Hello")

	if task.Status.State != a2a.TaskStateFailed {
		t.Fatalf("state = %q, want failed", task.Status.State)
	}
	if task.Status.Message == nil {
		t.Fatal("expected status message on failure")
	}
}

func TestServer_SendMessage_InputRequired(t *testing.T) {
	mock := &mockConv{
		sendFunc: func(_ context.Context, _ any) (SendResult, error) {
			return &mockSendResult{hasPending: true}, nil
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	task := a2aSendMessage(t, ts, "ctx-tools", "Hello")

	if task.Status.State != a2a.TaskStateInputRequired {
		t.Fatalf("state = %q, want input_required", task.Status.State)
	}
}

func TestServer_SendMessage_Multimodal(t *testing.T) {
	mock := &mockConv{
		sendFunc: func(_ context.Context, message any) (SendResult, error) {
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
			return &mockSendResult{
				parts: []types.ContentPart{types.NewTextPart("got it")},
				text:  "got it",
			}, nil
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
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

func TestServer_SendMessage_MultimodalArtifacts(t *testing.T) {
	imgURL := "https://example.com/result.png"
	mock := &mockConv{
		sendFunc: func(_ context.Context, _ any) (SendResult, error) {
			return &mockSendResult{
				parts: []types.ContentPart{
					types.NewTextPart("Here is the image"),
					{
						Type: types.ContentTypeImage,
						Media: &types.MediaContent{
							URL:      &imgURL,
							MIMEType: "image/png",
						},
					},
				},
				text: "Here is the image",
			}, nil
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
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

func TestServer_SendMessage_OpenerError(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) {
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

func TestServer_SendMessage_NoContextID(t *testing.T) {
	mock := completingMock()
	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
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

func TestServer_GetTask(t *testing.T) {
	mock := completingMock()
	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
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

func TestServer_GetTask_NotFound(t *testing.T) {
	_, ts := newTestServer(nopOpener)
	defer ts.Close()

	resp := a2aRPCRequest(t, ts, a2a.MethodGetTask, a2a.GetTaskRequest{ID: "nonexistent"})
	if resp.Error == nil {
		t.Fatal("expected error for nonexistent task")
	}
	if resp.Error.Code != -32001 {
		t.Fatalf("error code = %d, want -32001", resp.Error.Code)
	}
}

func TestServer_CancelTask(t *testing.T) {
	sendStarted := make(chan struct{})
	mock := &mockConv{
		sendFunc: func(ctx context.Context, _ any) (SendResult, error) {
			close(sendStarted)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
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

func TestServer_CancelTask_NotFound(t *testing.T) {
	_, ts := newTestServer(nopOpener)
	defer ts.Close()

	resp := a2aRPCRequest(t, ts, a2a.MethodCancelTask, a2a.CancelTaskRequest{ID: "nonexistent"})
	if resp.Error == nil {
		t.Fatal("expected error for nonexistent task")
	}
	if resp.Error.Code != -32001 {
		t.Fatalf("error code = %d, want -32001", resp.Error.Code)
	}
}

func TestServer_UnknownMethod(t *testing.T) {
	_, ts := newTestServer(nopOpener)
	defer ts.Close()

	resp := a2aRPCRequest(t, ts, "bogus/method", nil)
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Fatalf("error code = %d, want -32601", resp.Error.Code)
	}
}

func TestServer_InvalidJSON(t *testing.T) {
	_, ts := newTestServer(nopOpener)
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

func TestServer_ConversationReuse(t *testing.T) {
	var openCount atomic.Int32
	mock := completingMock()

	_, ts := newTestServer(func(string) (Conversation, error) {
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

func TestServer_ConcurrentRequests(t *testing.T) {
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

func TestServer_HITL(t *testing.T) {
	var callCount atomic.Int32
	mock := &mockConv{
		sendFunc: func(_ context.Context, _ any) (SendResult, error) {
			n := callCount.Add(1)
			if n == 1 {
				return &mockSendResult{hasPending: true}, nil
			}
			return &mockSendResult{
				parts: []types.ContentPart{types.NewTextPart("final answer")},
				text:  "final answer",
			}, nil
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
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

func TestServer_ListTasks(t *testing.T) {
	mock := completingMock()
	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
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

func TestServer_Shutdown(t *testing.T) {
	mock := completingMock()
	srv, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
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

func TestServer_WithTaskStore(t *testing.T) {
	store := NewInMemoryTaskStore()
	mock := completingMock()
	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil }, WithTaskStore(store))
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

func TestServer_WithPort(t *testing.T) {
	srv := NewServer(nopOpener, WithPort(0))
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
	mock := &mockConv{
		sendFunc: func(ctx context.Context, _ any) (SendResult, error) {
			gotSC = trace.SpanContextFromContext(ctx)
			return &mockSendResult{
				parts: []types.ContentPart{types.NewTextPart("ok")},
				text:  "ok",
			}, nil
		},
	}

	srv := NewServer(func(string) (Conversation, error) { return mock, nil })
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
	mock := &mockStreamConv{
		mockConv: mockConv{
			sendFunc: func(_ context.Context, _ any) (SendResult, error) {
				return nil, errors.New("should not be called")
			},
		},
		streamFunc: func(ctx context.Context, _ any) <-chan StreamEvent {
			gotSC = trace.SpanContextFromContext(ctx)
			ch := make(chan StreamEvent, 1)
			ch <- StreamEvent{Kind: EventDone}
			close(ch)
			return ch
		},
	}

	srv := NewServer(func(string) (Conversation, error) { return mock, nil })
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

// --- Group A: End-to-End Data Propagation (P0) ---

func TestServer_SendMessage_Base64DataArtifact(t *testing.T) {
	originalBytes := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	b64 := base64.StdEncoding.EncodeToString(originalBytes)

	mock := &mockConv{
		sendFunc: func(_ context.Context, _ any) (SendResult, error) {
			return &mockSendResult{
				parts: []types.ContentPart{
					{
						Type: types.ContentTypeImage,
						Media: &types.MediaContent{
							Data:     &b64,
							MIMEType: "image/png",
						},
					},
				},
			}, nil
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
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

func TestServer_SendMessage_URLMediaRoundTrip(t *testing.T) {
	imgURL := "https://example.com/photo.jpg"
	var receivedParts []types.ContentPart

	mock := &mockConv{
		sendFunc: func(_ context.Context, message any) (SendResult, error) {
			msg := message.(*types.Message)
			receivedParts = msg.Parts
			return &mockSendResult{
				parts: msg.Parts,
			}, nil
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	task := a2aSendMessageWithParts(t, ts, "ctx-url-rt", []a2a.Part{
		{URL: &imgURL, MediaType: "image/jpeg"},
	})

	if len(receivedParts) != 1 {
		t.Fatalf("mock received %d parts, want 1", len(receivedParts))
	}
	if receivedParts[0].Media == nil || receivedParts[0].Media.URL == nil {
		t.Fatal("mock did not receive URL media")
	}
	if *receivedParts[0].Media.URL != imgURL {
		t.Errorf("inbound URL = %q, want %q", *receivedParts[0].Media.URL, imgURL)
	}

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

func TestServer_SendMessage_MixedPartsPreserved(t *testing.T) {
	imgBytes := []byte{0xFF, 0xD8, 0xFF, 0xE0}
	imgB64 := base64.StdEncoding.EncodeToString(imgBytes)
	audioURL := "https://example.com/audio.wav"

	mock := &mockConv{
		sendFunc: func(_ context.Context, _ any) (SendResult, error) {
			return &mockSendResult{
				parts: []types.ContentPart{
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
			}, nil
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
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

	if parts[0].Text == nil || *parts[0].Text != "Here is the result" {
		t.Errorf("part[0] text = %v, want 'Here is the result'", parts[0].Text)
	}
	if !bytes.Equal(parts[1].Raw, imgBytes) {
		t.Errorf("part[1] Raw mismatch: got %x, want %x", parts[1].Raw, imgBytes)
	}
	if parts[1].MediaType != "image/jpeg" {
		t.Errorf("part[1] mediaType = %q, want image/jpeg", parts[1].MediaType)
	}
	if parts[2].URL == nil || *parts[2].URL != audioURL {
		t.Errorf("part[2] URL = %v, want %q", parts[2].URL, audioURL)
	}
	if parts[2].MediaType != "audio/wav" {
		t.Errorf("part[2] mediaType = %q, want audio/wav", parts[2].MediaType)
	}
}

func TestServer_SendMessage_RawBinaryRoundTrip(t *testing.T) {
	rawData := []byte{0x00, 0x01, 0x02, 0xFE, 0xFF}

	mock := &mockConv{
		sendFunc: func(_ context.Context, message any) (SendResult, error) {
			msg := message.(*types.Message)
			return &mockSendResult{
				parts: msg.Parts,
			}, nil
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
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

func TestServer_SendMessage_AudioVideoDocTypes(t *testing.T) {
	audioURL := "https://example.com/sound.wav"
	videoURL := "https://example.com/clip.mp4"
	pdfURL := "https://example.com/doc.pdf"

	mock := &mockConv{
		sendFunc: func(_ context.Context, _ any) (SendResult, error) {
			return &mockSendResult{
				parts: []types.ContentPart{
					{Type: types.ContentTypeAudio, Media: &types.MediaContent{URL: &audioURL, MIMEType: "audio/wav"}},
					{Type: types.ContentTypeVideo, Media: &types.MediaContent{URL: &videoURL, MIMEType: "video/mp4"}},
					{Type: types.ContentTypeDocument, Media: &types.MediaContent{URL: &pdfURL, MIMEType: "application/pdf"}},
				},
			}, nil
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
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

func TestServer_SendMessage_PartMetadataDocumented(t *testing.T) {
	mock := &mockConv{
		sendFunc: func(_ context.Context, message any) (SendResult, error) {
			msg := message.(*types.Message)
			if len(msg.Parts) != 1 || msg.Parts[0].Text == nil {
				return nil, errors.New("expected 1 text part")
			}
			return &mockSendResult{
				parts: []types.ContentPart{types.NewTextPart("ok")},
				text:  "ok",
			}, nil
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

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

func TestServer_SendMessage_MessageMetadata(t *testing.T) {
	var gotMeta map[string]any

	mock := &mockConv{
		sendFunc: func(_ context.Context, message any) (SendResult, error) {
			msg := message.(*types.Message)
			gotMeta = msg.Meta
			return &mockSendResult{
				parts: []types.ContentPart{types.NewTextPart("ok")},
				text:  "ok",
			}, nil
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
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

func TestServer_SendMessage_EmptyPartsTextFallback(t *testing.T) {
	mock := &mockConv{
		sendFunc: func(_ context.Context, _ any) (SendResult, error) {
			return &mockSendResult{text: "fallback text"}, nil
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
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

func TestServer_GetTask_ReturnsArtifacts(t *testing.T) {
	imgURL := "https://example.com/stored.png"
	mock := &mockConv{
		sendFunc: func(_ context.Context, _ any) (SendResult, error) {
			return &mockSendResult{
				parts: []types.ContentPart{
					types.NewTextPart("See the image"),
					{Type: types.ContentTypeImage, Media: &types.MediaContent{URL: &imgURL, MIMEType: "image/png"}},
				},
			}, nil
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	task := a2aSendMessage(t, ts, "ctx-get-art", "Show me")

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

func TestServer_SendMessage_LargeBinaryData(t *testing.T) {
	largeData := make([]byte, 1<<20) // 1 MB
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}
	b64 := base64.StdEncoding.EncodeToString(largeData)

	mock := &mockConv{
		sendFunc: func(_ context.Context, _ any) (SendResult, error) {
			return &mockSendResult{
				parts: []types.ContentPart{
					{
						Type: types.ContentTypeImage,
						Media: &types.MediaContent{
							Data:     &b64,
							MIMEType: "image/png",
						},
					},
				},
			}, nil
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
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

func TestServer_SendMessage_NonBlocking(t *testing.T) {
	mock := &mockConv{
		sendFunc: func(_ context.Context, _ any) (SendResult, error) {
			time.Sleep(100 * time.Millisecond)
			return &mockSendResult{
				parts: []types.ContentPart{types.NewTextPart("done")},
				text:  "done",
			}, nil
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	task := a2aRPCRequestTask(t, ts, a2a.MethodSendMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-nonblock",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("Hello")}},
		},
	})

	if task.Status.State == a2a.TaskStateCompleted {
		t.Fatalf("state = completed, want submitted or working for non-blocking call")
	}
}

func TestServer_ListTasks_PageSize(t *testing.T) {
	mock := completingMock()
	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

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

func TestServer_SendMessage_InvalidParams(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) {
		return completingMock(), nil
	})
	defer ts.Close()

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

func TestServer_SendMessage_InvalidPart(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) {
		return completingMock(), nil
	})
	defer ts.Close()

	resp := a2aRPCRequest(t, ts, a2a.MethodSendMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-invalid-part",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{}},
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

func TestServer_ListTasks_StatusFilterIgnored(t *testing.T) {
	mock := completingMock()
	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	a2aSendMessage(t, ts, "ctx-filter", "msg1")
	a2aSendMessage(t, ts, "ctx-filter", "msg2")

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
	if len(listResp.Tasks) != 2 {
		t.Fatalf("got %d tasks, want 2 (status filter should be ignored)", len(listResp.Tasks))
	}
}

func TestServer_GetTask_HistoryLengthIgnored(t *testing.T) {
	mock := completingMock()
	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	task := a2aSendMessage(t, ts, "ctx-histlen", "Hello")

	histLen := 0
	got := a2aRPCRequestTask(t, ts, a2a.MethodGetTask, a2a.GetTaskRequest{
		ID:            task.ID,
		HistoryLength: &histLen,
	})

	if got.ID != task.ID {
		t.Fatalf("task ID = %q, want %q", got.ID, task.ID)
	}
	if got.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("state = %q, want completed", got.Status.State)
	}
}

// --- Group D: Error Handling at Boundaries ---

func TestServer_SendMessage_FailedTaskErrorParts(t *testing.T) {
	mock := &mockConv{
		sendFunc: func(_ context.Context, _ any) (SendResult, error) {
			return nil, errors.New("provider crashed")
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
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

func TestServer_SendMessage_CancelDuringProcessing(t *testing.T) {
	sendStarted := make(chan struct{})
	mock := &mockConv{
		sendFunc: func(ctx context.Context, _ any) (SendResult, error) {
			close(sendStarted)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	task := a2aRPCRequestTask(t, ts, a2a.MethodSendMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-cancel-processing",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("slow")}},
		},
	})

	<-sendStarted

	cancelResp := a2aRPCRequest(t, ts, a2a.MethodCancelTask, a2a.CancelTaskRequest{ID: task.ID})
	if cancelResp.Error != nil {
		t.Fatalf("cancel error: %d %s", cancelResp.Error.Code, cancelResp.Error.Message)
	}

	time.Sleep(50 * time.Millisecond)

	got := a2aRPCRequestTask(t, ts, a2a.MethodGetTask, a2a.GetTaskRequest{ID: task.ID})
	if got.Status.State != a2a.TaskStateCanceled {
		t.Fatalf("state = %q, want canceled (not failed)", got.Status.State)
	}
}

func TestServer_SendMessage_DataPartRejected(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) {
		return completingMock(), nil
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

func TestServer_SendMessage_ConversationReuseMultimodal(t *testing.T) {
	var callCount atomic.Int32
	imgURL := "https://example.com/img.png"

	mock := &mockConv{
		sendFunc: func(_ context.Context, message any) (SendResult, error) {
			n := callCount.Add(1)
			msg := message.(*types.Message)
			if len(msg.Parts) != 2 {
				return nil, fmt.Errorf("call %d: expected 2 parts, got %d", n, len(msg.Parts))
			}
			return &mockSendResult{
				parts: []types.ContentPart{
					types.NewTextPart(fmt.Sprintf("response %d", n)),
					{Type: types.ContentTypeImage, Media: &types.MediaContent{URL: &imgURL, MIMEType: "image/png"}},
				},
			}, nil
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

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

func TestServerDefaultTimeouts(t *testing.T) {
	srv := NewServer(func(string) (Conversation, error) {
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

func TestServerCustomTimeouts(t *testing.T) {
	srv := NewServer(
		func(string) (Conversation, error) { return nil, nil },
		WithReadTimeout(5*time.Second),
		WithWriteTimeout(10*time.Second),
		WithIdleTimeout(15*time.Second),
		WithMaxBodySize(1024),
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

func TestServerMaxBodySizeRejectsOversizedRequest(t *testing.T) {
	opener := func(string) (Conversation, error) {
		return &mockConv{
			sendFunc: func(context.Context, any) (SendResult, error) {
				return &mockSendResult{
					parts: []types.ContentPart{types.NewTextPart("ok")},
					text:  "ok",
				}, nil
			},
		}, nil
	}

	srv := NewServer(opener, WithMaxBodySize(16))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

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

func TestServerListenAndServeTimeouts(t *testing.T) {
	srv := NewServer(
		func(string) (Conversation, error) { return nil, nil },
		WithReadTimeout(15*time.Second),
		WithWriteTimeout(30*time.Second),
		WithIdleTimeout(60*time.Second),
	)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	go func() { _ = srv.Serve(ln) }()

	addr := ln.Addr().String()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, dialErr := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if dialErr == nil {
			conn.Close()
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if srv.readTimeout != 15*time.Second {
		t.Errorf("readTimeout = %v, want 15s", srv.readTimeout)
	}
	if srv.writeTimeout != 30*time.Second {
		t.Errorf("writeTimeout = %v, want 30s", srv.writeTimeout)
	}
	if srv.idleTimeout != 60*time.Second {
		t.Errorf("idleTimeout = %v, want 60s", srv.idleTimeout)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

func TestServerListenAndServe(t *testing.T) {
	mock := completingMock()
	srv := NewServer(
		func(string) (Conversation, error) { return mock, nil },
		WithPort(0),
		WithReadTimeout(15*time.Second),
		WithWriteTimeout(30*time.Second),
		WithIdleTimeout(60*time.Second),
	)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	var addr string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		srv.httpSrvMu.Lock()
		if srv.httpSrv != nil {
			addr = srv.httpSrv.Addr
		}
		srv.httpSrvMu.Unlock()
		if addr != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if addr == "" {
		t.Fatal("httpSrv was never set by ListenAndServe")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("ListenAndServe returned unexpected error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ListenAndServe did not return after Shutdown")
	}
}

func TestServerListenAndServeWithTraffic(t *testing.T) {
	mock := completingMock()
	srv := NewServer(
		func(string) (Conversation, error) { return mock, nil },
		WithPort(0),
	)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	addr := ln.Addr().String()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, dialErr := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if dialErr == nil {
			conn.Close()
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	text := "hello"
	reqBody := a2a.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  a2a.MethodSendMessage,
	}
	params, _ := json.Marshal(a2a.SendMessageRequest{
		Message: a2a.Message{
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: &text}},
			ContextID: "ctx-listen-serve",
		},
	})
	reqBody.Params = params
	body, _ := json.Marshal(reqBody)

	resp, err := http.Post("http://"+addr+"/a2a", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /a2a: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	if !mock.closed.Load() {
		t.Fatal("conversation was not closed during shutdown")
	}

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("Serve returned unexpected error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return after Shutdown")
	}
}

func TestServerShutdownWithNilHTTPServer(t *testing.T) {
	mock := &mockConv{
		sendFunc: func(_ context.Context, _ any) (SendResult, error) {
			return &mockSendResult{
				parts: []types.ContentPart{types.NewTextPart("done")},
				text:  "done",
			}, nil
		},
	}
	srv := NewServer(func(string) (Conversation, error) { return mock, nil })

	srv.convsMu.Lock()
	srv.convs["ctx-1"] = mock
	srv.convsMu.Unlock()

	cancelled := false
	srv.cancelsMu.Lock()
	srv.cancels["task-1"] = func() { cancelled = true }
	srv.cancelsMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := srv.Shutdown(ctx)
	if err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	if !cancelled {
		t.Error("in-flight cancel func was not invoked")
	}
	if !mock.closed.Load() {
		t.Error("conversation was not closed")
	}

	srv.cancelsMu.Lock()
	if len(srv.cancels) != 0 {
		t.Error("cancels map not cleared")
	}
	srv.cancelsMu.Unlock()

	srv.convsMu.RLock()
	if len(srv.convs) != 0 {
		t.Error("convs map not cleared")
	}
	srv.convsMu.RUnlock()
}

func TestServerShutdownConvCloseError(t *testing.T) {
	wantErr := errors.New("close failed")
	mock := &mockConv{
		sendFunc: func(_ context.Context, _ any) (SendResult, error) {
			return &mockSendResult{
				parts: []types.ContentPart{types.NewTextPart("ok")},
				text:  "ok",
			}, nil
		},
	}
	srv := NewServer(func(string) (Conversation, error) { return mock, nil })

	failConv := &errorClosingConv{closeErr: wantErr}
	srv.convsMu.Lock()
	srv.convs["ctx-err"] = failConv
	srv.convsMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := srv.Shutdown(ctx)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Shutdown error = %v, want %v", err, wantErr)
	}
}
