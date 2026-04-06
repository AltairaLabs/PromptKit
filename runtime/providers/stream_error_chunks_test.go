package providers

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// --- Nil-safety ---

func TestStreamMetrics_ObserveStreamErrorChunksForwarded_NilSafe(t *testing.T) {
	t.Parallel()
	var m *StreamMetrics
	// Must not panic on a nil receiver.
	m.ObserveStreamErrorChunksForwarded("openai", 5)
}

// --- chunkForwardedContent ---

func TestChunkForwardedContent_Classification(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		chunk   StreamChunk
		forward bool
	}{
		{
			name:    "text delta counts",
			chunk:   StreamChunk{Delta: "hello"},
			forward: true,
		},
		{
			name:    "tool call counts",
			chunk:   StreamChunk{ToolCalls: []types.MessageToolCall{{ID: "c1", Name: "search"}}},
			forward: true,
		},
		{
			name:    "media data counts",
			chunk:   StreamChunk{MediaData: &StreamMediaData{Data: []byte{1, 2, 3}}},
			forward: true,
		},
		{
			name:    "content-only without delta does not count",
			chunk:   StreamChunk{Content: "accumulated"},
			forward: false,
		},
		{
			name:    "finish reason alone does not count",
			chunk:   StreamChunk{FinishReason: stringPtr("stop")},
			forward: false,
		},
		{
			name:    "error chunk does not count",
			chunk:   StreamChunk{Error: errors.New("boom")},
			forward: false,
		},
		{
			name:    "empty chunk does not count",
			chunk:   StreamChunk{},
			forward: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chunk := tc.chunk
			if got := chunkForwardedContent(&chunk); got != tc.forward {
				t.Errorf("chunkForwardedContent() = %v, want %v", got, tc.forward)
			}
		})
	}
}

// stringPtr is a local helper so tests can build StreamChunk values
// with a finish reason without importing the providers.StringPtr
// helper from another file (which would create a naming collision in
// this test file).
func stringPtr(s string) *string {
	return &s
}

// --- End-to-end via RunStreamingRequest ---

// runStreamingTestHarness wires up a BaseProvider, a fake upstream, and
// a synthetic consumer that emits a caller-specified sequence of
// StreamChunks. Returns the drained chunks (in order) and the registered
// metrics registry for inspection.
//
// The fake upstream just serves a 200 OK with a body the consumer
// ignores — all real test logic lives in the consumer closure.
type runStreamingTestHarness struct {
	t        *testing.T
	chunks   []StreamChunk
	provider *BaseProvider
	metrics  *StreamMetrics
	reg      *prometheus.Registry
}

func newRunStreamingTestHarness(t *testing.T, chunks []StreamChunk) *runStreamingTestHarness {
	t.Helper()
	ResetDefaultStreamMetrics()
	t.Cleanup(ResetDefaultStreamMetrics)

	reg := prometheus.NewRegistry()
	metrics := RegisterDefaultStreamMetrics(reg, "test", nil)

	// Build a BaseProvider with a canned HTTP client whose RoundTripper
	// always returns 200 OK + an SSE-ish body. The consumer ignores the
	// body and emits the caller's pre-programmed chunk sequence so tests
	// can exercise the relay in isolation from real parser logic.
	b := NewBaseProvider("test", false, &http.Client{
		Transport: &fixedResponseRoundTripper{
			body: "data: {}\n\ndata: [DONE]\n\n",
		},
	})

	return &runStreamingTestHarness{
		t:        t,
		chunks:   chunks,
		provider: &b,
		metrics:  metrics,
		reg:      reg,
	}
}

func (h *runStreamingTestHarness) run() []StreamChunk {
	h.t.Helper()
	ctx := context.Background()
	req := &StreamRetryRequest{
		Policy:       StreamRetryPolicy{},
		ProviderName: "test",
		IdleTimeout:  5 * time.Second,
		RequestFn: func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, "POST", "http://test.invalid/x", http.NoBody)
		},
		Client: h.provider.GetStreamingHTTPClient(),
	}
	ch, err := h.provider.RunStreamingRequest(ctx, req, func(_ context.Context, body io.ReadCloser, out chan<- StreamChunk) {
		defer close(out)
		defer func() { _ = body.Close() }()
		for _, c := range h.chunks {
			out <- c
		}
	})
	if err != nil {
		h.t.Fatalf("RunStreamingRequest: %v", err)
	}
	var got []StreamChunk
	for c := range ch {
		got = append(got, c)
	}
	return got
}

