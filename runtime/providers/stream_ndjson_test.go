package providers

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// A provider whose API streams newline-delimited JSON (Ollama's native API,
// for one) opens its stream through RunStreamingRequest with
// NDJSONFrameDetector. The SSE default never finds a frame in such a body; the
// NDJSON detector does, and the consumer then reads the whole body, the
// peeked first frame included.
func TestRunStreamingRequest_NDJSONFrameDetector(t *testing.T) {
	ResetDefaultStreamMetrics()
	t.Cleanup(ResetDefaultStreamMetrics)
	_ = RegisterDefaultStreamMetrics(prometheus.NewRegistry(), "test", nil)

	const body = "\n{\"response\":\"hel\"}\n{\"response\":\"lo\",\"done\":true}\n"
	b := NewBaseProvider("test", false, &http.Client{Transport: &fixedResponseRoundTripper{body: body}})
	newReq := func(detector FrameDetector) *StreamRetryRequest {
		return &StreamRetryRequest{
			ProviderName: "test", IdleTimeout: 5 * time.Second, FrameDetector: detector,
			RequestFn: func(ctx context.Context) (*http.Request, error) {
				return http.NewRequestWithContext(ctx, "POST", "http://test.invalid/api/chat", http.NoBody)
			},
			Client: b.GetStreamingHTTPClient(),
		}
	}
	consumer := func(_ context.Context, rc io.ReadCloser, out chan<- StreamChunk) {
		defer close(out)
		defer func() { _ = rc.Close() }()
		raw, err := io.ReadAll(rc)
		if err != nil {
			out <- StreamChunk{Error: err}
			return
		}
		out <- StreamChunk{Content: string(raw), FinishReason: stringPtr("stop")}
	}

	if _, err := b.RunStreamingRequest(context.Background(), newReq(nil), consumer); err == nil {
		t.Fatal("the SSE default must not accept an NDJSON body")
	}

	ch, err := b.RunStreamingRequest(context.Background(), newReq(NDJSONFrameDetector{}), consumer)
	if err != nil {
		t.Fatalf("RunStreamingRequest with NDJSONFrameDetector: %v", err)
	}
	var got []StreamChunk
	for c := range ch {
		got = append(got, c)
	}
	if len(got) != 1 || got[0].Content != body {
		t.Fatalf("consumer got %+v, want one chunk holding the whole body %q", got, body)
	}
}
