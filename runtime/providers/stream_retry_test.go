package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestStreamRetryPolicy_AttemptsDisabled(t *testing.T) {
	t.Parallel()
	p := StreamRetryPolicy{}
	if got := p.Attempts(); got != 1 {
		t.Fatalf("disabled policy should attempt exactly 1 time, got %d", got)
	}
}

func TestStreamRetryPolicy_AttemptsDefaults(t *testing.T) {
	t.Parallel()
	p := StreamRetryPolicy{Enabled: true}
	if got := p.Attempts(); got != DefaultStreamRetryMaxAttempts {
		t.Fatalf("enabled policy with zero MaxAttempts should use default %d, got %d",
			DefaultStreamRetryMaxAttempts, got)
	}
}

func TestStreamRetryPolicy_AttemptsExplicit(t *testing.T) {
	t.Parallel()
	p := StreamRetryPolicy{Enabled: true, MaxAttempts: 5}
	if got := p.Attempts(); got != 5 {
		t.Fatalf("explicit MaxAttempts=5 should yield 5, got %d", got)
	}
}

func TestStreamRetryPolicy_AttemptsClamped(t *testing.T) {
	t.Parallel()
	p := StreamRetryPolicy{Enabled: true, MaxAttempts: -3}
	if got := p.Attempts(); got != DefaultStreamRetryMaxAttempts {
		t.Fatalf("negative MaxAttempts should fall back to default, got %d", got)
	}
}

func TestStreamRetryPolicy_BackoffRespectsCeiling(t *testing.T) {
	t.Parallel()
	p := StreamRetryPolicy{
		Enabled:      true,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     500 * time.Millisecond,
	}
	// Run many samples; none should exceed the ceiling even at high attempt counts.
	for attempt := 0; attempt < 10; attempt++ {
		for i := 0; i < 100; i++ {
			d := p.BackoffFor(attempt)
			if d < 0 || d > 500*time.Millisecond {
				t.Fatalf("attempt=%d sample=%d: backoff %v out of [0, 500ms]", attempt, i, d)
			}
		}
	}
}

func TestStreamRetryPolicy_BackoffFullJitterDistribution(t *testing.T) {
	t.Parallel()
	// With full jitter, samples should span the full range [0, delay].
	// We sanity-check that we see at least one value below 25% and one
	// above 75% of the ceiling, which would be impossible with equal
	// jitter (which only covers the top half).
	p := StreamRetryPolicy{
		Enabled:      true,
		InitialDelay: 1 * time.Second,
		MaxDelay:     1 * time.Second,
	}
	var sawLow, sawHigh bool
	for i := 0; i < 200; i++ {
		d := p.BackoffFor(0)
		if d < 250*time.Millisecond {
			sawLow = true
		}
		if d > 750*time.Millisecond {
			sawHigh = true
		}
	}
	if !sawLow || !sawHigh {
		t.Fatalf("full jitter should span [0, delay], sawLow=%v sawHigh=%v", sawLow, sawHigh)
	}
}

func TestIsRetryableStreamError_Nil(t *testing.T) {
	t.Parallel()
	if IsRetryableStreamError(nil) {
		t.Fatal("nil error should not be retryable")
	}
}

func TestIsRetryableStreamError_ContextCancel(t *testing.T) {
	t.Parallel()
	if IsRetryableStreamError(context.Canceled) {
		t.Fatal("context.Canceled must never be retried")
	}
	if IsRetryableStreamError(context.DeadlineExceeded) {
		t.Fatal("context.DeadlineExceeded must never be retried")
	}
}

func TestIsRetryableStreamError_HTTP2BodyClosed(t *testing.T) {
	t.Parallel()
	// This is the exact error string we saw in the gpt5-pro capability
	// matrix failure — it's the whole point of Phase 1.
	err := errors.New("http2: response body closed")
	if !IsRetryableStreamError(err) {
		t.Fatal("http2: response body closed must be classified as retryable")
	}
}

