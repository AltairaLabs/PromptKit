//go:build loadtest

// Package openai — streaming retry / budget / semaphore load test.
//
// Run with:
//
//	go test -tags=loadtest -race -timeout=5m ./runtime/providers/openai/ \
//	  -run TestStreamRetryLoad -v
//
// Zero API tokens consumed. The test uses httptest.Server as a fake
// OpenAI endpoint with configurable failure injection, drives real
// openai.Provider code through it at scale, and asserts invariants on
// the Prometheus metrics exposed by the streaming retry infrastructure.
//
// What this validates (see issue #859 for the full list):
//  1. Budget defaults hold under herd-kill scenarios
//  2. Semaphore never exceeds its configured limit
//  3. In-flight gauges return to zero after all streams complete
//  4. Goroutine count does not grow unboundedly
//  5. Direct-update metrics stay accurate under write contention
package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// --- Failure injection ---

// failureMode configures how fakeOpenAI responds to requests. Each field
// is an independent knob; a request may be subject to any combination.
type failureMode struct {
	// preFirstChunkRate is the probability (0.0-1.0) that a request
	// will have its connection hijacked and closed immediately after
	// response headers, before any SSE data is written. This simulates
	// the h2 stream reset we saw on gpt-5-pro.
	preFirstChunkRate float64
	// midStreamRate is the probability that a request will have the
	// connection hijacked mid-SSE-frame — one complete delta chunk is
	// delivered (flushed), then a partial second chunk without the
	// trailing blank line, then abrupt close. This produces a scanner
	// error on the client side AFTER the first content chunk has been
	// forwarded downstream, testing whether retry correctly refuses
	// to fire (and thus cannot re-emit partial content).
	midStreamRate float64
	// firstChunkDelay is a fixed delay applied before the first SSE
	// data event is written. Simulates reasoning-model initial latency.
	firstChunkDelay time.Duration
	// status503Rate is the probability that a request will respond
	// with 503 Service Unavailable. Tests retryable status handling.
	status503Rate float64
}

// fakeOpenAI is a configurable SSE server that speaks the OpenAI Chat
// Completions streaming protocol. Call Close when done.
type fakeOpenAI struct {
	server *httptest.Server
	mu     sync.RWMutex
	mode   failureMode
	// counters of what the fake actually did, for diagnostics
	totalRequests  atomic.Int64
	preFirstKills  atomic.Int64
	midStreamKills atomic.Int64
	status503s     atomic.Int64
	successes      atomic.Int64
}

func newFakeOpenAI() *fakeOpenAI {
	f := &fakeOpenAI{}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	return f
}

func (f *fakeOpenAI) URL() string { return f.server.URL }
func (f *fakeOpenAI) Close()      { f.server.Close() }

func (f *fakeOpenAI) setMode(m failureMode) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mode = m
}

func (f *fakeOpenAI) getMode() failureMode {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.mode
}

// handle serves a single request. It uses a deterministic rotation
// keyed on the request count rather than true randomness so test runs
// are reproducible for the same mode distribution.
func (f *fakeOpenAI) handle(w http.ResponseWriter, _ *http.Request) {
	n := f.totalRequests.Add(1)
	mode := f.getMode()

	// Bucket the request into one of the failure modes by mapping its
	// sequence number to a cumulative probability threshold. This gives
	// us exact counts instead of stochastic noise at low request counts.
	cum := 0.0
	bucket := float64((n-1)%1000) / 1000.0 // 0.000..0.999
	cum += mode.status503Rate
	if bucket < cum {
		f.status503s.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":{"message":"service unavailable","type":"server_error"}}`)
		return
	}
	cum += mode.preFirstChunkRate
	if bucket < cum {
		f.preFirstKills.Add(1)
		// Write headers then hijack the connection and close it.
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, err := hj.Hijack()
			if err == nil {
				_ = conn.Close()
			}
		}
		return
	}
	cum += mode.midStreamRate
	if bucket < cum {
		f.midStreamKills.Add(1)
		writeMidStreamKill(w)
		return
	}

	// Success path.
	f.successes.Add(1)
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	if mode.firstChunkDelay > 0 {
		time.Sleep(mode.firstChunkDelay)
	}
	writeChunks(w, 3)
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}