// fixedResponseRoundTripper returns a 200 OK with a static SSE body
// regardless of the request. Used so the retry driver's peek succeeds
// and RunStreamingRequest enters the success path where the relay runs.
type fixedResponseRoundTripper struct {
	body string
}

func (f *fixedResponseRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(bytes.NewReader([]byte(f.body))),
		Request:    req,
	}, nil
}

func TestRunStreamingRequest_HealthyStreamObservesNothing(t *testing.T) {
	// Not t.Parallel: the harness mutates the package-level
	// DefaultStreamMetrics via ResetDefaultStreamMetrics +
	// RegisterDefaultStreamMetrics, and RunStreamingRequest reads
	// DefaultStreamMetrics() inside the relay goroutine. Running
	// these tests in parallel would interleave resets and cause one
	// test's observations to land in another test's registry.
	// A successful stream with content chunks and a clean finish must
	// NOT produce a sample in the error-chunks histogram — the metric
	// is strictly for errored streams.
	h := newRunStreamingTestHarness(t, []StreamChunk{
		{Delta: "hello "},
		{Delta: "world"},
		{FinishReason: stringPtr("stop")},
	})

	got := h.run()
	if len(got) != 3 {
		t.Errorf("chunks received = %d, want 3", len(got))
	}

	count := testutil.CollectAndCount(h.metrics.streamErrorChunksForwarded)
	if count != 0 {
		t.Errorf("error chunks histogram series count = %d, want 0 (no errors occurred)", count)
	}
}

func TestRunStreamingRequest_ErrorAtZeroContentChunks(t *testing.T) {
	// Not t.Parallel — see harness note on TestRunStreamingRequest_HealthyStreamObservesNothing.
	// An error terminal chunk with nothing forwarded first must be
	// observed as 0 — this is the bucket that represents errors Phase 1
	// could have caught if the classifier had recognised them.
	h := newRunStreamingTestHarness(t, []StreamChunk{
		{Error: errors.New("upstream exploded"), FinishReason: stringPtr("error")},
	})

	_ = h.run()

	if c, s := histogramSample(h.reg, "test_stream_error_chunks_forwarded", "test"); c != 1 || s != 0 {
		t.Errorf("histogram count=%d sum=%f, want count=1 sum=0 (zero content chunks before error)", c, s)
	}
}

func TestRunStreamingRequest_ErrorAfterContentChunks(t *testing.T) {
	// Not t.Parallel — see harness note on TestRunStreamingRequest_HealthyStreamObservesNothing.
	// The Phase 4 value-proposition case: an error after N content
	// chunks have been delivered downstream. The relay must count each
	// content-bearing chunk and observe N on the terminal error.
	h := newRunStreamingTestHarness(t, []StreamChunk{
		{Delta: "chunk-1"},
		{Delta: "chunk-2"},
		{Delta: "chunk-3"},
		{Error: errors.New("mid-stream kill"), FinishReason: stringPtr("error")},
	})

	got := h.run()
	// Caller must see all 4 chunks, including the terminal error — the
	// relay must not drop chunks on the way past the metric observation.
	if len(got) != 4 {
		t.Errorf("chunks received = %d, want 4 (relay must not drop chunks)", len(got))
	}
	if got[3].Error == nil {
		t.Error("final chunk must carry the error to the caller")
	}

	if c, s := histogramSample(h.reg, "test_stream_error_chunks_forwarded", "test"); c != 1 || s != 3 {
		t.Errorf("histogram count=%d sum=%f, want count=1 sum=3 (three content chunks before error)", c, s)
	}
}