func TestIsRetryableStreamError_GOAWAY(t *testing.T) {
	t.Parallel()
	err := errors.New("http2: server sent GOAWAY and closed the connection")
	if !IsRetryableStreamError(err) {
		t.Fatal("h2 GOAWAY should be classified as retryable")
	}
}

func TestIsRetryableStreamError_UnknownError(t *testing.T) {
	t.Parallel()
	err := errors.New("some application-layer parse failure")
	if IsRetryableStreamError(err) {
		t.Fatal("application-layer errors should not be classified as retryable")
	}
}

func TestIsRetryableStreamStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		code    int
		want    bool
		purpose string
	}{
		{429, true, "rate limit"},
		{502, true, "bad gateway"},
		{503, true, "service unavailable"},
		{504, true, "gateway timeout"},
		{500, false, "internal server error (not retryable — indicates app bug)"},
		{400, false, "bad request"},
		{401, false, "unauthorized"},
		{200, false, "ok"},
	}
	for _, tc := range cases {
		if got := IsRetryableStreamStatus(tc.code); got != tc.want {
			t.Errorf("IsRetryableStreamStatus(%d) [%s] = %v, want %v",
				tc.code, tc.purpose, got, tc.want)
		}
	}
}

// --- peekFirstSSEEvent ---

// Contract: peekFirstSSEEvent reads until it sees at least one complete SSE
// event, then returns *enough bytes* to replay the stream contiguously from
// the start. In practice the returned slice includes the first event plus
// any bytes the internal bufio.Reader pre-buffered past the event boundary.
// The caller guarantees the underlying reader is positioned exactly after
// the last byte of the returned slice, so (returned || underlying) is the
// original stream. We test that invariant rather than exact byte counts.

func TestPeekFirstSSEEvent_ContainsFirstEvent(t *testing.T) {
	t.Parallel()
	input := "data: hello\n\ndata: world\n\n"
	got, err := (SSEFrameDetector{}).PeekFirstFrame(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(string(got), "data: hello\n\n") {
		t.Fatalf("peeked %q must begin with the first event", string(got))
	}
	if !strings.Contains(input, string(got)) {
		t.Fatalf("peeked bytes must be a prefix of the input stream")
	}
}

func TestPeekFirstSSEEvent_WithComments(t *testing.T) {
	t.Parallel()
	// SSE comments (: lines) and keepalives must pass through to the peek
	// buffer so replay produces byte-identical output for downstream parsers.
	input := ": keepalive\ndata: first\n\ndata: second\n\n"
	got, err := (SSEFrameDetector{}).PeekFirstFrame(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(string(got), ": keepalive\ndata: first\n\n") {
		t.Fatalf("peeked %q must include the keepalive and first event", string(got))
	}
}

// Verify that when the underlying source emits data in small chunks, the
// peek stops reading new bytes as soon as it finds the first event
// boundary (i.e., it does NOT drain the entire stream unnecessarily).
func TestPeekFirstSSEEvent_StopsAtBoundary(t *testing.T) {
	t.Parallel()
	// A source that yields one byte per Read. bufio cannot pre-buffer
	// ahead of our manual reads beyond the requested size.
	slow := &byteReader{data: []byte("data: hello\n\ndata: world\n\n")}
	got, err := (SSEFrameDetector{}).PeekFirstFrame(slow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With a byte-at-a-time source, the peek should have consumed exactly
	// "data: hello\n\n" — anything more would indicate we over-read.
	if string(got) != "data: hello\n\n" {
		t.Fatalf("peeked %q, want exact first-event bytes", string(got))
	}
}

// byteReader is an io.Reader that returns one byte per Read call, used to
// exercise the peek's boundary detection under slow/chunked sources.
type byteReader struct {
	data []byte
	pos  int
}

func (r *byteReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}
	p[0] = r.data[r.pos]
	r.pos++
	return 1, nil
}

func TestPeekFirstSSEEvent_EOFAfterFirstEvent(t *testing.T) {
	t.Parallel()
	// Stream closes right after the first event with no trailing blank
	// line — should still be treated as a successful peek.
	input := "data: only\n"
	got, err := (SSEFrameDetector{}).PeekFirstFrame(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(got), "data: only") {
		t.Fatalf("peeked %q, expected it to contain 'data: only'", string(got))
	}
}

func TestPeekFirstSSEEvent_EmptyStream(t *testing.T) {
	t.Parallel()
	_, err := (SSEFrameDetector{}).PeekFirstFrame(strings.NewReader(""))
	if err == nil {
		t.Fatal("empty stream should return an error")
	}
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

// --- OpenStreamWithRetry integration ---

// streamTestServer returns an httptest.Server whose handler runs fn for each
// request. fn is expected to write to w and return. Use atomic counters in
// fn to observe attempt count across requests.
func streamTestServer(t *testing.T, fn http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(fn)
}

func TestOpenStreamWithRetry_SuccessFirstAttempt(t *testing.T) {
	t.Parallel()
	var attempts int32
	srv := streamTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: hello\n\ndata: world\n\n")
	})
	defer srv.Close()

	result, err := OpenStreamWithRetry(
		context.Background(),
		StreamRetryPolicy{Enabled: true, MaxAttempts: 3},
		"test",
		time.Second,
		func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, "GET", srv.URL, http.NoBody)
		},
		srv.Client(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer result.Body.Close()

	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("expected 1 server hit, got %d", got)
	}
	if result.Attempts != 1 {
		t.Errorf("result.Attempts = %d, want 1", result.Attempts)
	}

	// Read the full body and assert the peeked first event was replayed.
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	want := "data: hello\n\ndata: world\n\n"
	if string(body) != want {
		t.Errorf("replayed body = %q, want %q", string(body), want)
	}
}