// writeMidStreamKill simulates a real mid-stream failure: write one
// complete SSE event (including trailing blank line, so the retry
// driver's peekFirstSSEEvent is satisfied and returns its buffered
// bytes to the stream consumer), then write the prefix of a second
// event WITHOUT closing the frame, then hijack the connection.
//
// The downstream SSE scanner sees the first event cleanly, delivers
// its content to the caller via a StreamChunk, then blocks reading
// the second event, sees partial bytes, and hits an unexpected EOF
// when the connection closes. That error propagates as a terminal
// StreamChunk with Error set.
//
// Critically, this is the failure mode Phase 1 must NOT retry on —
// by the time the failure happens, content has been forwarded
// downstream and a retry would cause double-emission.
func writeMidStreamKill(w http.ResponseWriter) {
	flusher, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	if flusher != nil {
		flusher.Flush()
	}

	// One complete delta event — satisfies the retry driver's peek
	// and becomes the first content StreamChunk seen by the caller.
	first := map[string]any{
		"id":      "chatcmpl-loadtest",
		"object":  "chat.completion.chunk",
		"created": 1,
		"model":   "gpt-4o",
		"choices": []map[string]any{{
			"index":         0,
			"delta":         map[string]any{"content": "chunk-0 "},
			"finish_reason": nil,
		}},
	}
	b, _ := json.Marshal(first)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
	if flusher != nil {
		flusher.Flush()
	}

	// Partial second delta with NO trailing newline — the scanner will
	// hit EOF mid-frame when we hijack+close below.
	_, _ = io.WriteString(w, `data: {"id":"chatcmpl-loadtest","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"par`)
	if flusher != nil {
		flusher.Flush()
	}

	if hj, ok := w.(http.Hijacker); ok {
		if conn, _, hErr := hj.Hijack(); hErr == nil {
			_ = conn.Close()
		}
	}
}

// writeChunks emits n delta chunks plus a final stop-reason chunk.
// Each chunk is flushed immediately so the client sees them as real
// streaming deltas rather than one batched response.
func writeChunks(w http.ResponseWriter, n int) {
	flusher, _ := w.(http.Flusher)
	for i := 0; i < n; i++ {
		chunk := map[string]any{
			"id":      "chatcmpl-loadtest",
			"object":  "chat.completion.chunk",
			"created": 1,
			"model":   "gpt-4o",
			"choices": []map[string]any{{
				"index": 0,
				"delta": map[string]any{
					"content": fmt.Sprintf("chunk-%d ", i),
				},
				"finish_reason": nil,
			}},
		}
		b, _ := json.Marshal(chunk)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
		if flusher != nil {
			flusher.Flush()
		}
	}
	// Final chunk with finish_reason.
	final := map[string]any{
		"id":      "chatcmpl-loadtest",
		"object":  "chat.completion.chunk",
		"created": 1,
		"model":   "gpt-4o",
		"choices": []map[string]any{{
			"index":         0,
			"delta":         map[string]any{},
			"finish_reason": "stop",
		}},
	}
	b, _ := json.Marshal(final)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
	if flusher != nil {
		flusher.Flush()
	}
}

// --- Metrics isolation ---

// installTestMetrics replaces the process-wide DefaultStreamMetrics
// with a fresh instance registered into a dedicated test registry,
// and returns both the metrics handle and the registry. This lets
// each load scenario see a clean slate.
func installTestMetrics(t *testing.T) (*providers.StreamMetrics, *prometheus.Registry) {
	t.Helper()
	reg := prometheus.NewRegistry()
	providers.ResetDefaultStreamMetrics()
	m := providers.RegisterDefaultStreamMetrics(reg, "loadtest", nil)
	t.Cleanup(providers.ResetDefaultStreamMetrics)
	return m, reg
}

// --- Provider construction ---

// buildProvider constructs a real openai.Provider wired against the
// fake server, with the given retry/budget/semaphore policy.
func buildProvider(fakeURL string, opts providerOpts) *Provider {
	p := NewProviderWithConfig(
		"loadtest",
		"gpt-4o",
		fakeURL,
		providers.ProviderDefaults{},
		false,
		map[string]any{"api_mode": "completions"}, // force Chat Completions path
	)
	// Short idle timeout so stalled connections don't hold up the test.
	p.SetStreamIdleTimeout(5 * time.Second)
	if opts.policy.Enabled {
		p.SetStreamRetryPolicy(opts.policy)
	}
	if opts.budget != nil {
		p.SetStreamRetryBudget(opts.budget)
	}
	if opts.semaphore != nil {
		p.SetStreamSemaphore(opts.semaphore)
	}
	return p
}

type providerOpts struct {
	policy    providers.StreamRetryPolicy
	budget    *providers.RetryBudget
	semaphore *providers.StreamSemaphore
}

// --- Worker pool ---

