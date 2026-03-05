package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// okTask returns a simple completed task for test servers.
func okTask(id string) *Task {
	text := "ok"
	return &Task{
		ID: id,
		Status: TaskStatus{
			State:   TaskStateCompleted,
			Message: &Message{Role: RoleAgent, Parts: []Part{{Text: &text}}},
		},
	}
}

func TestExecutor_RetryOn502(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		req := decodeRPC(r)
		if n <= 2 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		rpcResult(w, req.ID, okTask("task-retry-502"))
	}))
	defer srv.Close()

	e := NewExecutor(WithRetryPolicy(RetryPolicy{
		MaxRetries:   3,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
	}))
	defer e.Close()
	desc := &tools.ToolDescriptor{
		Name:      "test-tool",
		A2AConfig: &tools.A2AConfig{AgentURL: srv.URL},
	}

	result, err := e.Execute(context.Background(), desc, json.RawMessage(`{"query":"hello"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out map[string]string
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out["response"] != "ok" {
		t.Errorf("response = %q, want %q", out["response"], "ok")
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
}

func TestExecutor_RetryOn503(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		req := decodeRPC(r)
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		rpcResult(w, req.ID, okTask("task-retry-503"))
	}))
	defer srv.Close()

	e := NewExecutor(WithRetryPolicy(RetryPolicy{
		MaxRetries:   2,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
	}))
	defer e.Close()
	desc := &tools.ToolDescriptor{
		Name:      "test-tool",
		A2AConfig: &tools.A2AConfig{AgentURL: srv.URL},
	}

	_, err := e.Execute(context.Background(), desc, json.RawMessage(`{"query":"hello"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Errorf("attempts = %d, want 2", got)
	}
}

func TestExecutor_RetryOn504(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		req := decodeRPC(r)
		if n == 1 {
			w.WriteHeader(http.StatusGatewayTimeout)
			return
		}
		rpcResult(w, req.ID, okTask("task-retry-504"))
	}))
	defer srv.Close()

	e := NewExecutor(WithRetryPolicy(RetryPolicy{
		MaxRetries:   2,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
	}))
	defer e.Close()
	desc := &tools.ToolDescriptor{
		Name:      "test-tool",
		A2AConfig: &tools.A2AConfig{AgentURL: srv.URL},
	}

	_, err := e.Execute(context.Background(), desc, json.RawMessage(`{"query":"hello"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Errorf("attempts = %d, want 2", got)
	}
}

func TestExecutor_NoRetryOn400(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	e := NewExecutor(WithRetryPolicy(RetryPolicy{
		MaxRetries:   3,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
	}))
	defer e.Close()
	desc := &tools.ToolDescriptor{
		Name:      "test-tool",
		A2AConfig: &tools.A2AConfig{AgentURL: srv.URL},
	}

	_, err := e.Execute(context.Background(), desc, json.RawMessage(`{"query":"hello"}`))
	if err == nil {
		t.Fatal("expected error for 400")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1 (no retry on 400)", got)
	}
}

func TestExecutor_NoRetryOnRPCError(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		req := decodeRPC(r)
		rpcErrorResp(w, req.ID, -32600, "invalid request")
	}))
	defer srv.Close()

	e := NewExecutor(WithRetryPolicy(RetryPolicy{
		MaxRetries:   3,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
	}))
	defer e.Close()
	desc := &tools.ToolDescriptor{
		Name:      "test-tool",
		A2AConfig: &tools.A2AConfig{AgentURL: srv.URL},
	}

	_, err := e.Execute(context.Background(), desc, json.RawMessage(`{"query":"hello"}`))
	if err == nil {
		t.Fatal("expected error for RPC error")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1 (no retry on RPC error)", got)
	}
}

func TestExecutor_RetryExhausted(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	e := NewExecutor(WithRetryPolicy(RetryPolicy{
		MaxRetries:   2,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
	}))
	defer e.Close()
	desc := &tools.ToolDescriptor{
		Name:      "test-tool",
		A2AConfig: &tools.A2AConfig{AgentURL: srv.URL},
	}

	_, err := e.Execute(context.Background(), desc, json.RawMessage(`{"query":"hello"}`))
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	// MaxRetries=2 means 3 total attempts (1 initial + 2 retries).
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
}

func TestExecutor_NoRetryOnContextCanceled(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	e := NewExecutor(WithRetryPolicy(RetryPolicy{
		MaxRetries:   3,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
	}))
	defer e.Close()
	desc := &tools.ToolDescriptor{
		Name:      "test-tool",
		A2AConfig: &tools.A2AConfig{AgentURL: srv.URL},
	}

	_, err := e.Execute(ctx, desc, json.RawMessage(`{"query":"hello"}`))
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestExecutor_WithNoRetry(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	e := NewExecutor(WithNoRetry())
	defer e.Close()
	desc := &tools.ToolDescriptor{
		Name:      "test-tool",
		A2AConfig: &tools.A2AConfig{AgentURL: srv.URL},
	}

	_, err := e.Execute(context.Background(), desc, json.RawMessage(`{"query":"hello"}`))
	if err == nil {
		t.Fatal("expected error")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1 (no retry)", got)
	}
}

func TestExecutor_RetryOnConnectionRefused(t *testing.T) {
	// Use a listener that we close immediately to get connection refused.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close() // Close so connections are refused.

	e := NewExecutor(WithRetryPolicy(RetryPolicy{
		MaxRetries:   1,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
	}))
	defer e.Close()
	desc := &tools.ToolDescriptor{
		Name:      "test-tool",
		A2AConfig: &tools.A2AConfig{AgentURL: "http://" + addr},
	}

	_, execErr := e.Execute(context.Background(), desc, json.RawMessage(`{"query":"hello"}`))
	if execErr == nil {
		t.Fatal("expected error for connection refused")
	}
}

func TestExecutor_DefaultRetryPolicy(t *testing.T) {
	p := DefaultRetryPolicy()
	if p.MaxRetries != DefaultA2AMaxRetries {
		t.Errorf("MaxRetries = %d, want %d", p.MaxRetries, DefaultA2AMaxRetries)
	}
	if p.InitialDelay != DefaultA2AInitialDelay {
		t.Errorf("InitialDelay = %v, want %v", p.InitialDelay, DefaultA2AInitialDelay)
	}
	if p.MaxDelay != DefaultA2AMaxDelay {
		t.Errorf("MaxDelay = %v, want %v", p.MaxDelay, DefaultA2AMaxDelay)
	}
}

func TestIsA2ARetryableError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{"nil error", nil, false},
		{"context canceled", context.Canceled, false},
		{"context deadline", context.DeadlineExceeded, false},
		{"HTTP 502", &HTTPStatusError{StatusCode: 502, Method: "test"}, true},
		{"HTTP 503", &HTTPStatusError{StatusCode: 503, Method: "test"}, true},
		{"HTTP 504", &HTTPStatusError{StatusCode: 504, Method: "test"}, true},
		{"HTTP 429", &HTTPStatusError{StatusCode: 429, Method: "test"}, true},
		{"HTTP 400", &HTTPStatusError{StatusCode: 400, Method: "test"}, false},
		{"HTTP 401", &HTTPStatusError{StatusCode: 401, Method: "test"}, false},
		{"HTTP 403", &HTTPStatusError{StatusCode: 403, Method: "test"}, false},
		{"HTTP 404", &HTTPStatusError{StatusCode: 404, Method: "test"}, false},
		{"RPC error", &RPCError{Code: -32600, Message: "bad"}, false},
		{"net.OpError", &net.OpError{Op: "dial", Err: errors.New("refused")}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isA2ARetryableError(tt.err); got != tt.retryable {
				t.Errorf("isA2ARetryableError() = %v, want %v", got, tt.retryable)
			}
		})
	}
}

func TestA2ACalculateBackoff(t *testing.T) {
	policy := RetryPolicy{
		MaxRetries:   3,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     5 * time.Second,
	}

	// Attempt 0: base delay ~100ms (+ jitter up to 50ms).
	d0 := a2aCalculateBackoff(policy, 0)
	if d0 < 100*time.Millisecond || d0 > 200*time.Millisecond {
		t.Errorf("attempt 0 delay = %v, want [100ms, 200ms]", d0)
	}

	// Attempt 1: base delay ~200ms (+ jitter up to 100ms).
	d1 := a2aCalculateBackoff(policy, 1)
	if d1 < 200*time.Millisecond || d1 > 400*time.Millisecond {
		t.Errorf("attempt 1 delay = %v, want [200ms, 400ms]", d1)
	}

	// Attempt 2: base delay ~400ms (+ jitter up to 200ms).
	d2 := a2aCalculateBackoff(policy, 2)
	if d2 < 400*time.Millisecond || d2 > 800*time.Millisecond {
		t.Errorf("attempt 2 delay = %v, want [400ms, 800ms]", d2)
	}
}

func TestA2ACalculateBackoff_CapsAtMaxDelay(t *testing.T) {
	policy := RetryPolicy{
		MaxRetries:   10,
		InitialDelay: 1 * time.Second,
		MaxDelay:     2 * time.Second,
	}

	// Attempt 5 would be 32s without cap, but should be capped at 2s + jitter.
	d := a2aCalculateBackoff(policy, 5)
	// Max possible: 2s + 50% jitter = 3s
	if d > 3*time.Second {
		t.Errorf("delay = %v, should be capped at ~3s (2s + jitter)", d)
	}
}

func TestA2ACalculateBackoff_DefaultsOnZero(t *testing.T) {
	policy := RetryPolicy{} // All zeros.

	d := a2aCalculateBackoff(policy, 0)
	// Should use DefaultA2AInitialDelay (500ms) + jitter.
	if d < DefaultA2AInitialDelay {
		t.Errorf("delay = %v, expected >= %v", d, DefaultA2AInitialDelay)
	}
}

func TestHTTPStatusError(t *testing.T) {
	err := &HTTPStatusError{StatusCode: 502, Method: "message/send"}
	want := "a2a: message/send: status 502"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestExecutor_RetryOn429(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		req := decodeRPC(r)
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		rpcResult(w, req.ID, okTask("task-retry-429"))
	}))
	defer srv.Close()

	e := NewExecutor(WithRetryPolicy(RetryPolicy{
		MaxRetries:   2,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
	}))
	defer e.Close()
	desc := &tools.ToolDescriptor{
		Name:      "test-tool",
		A2AConfig: &tools.A2AConfig{AgentURL: srv.URL},
	}

	_, err := e.Execute(context.Background(), desc, json.RawMessage(`{"query":"hello"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Errorf("attempts = %d, want 2", got)
	}
}

func TestExecutor_RetrySucceedsFirstAttempt(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		req := decodeRPC(r)
		rpcResult(w, req.ID, okTask("task-first-attempt"))
	}))
	defer srv.Close()

	e := NewExecutor(WithRetryPolicy(RetryPolicy{
		MaxRetries:   3,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
	}))
	defer e.Close()
	desc := &tools.ToolDescriptor{
		Name:      "test-tool",
		A2AConfig: &tools.A2AConfig{AgentURL: srv.URL},
	}

	_, err := e.Execute(context.Background(), desc, json.RawMessage(`{"query":"hello"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1 (success on first try)", got)
	}
}