func TestOpenStreamWithRetry_RetriesOnRetryableStatus(t *testing.T) {
	t.Parallel()
	var attempts int32
	srv := streamTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, "try later")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: recovered\n\n")
	})
	defer srv.Close()

	result, err := OpenStreamWithRetry(
		context.Background(),
		StreamRetryPolicy{
			Enabled:      true,
			MaxAttempts:  3,
			InitialDelay: 1 * time.Millisecond,
			MaxDelay:     5 * time.Millisecond,
		},
		"test",
		time.Second,
		func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, "GET", srv.URL, http.NoBody)
		},
		srv.Client(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer result.Body.Close()

	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Errorf("expected 2 server hits, got %d", got)
	}
	if result.Attempts != 2 {
		t.Errorf("result.Attempts = %d, want 2", result.Attempts)
	}
	body, _ := io.ReadAll(result.Body)
	if !strings.Contains(string(body), "data: recovered") {
		t.Errorf("expected recovered body, got %q", string(body))
	}
}

func TestOpenStreamWithRetry_NonRetryableStatusFails(t *testing.T) {
	t.Parallel()
	var attempts int32
	srv := streamTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "nope")
	})
	defer srv.Close()

	_, err := OpenStreamWithRetry(
		context.Background(),
		StreamRetryPolicy{Enabled: true, MaxAttempts: 3, InitialDelay: time.Millisecond},
		"test",
		time.Second,
		func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, "GET", srv.URL, http.NoBody)
		},
		srv.Client(),
	)
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("400 should not retry, got %d attempts", got)
	}
}