func TestRunStreamingRequest_FinishReasonChunkDoesNotCount(t *testing.T) {
	// Not t.Parallel — see harness note on TestRunStreamingRequest_HealthyStreamObservesNothing.
	// A chunk carrying only a finish reason (no delta, no tool calls,
	// no media) must NOT increment the content chunk count. Providers
	// emit this style of chunk as a clean-close marker, and it has no
	// payload the caller committed to.
	h := newRunStreamingTestHarness(t, []StreamChunk{
		{Delta: "one"},
		{FinishReason: stringPtr("stop")}, // clean close marker, no content
		{Error: errors.New("fail after close")},
	})
	_ = h.run()

	// Expected: content count = 1 (only the "one" delta), not 2.
	if c, s := histogramSample(h.reg, "test_stream_error_chunks_forwarded", "test"); c != 1 || s != 1 {
		t.Errorf("histogram count=%d sum=%f, want count=1 sum=1", c, s)
	}
}

func TestRunStreamingRequest_InFlightGaugesReturnToZero(t *testing.T) {
	// Not t.Parallel — see harness note on TestRunStreamingRequest_HealthyStreamObservesNothing.
	// Regression guard for the relay refactor: splitting the consumer
	// goroutine from the relay goroutine must not leave any in-flight
	// gauge stuck above zero after the caller drains outChan. This is
	// the same invariant the loadtest suite's
	// assertInFlightGaugesZero helper enforces, but scoped to the
	// relay path specifically.
	h := newRunStreamingTestHarness(t, []StreamChunk{
		{Delta: "a"},
		{Delta: "b"},
		{FinishReason: stringPtr("stop")},
	})
	_ = h.run()

	// Allow the relay's deferred decrements to settle — they fire
	// after the caller reads the closed channel.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if testutil.ToFloat64(h.metrics.streamsInFlight.WithLabelValues("test")) == 0 &&
			testutil.ToFloat64(h.metrics.providerCallsInFlight.WithLabelValues("test")) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("in-flight gauges did not return to zero: streams=%v calls=%v",
		testutil.ToFloat64(h.metrics.streamsInFlight.WithLabelValues("test")),
		testutil.ToFloat64(h.metrics.providerCallsInFlight.WithLabelValues("test")),
	)
}

// histogramSample returns the total sample count and sum for a named
// histogram series labelled provider=<provider>, reading from a
// prometheus registry. Returns (0, 0) when the histogram has no
// samples matching the label. Using Gather() avoids pulling the
// client_model/go dto package into this file's imports.
func histogramSample(reg *prometheus.Registry, metricName, provider string) (count uint64, sum float64) {
	mfs, _ := reg.Gather()
	for _, mf := range mfs {
		if mf.GetName() != metricName {
			continue
		}
		for _, m := range mf.GetMetric() {
			var matches bool
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "provider" && lp.GetValue() == provider {
					matches = true
				}
			}
			if !matches {
				continue
			}
			hist := m.GetHistogram()
			if hist == nil {
				continue
			}
			count += hist.GetSampleCount()
			sum += hist.GetSampleSum()
		}
	}
	return count, sum
}

// --- Mid-stream reset retry (retry_window: always) ---

