package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/testutil"
	"github.com/AltairaLabs/PromptKit/runtime/telemetry"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// --- test helpers ---

func rpcResult(w http.ResponseWriter, id any, result any) {
	b, _ := json.Marshal(result)
	_ = json.NewEncoder(w).Encode(JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  b,
	})
}

func rpcErrorResp(w http.ResponseWriter, id any, code int, msg string) {
	_ = json.NewEncoder(w).Encode(JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &JSONRPCError{Code: code, Message: msg},
	})
}

func decodeRPC(r *http.Request) JSONRPCRequest {
	var req JSONRPCRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	return req
}

func sseEvent(data any) string {
	b, _ := json.Marshal(data)
	return fmt.Sprintf("data: %s\n\n", b)
}

// --- unit tests ---

func TestDiscover(t *testing.T) {
	want := AgentCard{Name: "test-agent", Description: "A test agent"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/agent.json" {
			t.Errorf("path = %q, want /.well-known/agent.json", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		_ = json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	got, err := c.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if got.Name != want.Name {
		t.Errorf("Name = %q, want %q", got.Name, want.Name)
	}
	if got.Description != want.Description {
		t.Errorf("Description = %q, want %q", got.Description, want.Description)
	}
}

func TestDiscover_Caching(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		_ = json.NewEncoder(w).Encode(AgentCard{Name: "cached"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)

	card1, err := c.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	card2, err := c.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("expected 1 HTTP call, got %d", n)
	}
	if card1.Name != card2.Name {
		t.Errorf("cached card mismatch: %q != %q", card1.Name, card2.Name)
	}
}

func TestSendMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPC(r)
		if req.Method != MethodSendMessage {
			t.Errorf("method = %q, want %q", req.Method, MethodSendMessage)
		}
		rpcResult(w, req.ID, Task{
			ID:     "task-1",
			Status: TaskStatus{State: TaskStateWorking},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	task, err := c.SendMessage(context.Background(), &SendMessageRequest{
		Message: Message{
			Role:  RoleUser,
			Parts: []Part{{Text: testutil.Ptr("hello")}},
		},
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	if task.ID != "task-1" {
		t.Errorf("task.ID = %q, want 'task-1'", task.ID)
	}
	if task.Status.State != TaskStateWorking {
		t.Errorf("state = %q, want %q", task.Status.State, TaskStateWorking)
	}
}

func TestGetTask(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPC(r)
		if req.Method != MethodGetTask {
			t.Errorf("method = %q, want %q", req.Method, MethodGetTask)
		}
		var params GetTaskRequest
		_ = json.Unmarshal(req.Params, &params)
		rpcResult(w, req.ID, Task{
			ID:     params.ID,
			Status: TaskStatus{State: TaskStateCompleted},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	task, err := c.GetTask(context.Background(), "task-42")
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.ID != "task-42" {
		t.Errorf("task.ID = %q, want 'task-42'", task.ID)
	}
	if task.Status.State != TaskStateCompleted {
		t.Errorf("state = %q, want %q", task.Status.State, TaskStateCompleted)
	}
}

func TestCancelTask(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPC(r)
		if req.Method != MethodCancelTask {
			t.Errorf("method = %q, want %q", req.Method, MethodCancelTask)
		}
		rpcResult(w, req.ID, Task{
			ID:     "task-1",
			Status: TaskStatus{State: TaskStateCanceled},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	if err := c.CancelTask(context.Background(), "task-1"); err != nil {
		t.Fatalf("CancelTask() error = %v", err)
	}
}

func TestListTasks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPC(r)
		if req.Method != MethodListTasks {
			t.Errorf("method = %q, want %q", req.Method, MethodListTasks)
		}
		var params ListTasksRequest
		_ = json.Unmarshal(req.Params, &params)
		if params.ContextID != "ctx-1" {
			t.Errorf("ContextID = %q, want 'ctx-1'", params.ContextID)
		}
		rpcResult(w, req.ID, ListTasksResponse{
			Tasks: []Task{
				{ID: "t1", Status: TaskStatus{State: TaskStateCompleted}},
				{ID: "t2", Status: TaskStatus{State: TaskStateWorking}},
			},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	tasks, err := c.ListTasks(context.Background(), &ListTasksRequest{ContextID: "ctx-1"})
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("len(tasks) = %d, want 2", len(tasks))
	}
	if tasks[0].ID != "t1" {
		t.Errorf("tasks[0].ID = %q, want 't1'", tasks[0].ID)
	}
	if tasks[1].ID != "t2" {
		t.Errorf("tasks[1].ID = %q, want 't2'", tasks[1].ID)
	}
}

func TestSendMessageStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)

		fmt.Fprint(w, sseEvent(TaskStatusUpdateEvent{
			TaskID: "task-1",
			Status: TaskStatus{State: TaskStateWorking},
		}))
		f.Flush()

		fmt.Fprint(w, sseEvent(TaskArtifactUpdateEvent{
			TaskID:   "task-1",
			Artifact: Artifact{ArtifactID: "a1", Parts: []Part{{Text: testutil.Ptr("result")}}},
		}))
		f.Flush()

		fmt.Fprint(w, sseEvent(TaskStatusUpdateEvent{
			TaskID: "task-1",
			Status: TaskStatus{State: TaskStateCompleted},
		}))
		f.Flush()
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	ch, err := c.SendMessageStream(context.Background(), &SendMessageRequest{
		Message: Message{Role: RoleUser, Parts: []Part{{Text: testutil.Ptr("hello")}}},
	})
	if err != nil {
		t.Fatalf("SendMessageStream() error = %v", err)
	}

	var events []StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
	if events[0].StatusUpdate == nil || events[0].StatusUpdate.Status.State != TaskStateWorking {
		t.Error("event 0: expected status=working")
	}
	if events[1].ArtifactUpdate == nil || events[1].ArtifactUpdate.Artifact.ArtifactID != "a1" {
		t.Error("event 1: expected artifact a1")
	}
	if events[2].StatusUpdate == nil || events[2].StatusUpdate.Status.State != TaskStateCompleted {
		t.Error("event 2: expected status=completed")
	}
}

func TestSendMessageStream_JSONRPCWrapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)

		writeWrapped := func(result any) {
			b, _ := json.Marshal(result)
			envelope := JSONRPCResponse{JSONRPC: "2.0", ID: float64(1), Result: b}
			fmt.Fprint(w, sseEvent(envelope))
			f.Flush()
		}

		writeWrapped(TaskStatusUpdateEvent{
			TaskID: "t1",
			Status: TaskStatus{State: TaskStateWorking},
		})
		writeWrapped(TaskArtifactUpdateEvent{
			TaskID:   "t1",
			Artifact: Artifact{ArtifactID: "a1", Parts: []Part{{Text: testutil.Ptr("chunk")}}},
		})
		writeWrapped(TaskStatusUpdateEvent{
			TaskID: "t1",
			Status: TaskStatus{State: TaskStateCompleted},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	ch, err := c.SendMessageStream(context.Background(), &SendMessageRequest{
		Message: Message{Role: RoleUser, Parts: []Part{{Text: testutil.Ptr("go")}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var events []StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
	if events[0].StatusUpdate == nil {
		t.Error("event 0: expected StatusUpdate")
	}
	if events[1].ArtifactUpdate == nil {
		t.Error("event 1: expected ArtifactUpdate")
	}
	if events[2].StatusUpdate == nil || events[2].StatusUpdate.Status.State != TaskStateCompleted {
		t.Error("event 2: expected StatusUpdate completed")
	}
}

func TestWithAuth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(AgentCard{Name: "secure"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, WithAuth("Bearer", "secret-token"))
	_, err := c.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer secret-token" {
		t.Errorf("Authorization = %q, want 'Bearer secret-token'", gotAuth)
	}
}

func TestRPCError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPC(r)
		rpcErrorResp(w, req.ID, -32600, "Invalid Request")
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.SendMessage(context.Background(), &SendMessageRequest{
		Message: Message{Role: RoleUser, Parts: []Part{{Text: testutil.Ptr("hi")}}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected *RPCError, got %T: %v", err, err)
	}
	if rpcErr.Code != -32600 {
		t.Errorf("Code = %d, want -32600", rpcErr.Code)
	}
	if rpcErr.Message != "Invalid Request" {
		t.Errorf("Message = %q, want 'Invalid Request'", rpcErr.Message)
	}
}

func TestContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // block until client disconnects
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	c := NewClient(srv.URL)
	_, err := c.Discover(ctx)
	if err == nil {
		t.Fatal("expected context deadline error")
	}
}

func TestWithHTTPClient(t *testing.T) {
	custom := &http.Client{Timeout: 30 * time.Second}
	c := NewClient("http://example.com", WithHTTPClient(custom))
	if c.httpClient != custom {
		t.Error("expected custom HTTP client")
	}
}

func TestRPCError_ErrorString(t *testing.T) {
	err := &RPCError{Code: -32600, Message: "bad request"}
	got := err.Error()
	if got != "a2a: rpc error -32600: bad request" {
		t.Errorf("Error() = %q", got)
	}
}

func TestDiscover_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.Discover(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSendMessageStream_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.SendMessageStream(context.Background(), &SendMessageRequest{
		Message: Message{Role: RoleUser, Parts: []Part{{Text: testutil.Ptr("hi")}}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRPCCall_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.GetTask(context.Background(), "task-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- integration tests ---

func TestHappyPath_DiscoverSendPollComplete(t *testing.T) {
	var pollCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent.json":
			_ = json.NewEncoder(w).Encode(AgentCard{Name: "agent"})
		case "/a2a":
			req := decodeRPC(r)
			switch req.Method {
			case MethodSendMessage:
				rpcResult(w, req.ID, Task{
					ID:        "task-1",
					ContextID: "ctx-1",
					Status:    TaskStatus{State: TaskStateWorking},
				})
			case MethodGetTask:
				n := atomic.AddInt32(&pollCount, 1)
				task := Task{ID: "task-1"}
				if n >= 2 {
					task.Status = TaskStatus{State: TaskStateCompleted}
					task.Artifacts = []Artifact{{
						ArtifactID: "a1",
						Parts:      []Part{{Text: testutil.Ptr("done")}},
					}}
				} else {
					task.Status = TaskStatus{State: TaskStateWorking}
				}
				rpcResult(w, req.ID, task)
			}
		}
	}))
	defer srv.Close()

	ctx := context.Background()
	c := NewClient(srv.URL)

	// 1. Discover
	card, err := c.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if card.Name != "agent" {
		t.Errorf("agent name = %q", card.Name)
	}

	// 2. Send message
	task, err := c.SendMessage(ctx, &SendMessageRequest{
		Message: Message{Role: RoleUser, Parts: []Part{{Text: testutil.Ptr("solve this")}}},
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if task.Status.State != TaskStateWorking {
		t.Fatalf("expected working, got %s", task.Status.State)
	}

	// 3. Poll — first call: still working
	task, err = c.GetTask(ctx, "task-1")
	if err != nil {
		t.Fatalf("GetTask(1): %v", err)
	}
	if task.Status.State != TaskStateWorking {
		t.Fatalf("expected working, got %s", task.Status.State)
	}

	// 4. Poll — second call: completed with artifact
	task, err = c.GetTask(ctx, "task-1")
	if err != nil {
		t.Fatalf("GetTask(2): %v", err)
	}
	if task.Status.State != TaskStateCompleted {
		t.Fatalf("expected completed, got %s", task.Status.State)
	}
	if len(task.Artifacts) == 0 || *task.Artifacts[0].Parts[0].Text != "done" {
		t.Error("expected artifact with text 'done'")
	}
}

func TestErrorPath_DiscoverSendFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent.json":
			_ = json.NewEncoder(w).Encode(AgentCard{Name: "agent"})
		case "/a2a":
			req := decodeRPC(r)
			switch req.Method {
			case MethodSendMessage:
				rpcResult(w, req.ID, Task{
					ID:     "task-1",
					Status: TaskStatus{State: TaskStateWorking},
				})
			case MethodGetTask:
				rpcResult(w, req.ID, Task{
					ID:     "task-1",
					Status: TaskStatus{State: TaskStateFailed},
				})
			}
		}
	}))
	defer srv.Close()

	ctx := context.Background()
	c := NewClient(srv.URL)

	_, err := c.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	task, err := c.SendMessage(ctx, &SendMessageRequest{
		Message: Message{Role: RoleUser, Parts: []Part{{Text: testutil.Ptr("do something")}}},
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if task.Status.State != TaskStateWorking {
		t.Fatalf("expected working, got %s", task.Status.State)
	}

	task, err = c.GetTask(ctx, "task-1")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status.State != TaskStateFailed {
		t.Errorf("expected failed, got %s", task.Status.State)
	}
}

func TestClient_PropagatesTraceHeaders(t *testing.T) {
	// Configure OTel propagation so Inject writes W3C traceparent/tracestate.
	origProp := otel.GetTextMapPropagator()
	defer otel.SetTextMapPropagator(origProp)
	telemetry.SetupPropagation()

	// Create a real span context to propagate.
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-parent")
	defer span.End()

	sc := trace.SpanContextFromContext(ctx)
	wantTraceID := sc.TraceID().String()

	var gotTP string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTP = r.Header.Get("traceparent")

		switch r.URL.Path {
		case "/.well-known/agent.json":
			_ = json.NewEncoder(w).Encode(AgentCard{Name: "agent"})
		case "/a2a":
			req := decodeRPC(r)
			rpcResult(w, req.ID, Task{
				ID:     "task-1",
				Status: TaskStatus{State: TaskStateCompleted},
			})
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL)

	// Test Discover propagates trace headers.
	_, err := c.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if gotTP == "" {
		t.Fatal("Discover: expected traceparent header, got empty")
	}
	// Verify the traceparent contains the correct trace ID.
	if len(gotTP) < 36 || gotTP[3:35] != wantTraceID {
		t.Errorf("Discover: traceparent trace ID = %q, want %q", gotTP, wantTraceID)
	}

	// Reset cached card to test rpcCall path.
	c.mu.Lock()
	c.agentCard = nil
	c.mu.Unlock()
	gotTP = ""

	// Test SendMessage (rpcCall) propagates trace headers.
	_, err = c.SendMessage(ctx, &SendMessageRequest{
		Message: Message{Role: RoleUser, Parts: []Part{{Text: testutil.Ptr("hi")}}},
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if gotTP == "" {
		t.Fatal("SendMessage: expected traceparent header, got empty")
	}
	if len(gotTP) < 36 || gotTP[3:35] != wantTraceID {
		t.Errorf("SendMessage: traceparent trace ID = %q, want %q", gotTP, wantTraceID)
	}

	// Verify that without a span context, no headers are injected.
	c.mu.Lock()
	c.agentCard = nil
	c.mu.Unlock()
	gotTP = ""

	_, err = c.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover (no span): %v", err)
	}
	if gotTP != "" {
		t.Errorf("expected no traceparent without span context, got %q", gotTP)
	}
}
