package providers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// cancelTestRequest builds a stream request against a canned body. The
// consumer under test decides what the stream does; the body is never read.
func cancelTestRequest(b *BaseProvider, policy StreamRetryPolicy) *StreamRetryRequest {
	return &StreamRetryRequest{
		Policy:       policy,
		Budget:       NewRetryBudget(10, 5),
		ProviderName: "test",
		IdleTimeout:  5 * time.Second,
		RequestFn: func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, "POST", "http://test.invalid/x", http.NoBody)
		},
		Client: b.GetStreamingHTTPClient(),
	}
}

func cancelTestProvider() *BaseProvider {
	b := NewBaseProvider("test", false, &http.Client{
		Transport: &fixedResponseRoundTripper{body: "data: {}\n\n"},
	})
	return &b
}

// drainCancellingAfter reads the stream, cancelling once `after` chunks have
// arrived, and returns every chunk received.
func drainCancellingAfter(ch <-chan StreamChunk, cancel context.CancelFunc, after int) []StreamChunk {
	var got []StreamChunk
	for c := range ch {
		got = append(got, c)
		if len(got) == after {
			cancel()
		}
	}
	return got
}

func TestRunStreamingRequest_CanceledStreamEndsOnCancellation(t *testing.T) {
	cases := []struct {
		name string
		// end is what the parser does once the body is closed under it.
		end func(out chan<- StreamChunk)
	}{
		{
			name: "read error from the closed body",
			end: func(out chan<- StreamChunk) {
				out <- StreamChunk{Error: errors.New("read tcp: use of closed network connection")}
			},
		},
		{
			name: "clean EOF with no error",
			end:  func(chan<- StreamChunk) {},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := cancelTestProvider()
			consumer := func(ctx context.Context, body io.ReadCloser, out chan<- StreamChunk) {
				defer close(out)
				defer func() { _ = body.Close() }()
				out <- StreamChunk{Reasoning: "thinking"}
				out <- StreamChunk{Delta: "partial", Content: "partial"}
				<-ctx.Done()
				tc.end(out)
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ch, err := b.RunStreamingRequest(ctx, cancelTestRequest(b, StreamRetryPolicy{}), consumer)
			if err != nil {
				t.Fatalf("RunStreamingRequest: %v", err)
			}
			got := drainCancellingAfter(ch, cancel, 2)

			last := got[len(got)-1]
			if !errors.Is(last.Error, context.Canceled) {
				t.Fatalf("last chunk error = %v, want context.Canceled (chunks: %+v)", last.Error, got)
			}
		})
	}
}

func TestRunStreamingRequest_FinishedStreamIgnoresLaterCancel(t *testing.T) {
	b := cancelTestProvider()
	consumer := func(ctx context.Context, body io.ReadCloser, out chan<- StreamChunk) {
		defer close(out)
		defer func() { _ = body.Close() }()
		out <- StreamChunk{Delta: "done", Content: "done"}
		out <- StreamChunk{Content: "done", FinishReason: stringPtr("stop")}
		<-ctx.Done()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := b.RunStreamingRequest(ctx, cancelTestRequest(b, StreamRetryPolicy{}), consumer)
	if err != nil {
		t.Fatalf("RunStreamingRequest: %v", err)
	}
	got := drainCancellingAfter(ch, cancel, 2)

	for _, c := range got {
		if c.Error != nil {
			t.Fatalf("a stream that finished before the cancel reported %v", c.Error)
		}
	}
}

func TestRunStreamingRequest_CanceledStreamIsNotRetried(t *testing.T) {
	b := cancelTestProvider()
	var attempts atomic.Int32
	consumer := func(ctx context.Context, body io.ReadCloser, out chan<- StreamChunk) {
		defer close(out)
		defer func() { _ = body.Close() }()
		attempts.Add(1)
		out <- StreamChunk{Delta: "partial", Content: "partial"}
		<-ctx.Done()
		out <- StreamChunk{Error: errors.New("read on closed body")}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	policy := StreamRetryPolicy{
		Enabled: true, MaxAttempts: 2, InitialDelay: time.Millisecond,
		MaxDelay: time.Millisecond, Window: StreamRetryWindowAlways,
	}
	ch, err := b.RunStreamingRequest(ctx, cancelTestRequest(b, policy), consumer)
	if err != nil {
		t.Fatalf("RunStreamingRequest: %v", err)
	}
	got := drainCancellingAfter(ch, cancel, 1)

	for _, c := range got {
		if c.Reset {
			t.Fatal("a canceled stream was reset for a retry, discarding its partial output")
		}
	}
	if n := attempts.Load(); n != 1 {
		t.Errorf("consumer ran %d times, want 1", n)
	}
	if last := got[len(got)-1]; !errors.Is(last.Error, context.Canceled) {
		t.Errorf("last chunk error = %v, want context.Canceled", last.Error)
	}
}

// TestRunStreamingRequest_ReasoningOnlyFailureIsMidStream checks a stream that
// fails after delivering only reasoning takes the mid-stream retry: the caller
// has already seen that reasoning, so the retry must reset it.
func TestRunStreamingRequest_ReasoningOnlyFailureIsMidStream(t *testing.T) {
	b := cancelTestProvider()
	var attempts atomic.Int32
	consumer := func(_ context.Context, body io.ReadCloser, out chan<- StreamChunk) {
		defer close(out)
		defer func() { _ = body.Close() }()
		if attempts.Add(1) == 1 {
			out <- StreamChunk{Reasoning: "thinking"}
			out <- StreamChunk{Error: errors.New("upstream dropped")}
			return
		}
		out <- StreamChunk{Delta: "answer", Content: "answer", FinishReason: stringPtr("stop")}
	}

	policy := StreamRetryPolicy{
		Enabled: true, MaxAttempts: 2, InitialDelay: time.Millisecond,
		MaxDelay: time.Millisecond, Window: StreamRetryWindowAlways,
	}
	ch, err := b.RunStreamingRequest(context.Background(), cancelTestRequest(b, policy), consumer)
	if err != nil {
		t.Fatalf("RunStreamingRequest: %v", err)
	}
	var sawReset bool
	var last StreamChunk
	for c := range ch {
		sawReset = sawReset || c.Reset
		last = c
	}
	if !sawReset {
		t.Fatal("reasoning-only failure was not retried with a Reset")
	}
	if last.Error != nil || last.Content != "answer" {
		t.Fatalf("last chunk = %+v, want the retry's answer", last)
	}
}