// driveRequests runs n concurrent PredictStream calls through p with
// the given concurrency cap. Each request fully drains its channel
// so the goroutine bookkeeping is exercised end to end. Returns
// counts of successes and errors.
func driveRequests(t *testing.T, p *Provider, n, concurrency int, timeout time.Duration) (successes, failures int64) {
	t.Helper()
	var succ, fail atomic.Int64
	work := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		work <- struct{}{}
	}
	close(work)

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range work {
				ctx, cancel := context.WithTimeout(context.Background(), timeout)
				ch, err := p.PredictStream(ctx, providers.PredictionRequest{
					Messages: []types.Message{{Role: "user", Content: "go"}},
				})
				if err != nil {
					fail.Add(1)
					cancel()
					continue
				}
				// Drain the channel fully so the stream goroutine exits.
				streamErr := drainStream(ch)
				cancel()
				if streamErr != nil {
					fail.Add(1)
				} else {
					succ.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	return succ.Load(), fail.Load()
}

// drainStream reads every chunk from the channel until it is closed,
// returning any terminal error encountered. Acts as the downstream
// consumer of the retry driver's output.
func drainStream(ch <-chan providers.StreamChunk) error {
	for chunk := range ch {
		if chunk.Error != nil {
			// Drain the rest before returning so the producer goroutine
			// isn't left blocked on the channel.
			for range ch { //nolint:revive // intentional drain
			}
			return chunk.Error
		}
	}
	return nil
}

// --- Invariant assertions ---

// assertInFlightGaugesZero verifies that after all requests complete,
// both the streams_in_flight and provider_calls_in_flight gauges have
// returned to zero. Non-zero indicates a leaked stream goroutine.
func assertInFlightGaugesZero(t *testing.T, reg *prometheus.Registry, providerID string) {
	t.Helper()
	// Wait briefly — the stream goroutine's deferred release happens
	// asynchronously after the consumer's last channel read.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		streams := gaugeValue(reg, "loadtest_streams_in_flight", providerID)
		calls := gaugeValue(reg, "loadtest_provider_calls_in_flight", providerID)
		if streams == 0 && calls == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	streams := gaugeValue(reg, "loadtest_streams_in_flight", providerID)
	calls := gaugeValue(reg, "loadtest_provider_calls_in_flight", providerID)
	t.Errorf("in-flight gauges did not return to zero: streams=%v calls=%v", streams, calls)
}

// gaugeValue reads a gauge by metric name and provider label from the
// registry. Returns 0 if the metric is absent (which is also the
// expected resting state for these gauges).
func gaugeValue(reg *prometheus.Registry, metricName, providerID string) float64 {
	mfs, _ := reg.Gather()
	for _, mf := range mfs {
		if mf.GetName() != metricName {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "provider" && lp.GetValue() == providerID {
					return m.GetGauge().GetValue()
				}
			}
		}
	}
	return 0
}

// counterSum returns the total count of a counter across all label
// combinations matching provider=providerID.
func counterSum(reg *prometheus.Registry, metricName, providerID string) float64 {
	mfs, _ := reg.Gather()
	var total float64
	for _, mf := range mfs {
		if mf.GetName() != metricName {
			continue
		}
		for _, m := range mf.GetMetric() {
			var providerMatches bool
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "provider" && lp.GetValue() == providerID {
					providerMatches = true
				}
			}
			if providerMatches {
				total += m.GetCounter().GetValue()
			}
		}
	}
	return total
}

// logGoroutineDrift logs the delta between pre- and post-test goroutine
// counts. Informational only: the authoritative leak signal for
// streaming is the in-flight gauge (see assertInFlightGaugesZero), not
// raw goroutine count. HTTP keep-alive workers, httptest server
// workers, and GC-pending context cancellers all inflate the count
// without being "leaks" in any meaningful sense.
//
// To see the drift in log output even on success, run tests with -v.
func logGoroutineDrift(t *testing.T, before int) {
	t.Helper()
	// Force a GC cycle and give background goroutines a moment to
	// unwind. This reduces noise but does not eliminate it — idle
	// keep-alive workers hold on for up to IdleConnTimeout (90s).
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	after := runtime.NumGoroutine()
	t.Logf("goroutine drift: before=%d after=%d drift=%+d (informational; "+
		"in-flight gauges are the authoritative stream-leak signal)",
		before, after, after-before)
}

// --- Scenarios ---

// Scenario: healthy upstream, high concurrency, no failures. Verifies
// the happy path doesn't leak goroutines or over-count metrics.
func TestStreamRetryLoad_HealthyHighConcurrency(t *testing.T) {
	_, reg := installTestMetrics(t)
	fake := newFakeOpenAI()
	defer fake.Close()

	p := buildProvider(fake.URL(), providerOpts{
		policy:    providers.StreamRetryPolicy{Enabled: true, MaxAttempts: 2, InitialDelay: 50 * time.Millisecond, MaxDelay: 500 * time.Millisecond},
		budget:    providers.NewRetryBudget(10, 20),
		semaphore: providers.NewStreamSemaphore(50),
	})

	baseline := runtime.NumGoroutine()
	succ, fail := driveRequests(t, p, 500, 100, 10*time.Second)

	t.Logf("results: success=%d fail=%d | server: total=%d succ=%d",
		succ, fail, fake.totalRequests.Load(), fake.successes.Load())

	if fail > 0 {
		t.Errorf("healthy upstream should produce zero failures, got %d", fail)
	}
	if succ != 500 {
		t.Errorf("expected 500 successes, got %d", succ)
	}
	assertInFlightGaugesZero(t, reg, "loadtest")
	logGoroutineDrift(t, baseline)

	// Healthy path should emit no retry attempts.
	retries := counterSum(reg, "loadtest_stream_retries_total", "loadtest")
	if retries > 0 {
		t.Errorf("healthy path should not trigger retries, got %v", retries)
	}
}