// TestRunStreamingRequest_MidStreamResetRetry exercises the full reset
// path: consumer emits content, then errors → relay emits Reset →
// retry opens a new stream → caller sees Reset + fresh content.
func TestRunStreamingRequest_MidStreamResetRetry(t *testing.T) {
	ResetDefaultStreamMetrics()
	t.Cleanup(ResetDefaultStreamMetrics)
	reg := prometheus.NewRegistry()
	_ = RegisterDefaultStreamMetrics(reg, "test", nil)

	var attempt atomic.Int32
	b := NewBaseProvider("test", false, &http.Client{
		Transport: &fixedResponseRoundTripper{
			body: "data: {}\n\ndata: [DONE]\n\n",
		},
	})

	budget := NewRetryBudget(10, 5)
	b.SetStreamRetryPolicy(StreamRetryPolicy{
		Enabled:      true,
		MaxAttempts:  2,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     50 * time.Millisecond,
		Window:       StreamRetryWindowAlways,
	})
	b.SetStreamRetryBudget(budget)

	ctx := context.Background()
	req := &StreamRetryRequest{
		Policy: StreamRetryPolicy{
			Enabled:      true,
			MaxAttempts:  2,
			InitialDelay: 10 * time.Millisecond,
			MaxDelay:     50 * time.Millisecond,
			Window:       StreamRetryWindowAlways,
		},
		Budget:       budget,
		ProviderName: "test",
		IdleTimeout:  5 * time.Second,
		RequestFn: func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, "POST", "http://test.invalid/x", http.NoBody)
		},
		Client: b.GetStreamingHTTPClient(),
	}

	consumer := func(_ context.Context, body io.ReadCloser, out chan<- StreamChunk) {
		defer close(out)
		defer func() { _ = body.Close() }()
		n := attempt.Add(1)
		if n == 1 {
			// First attempt: emit content then error.
			out <- StreamChunk{Delta: "hello ", Content: "hello "}
			out <- StreamChunk{Delta: "world", Content: "hello world"}
			out <- StreamChunk{Error: errors.New("mid-stream kill"), FinishReason: stringPtr("error")}
		} else {
			// Retry: clean response.
			out <- StreamChunk{Delta: "fresh ", Content: "fresh "}
			out <- StreamChunk{Delta: "response", Content: "fresh response"}
			out <- StreamChunk{FinishReason: stringPtr("stop"), Content: "fresh response"}
		}
	}

	ch, err := b.RunStreamingRequest(ctx, req, consumer)
	if err != nil {
		t.Fatalf("RunStreamingRequest: %v", err)
	}

	var got []StreamChunk
	for c := range ch {
		got = append(got, c)
	}

	// Expect: [hello delta, world delta, Reset, fresh delta, response delta, finish]
	if len(got) < 4 {
		t.Fatalf("expected at least 4 chunks (content + reset + retry content), got %d", len(got))
	}

	// Find the Reset chunk.
	resetIdx := -1
	for i, c := range got {
		if c.Reset {
			resetIdx = i
			break
		}
	}
	if resetIdx == -1 {
		t.Fatal("expected a Reset chunk in the stream")
	}

	// Content before Reset should be from the failed attempt.
	if got[0].Delta != "hello " {
		t.Errorf("first chunk delta = %q, want %q", got[0].Delta, "hello ")
	}

	// Content after Reset should be from the retry.
	afterReset := got[resetIdx+1:]
	if len(afterReset) == 0 {
		t.Fatal("no chunks after Reset")
	}
	if afterReset[0].Delta != "fresh " {
		t.Errorf("first retry chunk delta = %q, want %q", afterReset[0].Delta, "fresh ")
	}

	// The last chunk should be a clean finish, not an error.
	last := got[len(got)-1]
	if last.Error != nil {
		t.Errorf("final chunk has error: %v (retry should have succeeded)", last.Error)
	}
	if last.FinishReason == nil || *last.FinishReason != "stop" {
		t.Errorf("final chunk finish_reason = %v, want 'stop'", last.FinishReason)
	}

	// Verify the mid_stream_reset metric was recorded.
	resets := labeledCounterFromReg(reg, "test_stream_retries_total", "test", "outcome", "mid_stream_reset")
	if resets != 1 {
		t.Errorf("mid_stream_reset retries = %v, want 1", resets)
	}
}

// TestRunStreamingRequest_MidStreamRetryDisabledByDefault confirms that
// with default config (retry_window: pre_first_chunk), a mid-stream
// error does NOT trigger reset+retry — the error propagates as-is.
func TestRunStreamingRequest_MidStreamRetryDisabledByDefault(t *testing.T) {
	ResetDefaultStreamMetrics()
	t.Cleanup(ResetDefaultStreamMetrics)
	reg := prometheus.NewRegistry()
	_ = RegisterDefaultStreamMetrics(reg, "test", nil)

	b := NewBaseProvider("test", false, &http.Client{
		Transport: &fixedResponseRoundTripper{
			body: "data: {}\n\ndata: [DONE]\n\n",
		},
	})

	ctx := context.Background()
	req := &StreamRetryRequest{
		Policy: StreamRetryPolicy{
			Enabled:      true,
			MaxAttempts:  2,
			InitialDelay: 10 * time.Millisecond,
			MaxDelay:     50 * time.Millisecond,
			Window:       StreamRetryWindowPreFirstChunk, // default
		},
		ProviderName: "test",
		IdleTimeout:  5 * time.Second,
		RequestFn: func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, "POST", "http://test.invalid/x", http.NoBody)
		},
		Client: b.GetStreamingHTTPClient(),
	}

	consumer := func(_ context.Context, body io.ReadCloser, out chan<- StreamChunk) {
		defer close(out)
		defer func() { _ = body.Close() }()
		out <- StreamChunk{Delta: "partial"}
		out <- StreamChunk{Error: errors.New("mid-stream fail")}
	}

	ch, err := b.RunStreamingRequest(ctx, req, consumer)
	if err != nil {
		t.Fatalf("RunStreamingRequest: %v", err)
	}

	var sawReset, sawError bool
	for c := range ch {
		if c.Reset {
			sawReset = true
		}
		if c.Error != nil {
			sawError = true
		}
	}
	if sawReset {
		t.Error("Reset chunk emitted with pre_first_chunk window — mid-stream retry should be disabled")
	}
	if !sawError {
		t.Error("expected error chunk to propagate with pre_first_chunk window")
	}
}