func TestOpenStreamWithRetry_RetriesOnMidBodyClose(t *testing.T) {
	t.Parallel()
	// This is the gpt-5-pro failure simulation: first response sends
	// headers, then closes the body before any SSE event is produced.
	// Second response succeeds.
	var attempts int32
	srv := streamTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("ResponseWriter does not support flushing")
			return
		}
		flusher.Flush()
		if n == 1 {
			// Abruptly close without sending any event.
			// Using hijack to simulate a connection reset.
			hijacker, okH := w.(http.Hijacker)
			if okH {
				conn, _, errH := hijacker.Hijack()
				if errH == nil {
					_ = conn.Close()
				}
			}
			return
		}
		_, _ = io.WriteString(w, "data: recovered\n\n")
		flusher.Flush()
	})
	defer srv.Close()

	result, err := OpenStreamWithRetry(
		context.Background(),
		StreamRetryPolicy{
			Enabled:      true,
			MaxAttempts:  3,
			InitialDelay: 1 * time.Millisecond,
			MaxDelay:     5 * time.Millisecond,
		},
		"test",
		2*time.Second,
		func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, "GET", srv.URL, http.NoBody)
		},
		srv.Client(),
	)
	if err != nil {
		t.Fatalf("expected retry success, got error: %v", err)
	}
	defer result.Body.Close()

	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Errorf("expected 2 server hits, got %d", got)
	}
	body, _ := io.ReadAll(result.Body)
	if !strings.Contains(string(body), "data: recovered") {
		t.Errorf("expected recovered body, got %q", string(body))
	}
}

func TestOpenStreamWithRetry_ExhaustsAttempts(t *testing.T) {
	t.Parallel()
	var attempts int32
	srv := streamTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	defer srv.Close()

	_, err := OpenStreamWithRetry(
		context.Background(),
		StreamRetryPolicy{
			Enabled:      true,
			MaxAttempts:  3,
			InitialDelay: 1 * time.Millisecond,
			MaxDelay:     5 * time.Millisecond,
		},
		"test",
		time.Second,
		func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, "GET", srv.URL, http.NoBody)
		},
		srv.Client(),
	)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("expected 3 server hits (max_attempts), got %d", got)
	}
}

func TestOpenStreamWithRetry_DisabledDoesNotRetry(t *testing.T) {
	t.Parallel()
	var attempts int32
	srv := streamTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	defer srv.Close()

	_, err := OpenStreamWithRetry(
		context.Background(),
		StreamRetryPolicy{}, // disabled
		"test",
		time.Second,
		func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, "GET", srv.URL, http.NoBody)
		},
		srv.Client(),
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("disabled policy should not retry, got %d attempts", got)
	}
}

func TestOpenStreamWithRetry_ContextCancelStopsRetry(t *testing.T) {
	t.Parallel()
	srv := streamTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the call

	_, err := OpenStreamWithRetry(
		ctx,
		StreamRetryPolicy{Enabled: true, MaxAttempts: 5, InitialDelay: 10 * time.Millisecond},
		"test",
		time.Second,
		func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, "GET", srv.URL, http.NoBody)
		},
		srv.Client(),
	)
	if err == nil {
		t.Fatal("expected context error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// --- Budget integration (Phase 2) ---

// When the budget is empty, a retryable failure must not trigger a retry —
// the call should fail fast with the original error. This is the
// load-bearing guarantee of the budget: during an upstream storm we burn
// attempts on the healthy traffic, not on re-dial amplification.
func TestOpenStreamWithRetryRequest_BudgetExhaustedFailsFast(t *testing.T) {
	t.Parallel()
	var attempts int32
	srv := streamTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	defer srv.Close()

	// Pre-drained budget: burst 1, immediately consume it.
	budget := NewRetryBudget(0.001, 1)
	if !budget.TryAcquire() {
		t.Fatal("pre-drain failed to acquire the single token")
	}

	_, err := OpenStreamWithRetryRequest(context.Background(), &StreamRetryRequest{
		Policy: StreamRetryPolicy{
			Enabled:      true,
			MaxAttempts:  5,
			InitialDelay: 1 * time.Millisecond,
			MaxDelay:     5 * time.Millisecond,
		},
		Budget:       budget,
		ProviderName: "test",
		IdleTimeout:  time.Second,
		RequestFn: func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, "GET", srv.URL, http.NoBody)
		},
		Client: srv.Client(),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	// Only 1 server hit expected: the initial attempt, then budget
	// blocks retry and we return immediately.
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("expected 1 hit (budget should block retry), got %d", got)
	}
}

// With a healthy budget, retries proceed normally — same behavior as
// Phase 1 (nil budget) but routing through the Phase 2 TryAcquire path.
func TestOpenStreamWithRetryRequest_BudgetAllowsRetry(t *testing.T) {
	t.Parallel()
	var attempts int32
	srv := streamTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: ok\n\n")
	})
	defer srv.Close()

	budget := NewRetryBudget(100, 10)

	result, err := OpenStreamWithRetryRequest(context.Background(), &StreamRetryRequest{
		Policy: StreamRetryPolicy{
			Enabled:      true,
			MaxAttempts:  3,
			InitialDelay: 1 * time.Millisecond,
			MaxDelay:     5 * time.Millisecond,
		},
		Budget:       budget,
		ProviderName: "test",
		Host:         "localhost",
		IdleTimeout:  time.Second,
		RequestFn: func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, "GET", srv.URL, http.NoBody)
		},
		Client: srv.Client(),
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	defer result.Body.Close()

	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Errorf("expected 2 hits, got %d", got)
	}
	// Budget should have been decremented by 1 (the retry).
	if avail := budget.Available(); avail > 9.5 { // allow minor refill
		t.Errorf("budget should be decremented, got Available() = %v", avail)
	}
}