// Scenario: 100% pre-first-chunk failures. Tests that every request
// retries up to MaxAttempts, exhausts the budget eventually, and the
// budget counter is incremented accurately.
func TestStreamRetryLoad_AllPreFirstChunkFail(t *testing.T) {
	_, reg := installTestMetrics(t)
	fake := newFakeOpenAI()
	defer fake.Close()
	fake.setMode(failureMode{preFirstChunkRate: 1.0})

	budget := providers.NewRetryBudget(1, 5) // tight: rate=1/s burst=5
	p := buildProvider(fake.URL(), providerOpts{
		policy:    providers.StreamRetryPolicy{Enabled: true, MaxAttempts: 3, InitialDelay: 10 * time.Millisecond, MaxDelay: 100 * time.Millisecond},
		budget:    budget,
		semaphore: providers.NewStreamSemaphore(100),
	})

	baseline := runtime.NumGoroutine()
	succ, fail := driveRequests(t, p, 100, 50, 10*time.Second)

	t.Logf("results: success=%d fail=%d | server: total=%d kills=%d",
		succ, fail, fake.totalRequests.Load(), fake.preFirstKills.Load())

	// Every request must fail (upstream is 100% broken).
	if succ != 0 {
		t.Errorf("expected zero successes against 100%% failure server, got %d", succ)
	}
	if fail != 100 {
		t.Errorf("expected 100 failures, got %d", fail)
	}

	// Budget should show exhaustion events.
	budgetExhausted := labeledCounter(reg, "loadtest_stream_retries_total", "loadtest", "outcome", "budget_exhausted")
	exhausted := labeledCounter(reg, "loadtest_stream_retries_total", "loadtest", "outcome", "exhausted")
	failed := labeledCounter(reg, "loadtest_stream_retries_total", "loadtest", "outcome", "failed")

	t.Logf("retry outcomes: failed=%v exhausted=%v budget_exhausted=%v",
		failed, exhausted, budgetExhausted)

	if failed+exhausted+budgetExhausted == 0 {
		t.Error("expected retry attempts to be recorded")
	}

	// The core invariant of Phase 2: with 100 requests and a budget of
	// burst=5, we should see at most ~5 retries complete before the
	// budget starts rejecting. This is the "cut retry amplification"
	// guarantee — without a budget we'd see 100×(MaxAttempts-1)=200
	// retry attempts hitting the upstream.
	totalServerHits := fake.totalRequests.Load()
	if totalServerHits > 115 {
		t.Errorf("budget failed to contain retry amplification: "+
			"server saw %d requests for 100 client calls (expected ~105 with burst=5)",
			totalServerHits)
	}

	assertInFlightGaugesZero(t, reg, "loadtest")
	logGoroutineDrift(t, baseline)
}

// Scenario: semaphore under-saturation. Drive more concurrent
// requests than the semaphore allows and verify that extra requests
// either queue successfully or reject with context deadline exceeded.
func TestStreamRetryLoad_SemaphoreBackPressure(t *testing.T) {
	_, reg := installTestMetrics(t)
	fake := newFakeOpenAI()
	defer fake.Close()
	// Slow first chunk so requests pile up on the semaphore.
	fake.setMode(failureMode{firstChunkDelay: 100 * time.Millisecond})

	// Semaphore limit of 10, drive 200 concurrent requests.
	sem := providers.NewStreamSemaphore(10)
	p := buildProvider(fake.URL(), providerOpts{semaphore: sem})

	// Use a short timeout so rejections accumulate visibly.
	succ, fail := driveRequests(t, p, 200, 200, 1500*time.Millisecond)

	rejections := counterSum(reg, "loadtest_stream_concurrency_rejections_total", "loadtest")
	t.Logf("results: success=%d fail=%d | rejections=%v", succ, fail, rejections)

	// With delay=100ms, limit=10, timeout=1500ms, we expect roughly
	// ~150 to complete and ~50 to time out on the semaphore. Exact
	// numbers depend on scheduling; assert only that the semaphore
	// actually exerted back-pressure (some rejections happened).
	if succ == 0 {
		t.Error("expected some successes")
	}
	if rejections == 0 && fail == 0 {
		t.Error("expected some back-pressure evidence (rejections or failures)")
	}
	assertInFlightGaugesZero(t, reg, "loadtest")
}