// TestRunStreamingRequest_MidStreamRetryBudgetExhausted confirms that
// even with retry_window: always, an empty budget prevents the reset
// retry and the error propagates.
func TestRunStreamingRequest_MidStreamRetryBudgetExhausted(t *testing.T) {
	ResetDefaultStreamMetrics()
	t.Cleanup(ResetDefaultStreamMetrics)
	_ = RegisterDefaultStreamMetrics(prometheus.NewRegistry(), "test", nil)

	b := NewBaseProvider("test", false, &http.Client{
		Transport: &fixedResponseRoundTripper{
			body: "data: {}\n\ndata: [DONE]\n\n",
		},
	})

	// Budget with burst=1: one token available. Drain it before the test
	// so the mid-stream retry path finds the budget empty.
	budget := NewRetryBudget(0.001, 1) // near-zero refill rate
	budget.TryAcquire()                // drain the single token

	ctx := context.Background()
	req := &StreamRetryRequest{
		Policy: StreamRetryPolicy{
			Enabled:      true,
			MaxAttempts:  2,
			InitialDelay: 10 * time.Millisecond,
			MaxDelay:     50 * time.Millisecond,
			Window:       StreamRetryWindowAlways,
		},
		Budget:       budget,
		ProviderName: "test",
		IdleTimeout:  5 * time.Second,
		RequestFn: func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, "POST", "http://test.invalid/x", http.NoBody)
		},
		Client: b.GetStreamingHTTPClient(),
	}

	consumer := func(_ context.Context, body io.ReadCloser, out chan<- StreamChunk) {
		defer close(out)
		defer func() { _ = body.Close() }()
		out <- StreamChunk{Delta: "partial"}
		out <- StreamChunk{Error: errors.New("mid-stream fail")}
	}

	ch, err := b.RunStreamingRequest(ctx, req, consumer)
	if err != nil {
		t.Fatalf("RunStreamingRequest: %v", err)
	}

	var sawReset bool
	for c := range ch {
		if c.Reset {
			sawReset = true
		}
	}
	if sawReset {
		t.Error("Reset chunk emitted with exhausted budget — should have failed fast")
	}
}