// A nil budget must preserve Phase 1 semantics exactly — unbounded
// retries up to MaxAttempts. This is the backwards-compat guarantee.
func TestOpenStreamWithRetryRequest_NilBudgetIsUnbounded(t *testing.T) {
	t.Parallel()
	var attempts int32
	srv := streamTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	defer srv.Close()

	_, err := OpenStreamWithRetryRequest(context.Background(), &StreamRetryRequest{
		Policy: StreamRetryPolicy{
			Enabled:      true,
			MaxAttempts:  3,
			InitialDelay: 1 * time.Millisecond,
			MaxDelay:     5 * time.Millisecond,
		},
		Budget:       nil, // explicit
		ProviderName: "test",
		IdleTimeout:  time.Second,
		RequestFn: func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, "GET", srv.URL, http.NoBody)
		},
		Client: srv.Client(),
	})
	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}
	// All 3 attempts should land — nil budget = unbounded.
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("expected 3 hits with nil budget, got %d", got)
	}
}

// --- Host label ---

func TestHostFromURL(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"https://api.openai.com/v1/chat/completions":           "api.openai.com",
		"http://localhost:8080/responses":                      "localhost:8080",
		"https://generativelanguage.googleapis.com/v1beta/...": "generativelanguage.googleapis.com",
		"":                 "",
		"not a url at all": "",
		"://broken":        "",
	}
	for input, want := range cases {
		if got := HostFromURL(input); got != want {
			t.Errorf("HostFromURL(%q) = %q, want %q", input, got, want)
		}
	}
}

// Verify that the error message from a non-retryable HTTP status bubbles
// up with a helpful snippet of the body so operators can debug.
func TestOpenStreamWithRetry_ErrorIncludesStatusAndBody(t *testing.T) {
	t.Parallel()
	srv := streamTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "bad thing happened")
	})
	defer srv.Close()

	_, err := OpenStreamWithRetry(
		context.Background(),
		StreamRetryPolicy{},
		"test",
		time.Second,
		func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, "GET", srv.URL, http.NoBody)
		},
		srv.Client(),
	)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := fmt.Sprint(err)
	if !strings.Contains(msg, "400") {
		t.Errorf("error should mention status 400: %q", msg)
	}
	if !strings.Contains(msg, "bad thing happened") {
		t.Errorf("error should include response body: %q", msg)
	}
}
