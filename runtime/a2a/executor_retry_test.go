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

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
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

// noCard serves an agent without an agent card: the executor's card
// discovery (two GETs, current and legacy path) gets 404s and falls back to
// {base}/a2a, and only JSON-RPC POSTs reach h, so h counts call attempts
// alone.
func noCard(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			http.NotFound(w, r)
			return
		}
		h(w, r)
	})
}

func TestExecutor_RetryOn502(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(noCard(func(w http.ResponseWriter, r *http.Request) {
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
	srv := httptest.NewServer(noCard(func(w http.ResponseWriter, r *http.Request) {
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
	srv := httptest.NewServer(noCard(func(w http.ResponseWriter, r *http.Request) {
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
	srv := httptest.NewServer(noCard(func(w http.ResponseWriter, _ *http.Request) {
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
	srv := httptest.NewServer(noCard(func(w http.ResponseWriter, r *http.Request) {
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
	srv := httptest.NewServer(noCard(func(w http.ResponseWriter, _ *http.Request) {
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
	srv := httptest.NewServer(noCard(func(w http.ResponseWriter, _ *http.Request) {
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
	srv := httptest.NewServer(noCard(func(w http.ResponseWriter, _ *http.Request) {
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

// backoffSamples draws n delays for one attempt.
func backoffSamples(policy RetryPolicy, attempt, n int) []time.Duration {
	out := make([]time.Duration, n)
	for i := range out {
		out[i] = a2aBackoff(policy).Delay(attempt)
	}
	return out
}

// requireJitteredWithin fails unless every delay lies in [base/2, base] and
// the delays are not all equal.
func requireJitteredWithin(t *testing.T, delays []time.Duration, base time.Duration) {
	t.Helper()
	for _, d := range delays {
		if d < base/2 || d > base {
			t.Fatalf("delay = %v, want within [%v, %v]", d, base/2, base)
		}
	}
	for _, d := range delays[1:] {
		if d != delays[0] {
			return
		}
	}
	t.Fatalf("all %d delays are %v: no jitter", len(delays), delays[0])
}

func TestA2ACalculateBackoff(t *testing.T) {
	policy := RetryPolicy{
		MaxRetries:   3,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     5 * time.Second,
	}

	// Below the cap the base delay doubles per attempt; jitter shortens it by
	// up to half.
	requireJitteredWithin(t, backoffSamples(policy, 0, 50), 100*time.Millisecond)
	requireJitteredWithin(t, backoffSamples(policy, 1, 50), 200*time.Millisecond)
	requireJitteredWithin(t, backoffSamples(policy, 2, 50), 400*time.Millisecond)
}

func TestA2ACalculateBackoff_CapsAtMaxDelay(t *testing.T) {
	policy := RetryPolicy{
		MaxRetries:   10,
		InitialDelay: 1 * time.Second,
		MaxDelay:     2 * time.Second,
	}

	// Attempt 5 would be 32s without the cap. No delay exceeds MaxDelay, and
	// delays at the cap still vary, so retries do not fall into step.
	requireJitteredWithin(t, backoffSamples(policy, 5, 50), policy.MaxDelay)
}

func TestA2ACalculateBackoff_LargeAttemptStaysCapped(t *testing.T) {
	policy := RetryPolicy{InitialDelay: time.Second, MaxDelay: 3 * time.Second}

	// 2^100 seconds overflows time.Duration; the cap must still hold.
	requireJitteredWithin(t, backoffSamples(policy, 100, 50), policy.MaxDelay)
}

func TestA2ACalculateBackoff_DefaultsOnZero(t *testing.T) {
	policy := RetryPolicy{} // All zeros.

	// Uses DefaultA2AInitialDelay, shortened by jitter.
	requireJitteredWithin(t, backoffSamples(policy, 0, 50), DefaultA2AInitialDelay)
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
	srv := httptest.NewServer(noCard(func(w http.ResponseWriter, r *http.Request) {
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
	srv := httptest.NewServer(noCard(func(w http.ResponseWriter, r *http.Request) {
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

// A context canceled while the executor waits to retry ends the call at once:
// one attempt reaches the agent, and the backoff is not waited out.
func TestExecutor_CancelDuringBackoffStopsRetrying(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var attempts int32
	answered := make(chan struct{}, 1)
	srv := httptest.NewServer(noCard(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
		answered <- struct{}{}
	}))
	defer srv.Close()
	// Cancel once the first 503 is in and the executor is waiting to retry.
	go func() {
		<-answered
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	e := NewExecutor(WithRetryPolicy(RetryPolicy{MaxRetries: 5, InitialDelay: time.Hour, MaxDelay: time.Hour}))
	defer e.Close()
	desc := &tools.ToolDescriptor{Name: "test-tool", A2AConfig: &tools.A2AConfig{AgentURL: srv.URL}}

	start := time.Now()
	_, err := e.Execute(ctx, desc, json.RawMessage(`{"query":"hello"}`))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1", got)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("took %v: the backoff was waited out", elapsed)
	}
}

// When every attempt fails with a retryable status, the error names the last
// status the agent sent.
func TestExecutor_RetryExhaustedReturnsTheLastStatus(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(noCard(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&attempts, 1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	defer srv.Close()

	e := NewExecutor(WithRetryPolicy(RetryPolicy{MaxRetries: 2, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond}))
	defer e.Close()
	desc := &tools.ToolDescriptor{Name: "test-tool", A2AConfig: &tools.A2AConfig{AgentURL: srv.URL}}

	_, err := e.Execute(context.Background(), desc, json.RawMessage(`{"query":"hello"}`))
	var httpErr *HTTPStatusError
	if !errors.As(err, &httpErr) {
		t.Fatalf("err = %v, want an *HTTPStatusError", err)
	}
	if httpErr.StatusCode != http.StatusGatewayTimeout {
		t.Errorf("status = %d, want %d (the last attempt's)", httpErr.StatusCode, http.StatusGatewayTimeout)
	}
}

// A 429's Retry-After reaches the HTTPStatusError the client returns.
func TestClient_RateLimitCarriesRetryAfter(t *testing.T) {
	srv := httptest.NewServer(noCard(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	e := NewExecutor(WithNoRetry())
	defer e.Close()
	desc := &tools.ToolDescriptor{Name: "test-tool", A2AConfig: &tools.A2AConfig{AgentURL: srv.URL}}

	_, err := e.Execute(context.Background(), desc, json.RawMessage(`{"query":"hello"}`))
	var httpErr *HTTPStatusError
	if !errors.As(err, &httpErr) {
		t.Fatalf("err = %v, want an *HTTPStatusError", err)
	}
	if httpErr.RetryAfter != 7*time.Second {
		t.Errorf("RetryAfter = %v, want 7s", httpErr.RetryAfter)
	}
}

// The executor waits what a 429's Retry-After asks for instead of its own
// backoff, while that is within MaxDelay.
func TestA2ARetryDelay_HonorsRetryAfterUpToMaxDelay(t *testing.T) {
	policy := RetryPolicy{InitialDelay: time.Hour, MaxDelay: 10 * time.Second}
	cases := []struct {
		name string
		err  error
		want time.Duration
	}{
		{"asks for 3s", &HTTPStatusError{StatusCode: 429, RetryAfter: 3 * time.Second}, 3 * time.Second},
		{"asks for MaxDelay", &HTTPStatusError{StatusCode: 503, RetryAfter: 10 * time.Second}, 10 * time.Second},
		{"does not ask", &HTTPStatusError{StatusCode: 503}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := a2aRetryPolicy(policy, "")
			retry, after := p.Classify(tc.err)
			if !retry {
				t.Fatalf("a retryable status was not retried")
			}
			wait, ok := p.Wait(0, after)
			if !ok {
				t.Fatalf("Retry-After %v within MaxDelay was not retried", after)
			}
			if tc.want == 0 {
				// No Retry-After: the backoff, an hour capped at MaxDelay and jittered.
				if wait < policy.MaxDelay/2 || wait > policy.MaxDelay {
					t.Errorf("wait = %v, want the backoff within [%v, %v]", wait, policy.MaxDelay/2, policy.MaxDelay)
				}
				return
			}
			if wait != tc.want {
				t.Errorf("wait = %v, want %v", wait, tc.want)
			}
		})
	}
}

// A 429 whose Retry-After is past MaxDelay is not waited out: the call fails
// after that one attempt, at once, with the agent's status.
func TestExecutor_RetryAfterPastMaxDelayStopsRetrying(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(noCard(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	e := NewExecutor(WithRetryPolicy(RetryPolicy{MaxRetries: 3, InitialDelay: time.Millisecond, MaxDelay: 10 * time.Second}))
	defer e.Close()
	desc := &tools.ToolDescriptor{Name: "test-tool", A2AConfig: &tools.A2AConfig{AgentURL: srv.URL}}

	start := time.Now()
	_, err := e.Execute(context.Background(), desc, json.RawMessage(`{"query":"hello"}`))
	var httpErr *HTTPStatusError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("err = %v, want the agent's 429", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1", got)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("took %v: the Retry-After was waited", elapsed)
	}
}

// alwaysFailing serves an agent that answers every call 502, counting them.
func alwaysFailing(t *testing.T) (string, *int32) {
	t.Helper()
	var attempts int32
	srv := httptest.NewServer(noCard(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &attempts
}

// attemptsWith runs one call to url with the tool's retry override and returns
// how many attempts reached the agent.
func attemptsWith(t *testing.T, e *Executor, url string, attempts *int32, override *tools.A2ARetryConfig) int32 {
	t.Helper()
	atomic.StoreInt32(attempts, 0)
	desc := &tools.ToolDescriptor{Name: "test-tool", A2AConfig: &tools.A2AConfig{AgentURL: url, RetryPolicy: override}}
	if _, err := e.Execute(context.Background(), desc, json.RawMessage(`{"query":"hello"}`)); err == nil {
		t.Fatal("expected an error from an agent that always fails")
	}
	return atomic.LoadInt32(attempts)
}

// A tool's A2AConfig.RetryPolicy is merged onto the executor's field by
// field: what it sets wins, what it leaves zero is the executor's.
func TestExecutor_PerAgentRetryPolicyMergesOntoTheExecutors(t *testing.T) {
	url, attempts := alwaysFailing(t)
	e := NewExecutor(WithRetryPolicy(RetryPolicy{MaxRetries: 4, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond}))
	defer e.Close()

	cases := []struct {
		name     string
		override *tools.A2ARetryConfig
		want     int32
	}{
		{"no override: the executor's 4 retries", nil, 5},
		{"a block setting only a delay keeps the executor's retries", &tools.A2ARetryConfig{InitialDelayMs: 1}, 5},
		{"a full block", &tools.A2ARetryConfig{MaxRetries: 1, InitialDelayMs: 1, MaxDelayMs: 1}, 2},
		{"negative MaxRetries turns retries off", &tools.A2ARetryConfig{MaxRetries: -1}, 1},
	}
	for _, tc := range cases {
		if got := attemptsWith(t, e, url, attempts, tc.override); got != tc.want {
			t.Errorf("%s: attempts = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// An executor built WithNoRetry stays single-attempt whatever an agent's block
// asks for: its host retries for itself.
func TestExecutor_WithNoRetryIgnoresAPerAgentRetryPolicy(t *testing.T) {
	url, attempts := alwaysFailing(t)
	e := NewExecutor(WithNoRetry())
	defer e.Close()
	block := &tools.A2ARetryConfig{MaxRetries: 3, InitialDelayMs: 1, MaxDelayMs: 1}
	if got := attemptsWith(t, e, url, attempts, block); got != 1 {
		t.Errorf("attempts = %d, want 1", got)
	}
}

// The merge, field by field: set fields in milliseconds, zero fields kept.
func TestMergeRetryPolicy(t *testing.T) {
	base := RetryPolicy{MaxRetries: 3, InitialDelay: 500 * time.Millisecond, MaxDelay: 30 * time.Second}
	cases := []struct {
		name string
		c    *tools.A2ARetryConfig
		want RetryPolicy
	}{
		{"nil", nil, base},
		{"full", &tools.A2ARetryConfig{MaxRetries: 2, InitialDelayMs: 100, MaxDelayMs: 5000},
			RetryPolicy{MaxRetries: 2, InitialDelay: 100 * time.Millisecond, MaxDelay: 5 * time.Second}},
		{"only initial", &tools.A2ARetryConfig{InitialDelayMs: 2000},
			RetryPolicy{MaxRetries: 3, InitialDelay: 2 * time.Second, MaxDelay: 30 * time.Second}},
		{"only max", &tools.A2ARetryConfig{MaxDelayMs: 1000},
			RetryPolicy{MaxRetries: 3, InitialDelay: 500 * time.Millisecond, MaxDelay: time.Second}},
		{"negative delays are kept from the executor", &tools.A2ARetryConfig{InitialDelayMs: -5, MaxDelayMs: -5}, base},
	}
	for _, tc := range cases {
		if got := mergeRetryPolicy(base, tc.c); got != tc.want {
			t.Errorf("%s: merge = %+v, want %+v", tc.name, got, tc.want)
		}
	}
	noRetry := RetryPolicy{}
	if got := mergeRetryPolicy(noRetry, &tools.A2ARetryConfig{MaxRetries: 3}); got != noRetry {
		t.Errorf("merge onto no-retry = %+v, want %+v", got, noRetry)
	}
}