// Scenario: mid-stream failure invariant. The critical safety property
// of Phase 1 retry is that retry MUST NOT fire once a content chunk
// has been forwarded downstream. Violation would cause double-emission
// of content (corrupting tool-call arg accumulation, reasoning
// buffers, etc.) — the worst kind of bug because it's silent.
//
// This scenario drives N concurrent requests against a fake upstream
// that delivers one complete SSE event (satisfying the pre-first-chunk
// peek) then hijacks the connection mid-frame. The expected behavior:
//
//  1. The retry driver's peek returns successfully with the first
//     event — retry loop exits, result is handed to the stream
//     goroutine.
//  2. The stream goroutine's SSE scanner reads and emits the first
//     delta chunk to the caller.
//  3. Continued reads hit unexpected EOF mid-frame; scanner returns
//     an error which the provider surfaces as a terminal StreamChunk
//     with Error set.
//  4. Retry driver is already out of scope; the error propagates to
//     the caller without any retry attempt being recorded.
//
// The invariant this scenario enforces:
//
//   - Total server hits equals N (no retries attempted)
//   - stream_retries_total is zero across all outcomes
//   - Every caller sees at least one delta content chunk (the first)
//   - Every caller sees a terminal error (stream did not complete
//     normally)
//   - Total content fragments across all callers == N (exactly one
//     per caller, no duplication)
//   - In-flight gauges return to zero (stream goroutines cleaned up)
//
// If this scenario ever starts failing, the safety property of Phase 1
// has regressed and retry is double-emitting content.
func TestStreamRetryLoad_MidStreamFailureInvariant(t *testing.T) {
	_, reg := installTestMetrics(t)
	fake := newFakeOpenAI()
	defer fake.Close()
	fake.setMode(failureMode{midStreamRate: 1.0})

	// Retry enabled with a generous policy to prove it does NOT fire
	// for mid-stream failures even when allowed. If Phase 1's window
	// enforcement is broken, this test will see either more than N
	// server hits or retry counter increments.
	p := buildProvider(fake.URL(), providerOpts{
		policy: providers.StreamRetryPolicy{
			Enabled:      true,
			MaxAttempts:  3,
			InitialDelay: 5 * time.Millisecond,
			MaxDelay:     50 * time.Millisecond,
		},
		budget: providers.NewRetryBudget(100, 100),
	})

	const N = 20
	type capture struct {
		deltaCount int
		sawError   bool
		content    string
	}
	results := make([]capture, N)

	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ch, err := p.PredictStream(ctx, providers.PredictionRequest{
				Messages: []types.Message{{Role: "user", Content: "test"}},
			})
			if err != nil {
				// A request failing before any channel is returned
				// would indicate the initial HTTP dial failed; not
				// what we're testing here. Record as an error seen.
				results[idx] = capture{sawError: true}
				return
			}
			var cap capture
			for chunk := range ch {
				if chunk.Error != nil {
					cap.sawError = true
				}
				if chunk.Delta != "" {
					cap.deltaCount++
					cap.content += chunk.Delta
				}
			}
			results[idx] = cap
		}(i)
	}
	wg.Wait()

	// Invariant 1: server saw exactly N requests — no retries fired.
	if got := fake.totalRequests.Load(); got != N {
		t.Errorf("invariant violated: expected exactly %d server hits "+
			"(retry must NOT fire after first chunk), got %d", N, got)
	}

	// Invariant 2: retry counter should have zero increments for all
	// outcomes. Phase 1's window enforcement must keep the retry
	// driver from ever running its loop for these failures.
	for _, outcome := range []string{"failed", "exhausted", "budget_exhausted", "success"} {
		if got := labeledCounter(reg, "loadtest_stream_retries_total", "loadtest", "outcome", outcome); got != 0 {
			t.Errorf("invariant violated: stream_retries_total{outcome=%q} = %v, "+
				"expected 0 for mid-stream failures", outcome, got)
		}
	}

	// Invariant 3: every caller sees at least one delta and a terminal
	// error. If deltaCount is 0, the first-chunk delivery path is
	// broken; if sawError is false, the caller got silent truncation
	// (almost as bad as double-emission).
	for i, r := range results {
		if r.deltaCount == 0 {
			t.Errorf("stream %d: expected at least 1 delta, got 0 (content=%q)",
				i, r.content)
		}
		if !r.sawError {
			t.Errorf("stream %d: expected terminal error after mid-stream kill, got none",
				i)
		}
	}

	// Invariant 4: total delta count across all callers equals N.
	// The fake sends exactly one complete delta per request, so N
	// successful first-chunk deliveries should yield N total deltas.
	// More than N would indicate retry re-emitted content. Less
	// than N would indicate some streams lost even their first chunk.
	var totalDeltas int
	for _, r := range results {
		totalDeltas += r.deltaCount
	}
	if totalDeltas != N {
		t.Errorf("invariant violated: total delta count = %d, expected %d "+
			"(mismatch would indicate content duplication or lost first chunks)",
			totalDeltas, N)
	}

	// Invariant 5: in-flight gauges return to zero.
	assertInFlightGaugesZero(t, reg, "loadtest")

	// Server-side sanity: every request was routed through the
	// mid-stream kill path, not any other mode.
	if got := fake.midStreamKills.Load(); got != N {
		t.Errorf("expected %d mid-stream kills, got %d", N, got)
	}

	t.Logf("invariant validated: %d streams, %d deltas, %d mid-stream kills, 0 retries",
		N, totalDeltas, fake.midStreamKills.Load())
}

