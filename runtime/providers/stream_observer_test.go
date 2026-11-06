package providers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// recordingObserver records every StreamObserver and StreamEventObserver call.
type recordingObserver struct {
	mu        sync.Mutex
	chunks    []StreamChunk
	errs      []error
	completes int
	tokens    int
	duration  time.Duration
	events    []StreamEvent
}

func (o *recordingObserver) OnChunk(c StreamChunk) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.chunks = append(o.chunks, c)
}

func (o *recordingObserver) OnComplete(totalTokens int, d time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.completes++
	o.tokens = totalTokens
	o.duration = d
}

func (o *recordingObserver) OnError(err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.errs = append(o.errs, err)
}

func (o *recordingObserver) OnStreamEvent(e StreamEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.events = append(o.events, e)
}

func (o *recordingObserver) eventTypes() []string {
	out := make([]string, 0, len(o.events))
	for _, e := range o.events {
		out = append(out, e.Type)
	}
	return out
}

func TestRunStreamingRequest_ObserverSeesHealthyStream(t *testing.T) {
	h := newRunStreamingTestHarness(t, []StreamChunk{
		{Delta: "hello ", Content: "hello ", TokenCount: 1},
		{Delta: "world", Content: "hello world", TokenCount: 2},
		{FinishReason: stringPtr("stop"), Content: "hello world", TokenCount: 2},
	})
	obs := &recordingObserver{}
	h.provider.SetStreamObserver(obs)

	got := h.run()

	if len(got) != 3 {
		t.Fatalf("caller got %d chunks, want 3", len(got))
	}
	if len(obs.chunks) != 3 || obs.chunks[1].Delta != "world" {
		t.Errorf("OnChunk saw %d chunks (%v), want the 3 delivered", len(obs.chunks), obs.chunks)
	}
	if obs.completes != 1 || obs.tokens != 2 || obs.duration <= 0 {
		t.Errorf("OnComplete calls=%d tokens=%d duration=%v, want 1 call, 2 tokens, positive duration",
			obs.completes, obs.tokens, obs.duration)
	}
	if len(obs.errs) != 0 {
		t.Errorf("OnError called for a healthy stream: %v", obs.errs)
	}
	want := []string{StreamEventChunk, StreamEventChunk, StreamEventChunk, StreamEventComplete}
	if gotTypes := obs.eventTypes(); !equalStrings(gotTypes, want) {
		t.Errorf("events = %v, want %v", gotTypes, want)
	}
	for _, e := range obs.events {
		if e.Timestamp.IsZero() || e.Chunk == nil {
			t.Errorf("event %+v must carry a timestamp and a chunk", e)
		}
	}
}

func TestRunStreamingRequest_ObserverSeesError(t *testing.T) {
	boom := errors.New("mid-stream kill")
	h := newRunStreamingTestHarness(t, []StreamChunk{
		{Delta: "partial"},
		{Error: boom, FinishReason: stringPtr("error")},
	})
	obs := &recordingObserver{}
	h.provider.SetStreamObserver(obs)

	_ = h.run()

	if len(obs.chunks) != 1 {
		t.Errorf("OnChunk saw %d chunks, want only the content chunk", len(obs.chunks))
	}
	if len(obs.errs) != 1 || !errors.Is(obs.errs[0], boom) {
		t.Errorf("OnError = %v, want [%v]", obs.errs, boom)
	}
	if obs.completes != 0 {
		t.Error("OnComplete must not be called for a failed stream")
	}
	want := []string{StreamEventChunk, StreamEventError}
	if gotTypes := obs.eventTypes(); !equalStrings(gotTypes, want) {
		t.Errorf("events = %v, want %v", gotTypes, want)
	}
	if !errors.Is(obs.events[1].Error, boom) {
		t.Errorf("error event Error = %v, want %v", obs.events[1].Error, boom)
	}
}

type panickingObserver struct{}

func (panickingObserver) OnChunk(StreamChunk)           { panic("observer bug") }
func (panickingObserver) OnComplete(int, time.Duration) { panic("observer bug") }
func (panickingObserver) OnError(error)                 { panic("observer bug") }
func (panickingObserver) OnStreamEvent(StreamEvent)     { panic("observer bug") }

func TestRunStreamingRequest_PanickingObserverDoesNotBreakStream(t *testing.T) {
	h := newRunStreamingTestHarness(t, []StreamChunk{
		{Delta: "a"},
		{Delta: "b"},
		{FinishReason: stringPtr("stop")},
	})
	h.provider.SetStreamObserver(panickingObserver{})

	if got := h.run(); len(got) != 3 {
		t.Fatalf("caller got %d chunks, want 3", len(got))
	}
}