// TestRunStreamingRequest_MidStreamRetryFailsToReopen confirms that
// when the retry itself fails to open a new stream (e.g. upstream is
// down), the caller gets an error chunk after the Reset rather than
// hanging indefinitely.
func TestRunStreamingRequest_MidStreamRetryFailsToReopen(t *testing.T) {
	ResetDefaultStreamMetrics()
	t.Cleanup(ResetDefaultStreamMetrics)
	_ = RegisterDefaultStreamMetrics(prometheus.NewRegistry(), "test", nil)

	var attempt atomic.Int32
	b := NewBaseProvider("test", false, &http.Client{
		Transport: &fixedResponseRoundTripper{
			body: "data: {}\n\ndata: [DONE]\n\n",
		},
	})
	budget := NewRetryBudget(10, 5)

	ctx := context.Background()
	req := &StreamRetryRequest{
		Policy: StreamRetryPolicy{
			Enabled:      true,
			MaxAttempts:  1, // no pre-first-chunk retry on the re-open
			InitialDelay: 10 * time.Millisecond,
			MaxDelay:     50 * time.Millisecond,
			Window:       StreamRetryWindowAlways,
		},
		Budget:       budget,
		ProviderName: "test",
		IdleTimeout:  5 * time.Second,
		RequestFn: func(ctx context.Context) (*http.Request, error) {
			n := attempt.Add(1)
			if n >= 2 {
				// Second call (the retry's re-open) fails.
				return nil, errors.New("connection refused")
			}
			return http.NewRequestWithContext(ctx, "POST", "http://test.invalid/x", http.NoBody)
		},
		Client: b.GetStreamingHTTPClient(),
	}

	consumer := func(_ context.Context, body io.ReadCloser, out chan<- StreamChunk) {
		defer close(out)
		defer func() { _ = body.Close() }()
		out <- StreamChunk{Delta: "partial content"}
		out <- StreamChunk{Error: errors.New("stream died")}
	}

	ch, err := b.RunStreamingRequest(ctx, req, consumer)
	if err != nil {
		t.Fatalf("RunStreamingRequest: %v", err)
	}

	var sawReset, sawError bool
	for c := range ch {
		if c.Reset {
			sawReset = true
		}
		if c.Error != nil {
			sawError = true
		}
	}
	if !sawReset {
		t.Error("expected Reset before the retry error")
	}
	if !sawError {
		t.Error("expected error chunk when retry fails to re-open")
	}
}

// TestRunStreamingRequest_MidStreamRetryCleanClose confirms that a
// stream that closes cleanly (no error) does not trigger mid-stream
// retry — the relay just exits.
func TestRunStreamingRequest_MidStreamRetryCleanClose(t *testing.T) {
	ResetDefaultStreamMetrics()
	t.Cleanup(ResetDefaultStreamMetrics)
	_ = RegisterDefaultStreamMetrics(prometheus.NewRegistry(), "test", nil)

	b := NewBaseProvider("test", false, &http.Client{
		Transport: &fixedResponseRoundTripper{
			body: "data: {}\n\ndata: [DONE]\n\n",
		},
	})

	ctx := context.Background()
	req := &StreamRetryRequest{
		Policy: StreamRetryPolicy{
			Enabled:      true,
			MaxAttempts:  2,
			InitialDelay: 10 * time.Millisecond,
			MaxDelay:     50 * time.Millisecond,
			Window:       StreamRetryWindowAlways,
		},
		Budget:       NewRetryBudget(10, 5),
		ProviderName: "test",
		IdleTimeout:  5 * time.Second,
		RequestFn: func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, "POST", "http://test.invalid/x", http.NoBody)
		},
		Client: b.GetStreamingHTTPClient(),
	}

	consumer := func(_ context.Context, body io.ReadCloser, out chan<- StreamChunk) {
		defer close(out)
		defer func() { _ = body.Close() }()
		out <- StreamChunk{Delta: "complete "}
		out <- StreamChunk{Delta: "response"}
		out <- StreamChunk{FinishReason: stringPtr("stop")}
	}

	ch, err := b.RunStreamingRequest(ctx, req, consumer)
	if err != nil {
		t.Fatalf("RunStreamingRequest: %v", err)
	}

	var sawReset bool
	var chunks int
	for c := range ch {
		chunks++
		if c.Reset {
			sawReset = true
		}
	}
	if sawReset {
		t.Error("clean stream should not emit Reset")
	}
	if chunks != 3 {
		t.Errorf("expected 3 chunks, got %d", chunks)
	}
}

// labeledCounterFromReg reads a counter matching provider and outcome
// labels from a registry. Like labeledCounter in the loadtest file but
// accessible from this test file.
func labeledCounterFromReg(reg *prometheus.Registry, metricName, provider, labelKey, labelVal string) float64 {
	mfs, _ := reg.Gather()
	for _, mf := range mfs {
		if mf.GetName() != metricName {
			continue
		}
		for _, m := range mf.GetMetric() {
			var providerMatches, labelMatches bool
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "provider" && lp.GetValue() == provider {
					providerMatches = true
				}
				if lp.GetName() == labelKey && lp.GetValue() == labelVal {
					labelMatches = true
				}
			}
			if providerMatches && labelMatches {
				return m.GetCounter().GetValue()
			}
		}
	}
	return 0
}