// --- Scale ramp scenario ---
//
// The scale ramp runs the healthy streaming path at increasing
// concurrency tiers and measures the resource cost at each tier. Its
// purpose is to validate (or refute) the "scale horizons" claims in
// docs/local-backlog/STREAMING_RETRY_AT_SCALE.md, which were napkin
// math when the design was drafted. The other load-test scenarios
// top out at 500 concurrent streams, so nothing in the existing
// suite actually exercises the 1k+ target that the design was sized
// for.
//
// At each tier we record:
//   - goroutine delta vs. the test's baseline (real stream
//     bookkeeping + HTTP keep-alive workers)
//   - heap-in-use delta via runtime.MemStats
//   - GC pause counts during the tier (forced GC at boundaries so
//     we only count pauses caused by our workload)
//   - wall-time throughput (requests/sec sustained)
//   - per-request latency p50 and p99
//   - success rate
//
// The scenario logs a formatted table at the end so the results can
// be copied into the design doc. It does NOT enforce strict
// invariants at each tier — the point is to surface whatever
// actually happens, not to fail the test when reality diverges from
// a napkin-math prediction. A divergence is diagnostic, not a bug.
//
// Run with:
//
//	go test -tags=loadtest -race -timeout=5m -v \
//	    -run TestStreamRetryLoad_ScaleRamp \
//	    ./runtime/providers/openai/
//
// -race is expensive at this scale; omit it if you want to measure
// uncontended throughput (the race detector roughly halves throughput
// but surfaces data races that would otherwise only appear in
// production).

// scaleRampTier is one row of the ramp report — one concurrency
// level's measured behavior.
type scaleRampTier struct {
	concurrency       int
	totalRequests     int
	successes         int64
	failures          int64
	wallTime          time.Duration
	requestsPerSec    float64
	goroutinesBefore  int
	goroutinesPeak    int
	goroutinesAfter   int
	heapInuseBeforeMB float64
	heapInusePeakMB   float64
	gcPauseCount      uint32
	gcPauseTotalMs    float64
	latencyP50        time.Duration
	latencyP99        time.Duration
}

func TestStreamRetryLoad_ScaleRamp(t *testing.T) {
	// Tiers chosen to bracket the design doc's "1k target":
	//   100   — sanity-check baseline
	//   500   — matches existing scenarios' top concurrency
	//   1000  — the design doc's stated target
	//   2000  — past target, should still work per doc
	//   3000  — user-requested "what if I'd picked this" point
	//   5000  — halfway to the doc's "10k binds on h2" claim
	//
	// 10000 and beyond are excluded by default to keep CI time
	// bounded (each tier runs 2× concurrency requests, so 10k tier
	// dispatches 20k requests). Operators who want to push further
	// can change the slice and re-run manually.
	tiers := []int{100, 500, 1000, 2000, 3000, 5000}

	_, _ = installTestMetrics(t)
	fake := newFakeOpenAI()
	defer fake.Close()

	// Provider: healthy upstream, retry enabled, no budget, no
	// semaphore. We want to stress the goroutine/gauge/channel
	// machinery, not the back-pressure paths (those are covered by
	// other scenarios).
	p := buildProvider(fake.URL(), providerOpts{
		policy: providers.StreamRetryPolicy{
			Enabled:      true,
			MaxAttempts:  2,
			InitialDelay: 50 * time.Millisecond,
			MaxDelay:     500 * time.Millisecond,
		},
	})

	var results []scaleRampTier
	for _, c := range tiers {
		result := runScaleRampTier(t, p, fake, c)
		results = append(results, result)

		// Cool-down between tiers: force GC and sleep briefly so
		// the next tier starts from a clean goroutine/heap baseline.
		runtime.GC()
		time.Sleep(200 * time.Millisecond)
	}

	// Print a table that can be copied into the design doc.
	t.Log("")
	t.Log("=== SCALE RAMP RESULTS ===")
	t.Log("concurrency | reqs  | ok   | fail | wall(s) | rps    | goroutines (before/peak/after) | heap(MB) before→peak | gcPauses | p50    | p99")
	t.Log("------------+-------+------+------+---------+--------+--------------------------------+----------------------+----------+--------+--------")
	for _, r := range results {
		t.Logf("%11d | %5d | %4d | %4d | %7.2f | %6.0f | %5d / %5d / %5d            | %8.1f → %7.1f    | %8d | %6v | %6v",
			r.concurrency, r.totalRequests, r.successes, r.failures,
			r.wallTime.Seconds(), r.requestsPerSec,
			r.goroutinesBefore, r.goroutinesPeak, r.goroutinesAfter,
			r.heapInuseBeforeMB, r.heapInusePeakMB,
			r.gcPauseCount,
			r.latencyP50.Round(time.Millisecond),
			r.latencyP99.Round(time.Millisecond),
		)
	}
	t.Log("")
}