func TestRunStreamingRequest_FinalChunkCarriesPredictionResponse(t *testing.T) {
	cost := &types.CostInfo{InputTokens: 3, OutputTokens: 2}
	call := types.MessageToolCall{ID: "c1", Name: "lookup"}
	h := newRunStreamingTestHarness(t, []StreamChunk{
		{Delta: "hi", Content: "hi"},
		{
			FinishReason: stringPtr("tool_use"), Content: "hi",
			ToolCalls: []types.MessageToolCall{call}, CostInfo: cost,
		},
	})

	got := h.run()

	if got[0].FinalResult != nil {
		t.Error("only the final chunk carries a FinalResult")
	}
	res, ok := got[1].FinalResult.(*PredictionResponse)
	if !ok {
		t.Fatalf("final FinalResult = %T, want *PredictionResponse", got[1].FinalResult)
	}
	if res.Content != "hi" || res.FinishReason != "tool_use" || res.CostInfo != cost ||
		len(res.ToolCalls) != 1 || res.ToolCalls[0].ID != "c1" || res.Latency <= 0 {
		t.Errorf("FinalResult = %+v, want the final chunk's content, finish reason, tool calls, cost and latency", res)
	}
}

func TestRunStreamingRequest_FailedStreamHasNoFinalResult(t *testing.T) {
	h := newRunStreamingTestHarness(t, []StreamChunk{
		{Delta: "partial", Content: "partial"},
		{Error: errors.New("boom"), FinishReason: stringPtr("error")},
	})
	for _, c := range h.run() {
		if c.FinalResult != nil {
			t.Fatalf("a failed stream must not report a FinalResult, got %+v", c.FinalResult)
		}
	}
}

func TestRunStreamingRequest_KeepsProviderFinalResult(t *testing.T) {
	own := &PredictionResponse{Content: "provider's own"}
	h := newRunStreamingTestHarness(t, []StreamChunk{
		{FinishReason: stringPtr("stop"), Content: "x", FinalResult: own},
	})
	if got := h.run(); got[0].FinalResult != ExecutionResult(own) {
		t.Errorf("FinalResult = %v, want the provider's own", got[0].FinalResult)
	}
}

func TestCreateProviderFromSpec_AppliesStreamObserver(t *testing.T) {
	factoryName := "test-stream-observer-provider"
	RegisterProviderFactory(factoryName, func(spec ProviderSpec) (Provider, error) {
		return &headersTestProvider{BaseProvider: NewBaseProvider(spec.ID, false, &http.Client{})}, nil
	})
	t.Cleanup(func() {
		providerFactoriesMu.Lock()
		delete(providerFactories, factoryName)
		providerFactoriesMu.Unlock()
	})

	obs := &recordingObserver{}
	p, err := CreateProviderFromSpec(ProviderSpec{ID: "obs", Type: factoryName, StreamObserver: obs})
	if err != nil {
		t.Fatalf("CreateProviderFromSpec: %v", err)
	}
	if got := p.(*headersTestProvider).streamObserver; got != StreamObserver(obs) {
		t.Errorf("stream observer = %v, want the spec's", got)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// After a mid-stream Reset the retried attempt decides the outcome: an error
// on the first attempt followed by a clean retry still completes.
func TestRunStreamingRequest_ObserverCompletesAfterSuccessfulReset(t *testing.T) {
	ResetDefaultStreamMetrics()
	t.Cleanup(ResetDefaultStreamMetrics)
	_ = RegisterDefaultStreamMetrics(prometheus.NewRegistry(), "test", nil)

	b := NewBaseProvider("test", false, &http.Client{
		Transport: &fixedResponseRoundTripper{body: "data: {}\n\ndata: [DONE]\n\n"},
	})
	obs := &recordingObserver{}
	b.SetStreamObserver(obs)
	policy := StreamRetryPolicy{
		Enabled: true, MaxAttempts: 2, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond,
		Window: StreamRetryWindowAlways,
	}
	req := &StreamRetryRequest{
		Policy: policy, Budget: NewRetryBudget(10, 5), ProviderName: "test", IdleTimeout: 5 * time.Second,
		RequestFn: func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, "POST", "http://test.invalid/x", http.NoBody)
		},
		Client: b.GetStreamingHTTPClient(),
	}
	var attempt atomic.Int32
	ch, err := b.RunStreamingRequest(context.Background(), req,
		func(_ context.Context, body io.ReadCloser, out chan<- StreamChunk) {
			defer close(out)
			defer func() { _ = body.Close() }()
			if attempt.Add(1) == 1 {
				out <- StreamChunk{Delta: "x", Content: "x"}
				out <- StreamChunk{Error: errors.New("mid-stream kill"), FinishReason: stringPtr("error")}
				return
			}
			out <- StreamChunk{FinishReason: stringPtr("stop"), Content: "fresh", TokenCount: 4}
		})
	if err != nil {
		t.Fatalf("RunStreamingRequest: %v", err)
	}
	for range ch { //nolint:revive // drain
	}

	want := []string{StreamEventChunk, StreamEventError, StreamEventChunk, StreamEventChunk, StreamEventComplete}
	if got := obs.eventTypes(); !equalStrings(got, want) {
		t.Errorf("events = %v, want %v (content, error, reset, final, complete)", got, want)
	}
	if obs.completes != 1 || obs.tokens != 4 {
		t.Errorf("OnComplete calls=%d tokens=%d, want 1 and 4", obs.completes, obs.tokens)
	}
}