// TestStreamRetryLoad_PushToDestruction is a deliberate "how hard can
// this go before it breaks" probe. Unlike the bounded ScaleRamp test
// it does not fail on errors — it keeps running and reports whatever
// the stack produced. Each tier is hard-capped at 60 seconds wall
// time; if a tier exceeds that or produces any failures, the test
// logs the result and stops at that tier.
//
// Run with:
//
//	go test -tags=loadtest -timeout=30m -v -run TestStreamRetryLoad_PushToDestruction ./runtime/providers/openai/
func TestStreamRetryLoad_PushToDestruction(t *testing.T) {
	// Progressively harder tiers. Stop as soon as one fails or times
	// out so we don't waste minutes on a wedged tier.
	// Tiers chosen to bracket observed stack behavior:
	//   5k–50k   — past the ScaleRamp ceiling, exercises real goroutine pressure
	//   100k–300k — single-process headroom; all observed clean in empirical runs
	//
	// 500k was measured as the first tier that produced failures in the
	// harness, but those failures are the 10-second per-request context
	// deadline being exceeded — at ~50k rps the fake SSE server's CPU
	// ceiling, 1M total requests need ~21s wall time and tail requests
	// sitting in the worker queue hit the deadline. That's a harness
	// artifact, not a stack failure, so 500k is intentionally NOT in the
	// default tier list. See the design doc's "Push to destruction" section.
	tiers := []int{5000, 10000, 25000, 50000, 100000, 200000, 300000}

	_, _ = installTestMetrics(t)
	fake := newFakeOpenAI()
	defer fake.Close()

	p := buildProvider(fake.URL(), providerOpts{
		policy: providers.StreamRetryPolicy{
			Enabled:      true,
			MaxAttempts:  2,
			InitialDelay: 50 * time.Millisecond,
			MaxDelay:     500 * time.Millisecond,
		},
	})

	var results []scaleRampTier
	for _, c := range tiers {
		t.Logf(">>> pushing to %d concurrent...", c)
		done := make(chan scaleRampTier, 1)
		go func(c int) {
			done <- runScaleRampTier(t, p, fake, c)
		}(c)

		var result scaleRampTier
		var timedOut bool
		select {
		case result = <-done:
		case <-time.After(120 * time.Second):
			timedOut = true
			t.Logf(">>> tier %d: TIMED OUT after 120s — stopping", c)
		}
		if timedOut {
			break
		}
		results = append(results, result)
		t.Logf(">>> tier %d: ok=%d fail=%d wall=%.2fs goroutines_peak=%d heap_peak_MB=%.1f p99=%v",
			c, result.successes, result.failures, result.wallTime.Seconds(),
			result.goroutinesPeak, result.heapInusePeakMB,
			result.latencyP99.Round(time.Millisecond))

		if result.failures > 0 {
			t.Logf(">>> tier %d had failures — stopping", c)
			break
		}

		// Cool-down: force GC + sleep so the next tier starts clean.
		runtime.GC()
		time.Sleep(500 * time.Millisecond)
	}

	t.Log("")
	t.Log("=== PUSH-TO-DESTRUCTION RESULTS ===")
	t.Log("concurrency | reqs   | ok     | fail  | wall(s) | rps     | goroutines_peak | heap_peak_MB | p50    | p99")
	t.Log("------------+--------+--------+-------+---------+---------+-----------------+--------------+--------+--------")
	for _, r := range results {
		t.Logf("%11d | %6d | %6d | %5d | %7.2f | %7.0f | %15d | %12.1f | %6v | %6v",
			r.concurrency, r.totalRequests, r.successes, r.failures,
			r.wallTime.Seconds(), r.requestsPerSec,
			r.goroutinesPeak, r.heapInusePeakMB,
			r.latencyP50.Round(time.Millisecond),
			r.latencyP99.Round(time.Millisecond),
		)
	}
	t.Log("")
}

// runScaleRampTier runs a single concurrency tier end-to-end and
// returns the measured resource cost. 2× concurrency is used for
// total request count so each worker processes ~2 requests on
// average — enough to exercise goroutine teardown/recreation
// without inflating wall time.
func runScaleRampTier(t *testing.T, p *Provider, fake *fakeOpenAI, concurrency int) scaleRampTier {
	t.Helper()
	totalRequests := concurrency * 2

	// Reset the fake's counters so per-tier server-side observations
	// are independent.
	fake.totalRequests.Store(0)
	fake.successes.Store(0)

	// Baseline: force GC and snapshot runtime state before we start
	// dispatching.
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	goroutinesBefore := runtime.NumGoroutine()
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)
	gcPausesBefore := memBefore.NumGC
	gcPauseTotalBefore := memBefore.PauseTotalNs

	// Peak goroutine tracking: a single background sampler that
	// polls runtime.NumGoroutine at 10ms intervals and remembers the
	// maximum seen between start and stop. Sampling rather than
	// peak-per-request keeps overhead negligible.
	var peakGoroutines atomic.Int32
	peakGoroutines.Store(int32(goroutinesBefore))
	var peakHeapInuse atomic.Uint64
	peakHeapInuse.Store(memBefore.HeapInuse)
	samplerStop := make(chan struct{})
	samplerDone := make(chan struct{})
	go func() {
		defer close(samplerDone)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-samplerStop:
				return
			case <-ticker.C:
				if g := int32(runtime.NumGoroutine()); g > peakGoroutines.Load() {
					peakGoroutines.Store(g)
				}
				var ms runtime.MemStats
				runtime.ReadMemStats(&ms)
				if ms.HeapInuse > peakHeapInuse.Load() {
					peakHeapInuse.Store(ms.HeapInuse)
				}
			}
		}
	}()

	// Collect per-request latencies so we can compute p50/p99 at
	// the end.
	latencies := make([]time.Duration, 0, totalRequests)
	var latenciesMu sync.Mutex

	// Drive the requests. Each worker pulls work items from a buffered
	// channel; all requests are queued up front, then workers start.
	work := make(chan struct{}, totalRequests)
	for i := 0; i < totalRequests; i++ {
		work <- struct{}{}
	}
	close(work)

	var succ, fail atomic.Int64
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range work {
				reqStart := time.Now()
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				ch, err := p.PredictStream(ctx, providers.PredictionRequest{
					Messages: []types.Message{{Role: "user", Content: "go"}},
				})
				if err != nil {
					fail.Add(1)
					cancel()
					continue
				}
				streamErr := drainStream(ch)
				cancel()
				elapsed := time.Since(reqStart)
				if streamErr != nil {
					fail.Add(1)
				} else {
					succ.Add(1)
					latenciesMu.Lock()
					latencies = append(latencies, elapsed)
					latenciesMu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	wallTime := time.Since(start)

	close(samplerStop)
	<-samplerDone

	// Post-state snapshot.
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	goroutinesAfter := runtime.NumGoroutine()
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)
	gcPauseCount := memAfter.NumGC - gcPausesBefore
	gcPauseTotalNs := memAfter.PauseTotalNs - gcPauseTotalBefore

	// Sort latencies to extract percentiles.
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	var p50, p99 time.Duration
	if n := len(latencies); n > 0 {
		p50 = latencies[n*50/100]
		if idx := n * 99 / 100; idx < n {
			p99 = latencies[idx]
		} else {
			p99 = latencies[n-1]
		}
	}

	const bytesPerMB = 1024.0 * 1024.0
	return scaleRampTier{
		concurrency:       concurrency,
		totalRequests:     totalRequests,
		successes:         succ.Load(),
		failures:          fail.Load(),
		wallTime:          wallTime,
		requestsPerSec:    float64(totalRequests) / wallTime.Seconds(),
		goroutinesBefore:  goroutinesBefore,
		goroutinesPeak:    int(peakGoroutines.Load()),
		goroutinesAfter:   goroutinesAfter,
		heapInuseBeforeMB: float64(memBefore.HeapInuse) / bytesPerMB,
		heapInusePeakMB:   float64(peakHeapInuse.Load()) / bytesPerMB,
		gcPauseCount:      gcPauseCount,
		gcPauseTotalMs:    float64(gcPauseTotalNs) / 1e6,
		latencyP50:        p50,
		latencyP99:        p99,
	}
}

// labeledCounter reads a counter matching both provider and an
// additional label (typically outcome or reason).
func labeledCounter(reg *prometheus.Registry, metricName, providerID, labelKey, labelVal string) float64 {
	mfs, _ := reg.Gather()
	for _, mf := range mfs {
		if mf.GetName() != metricName {
			continue
		}
		for _, m := range mf.GetMetric() {
			var providerMatches, labelMatches bool
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "provider" && lp.GetValue() == providerID {
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
