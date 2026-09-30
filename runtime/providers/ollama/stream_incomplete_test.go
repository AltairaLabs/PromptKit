package ollama

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// cancelOnEOFReader serves a fixed stream and cancels ctx the moment the
// stream reaches EOF — the shape of a body closed by the cancel goroutine.
type cancelOnEOFReader struct {
	r      io.Reader
	cancel context.CancelFunc
}

func (c *cancelOnEOFReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if errors.Is(err, io.EOF) {
		c.cancel()
	}
	return n, err
}

func (c *cancelOnEOFReader) Close() error { return nil }

func lastOllamaChunk(t *testing.T, ch <-chan providers.StreamChunk) providers.StreamChunk {
	t.Helper()
	var last providers.StreamChunk
	n := 0
	for c := range ch {
		last = c
		n++
	}
	if n == 0 {
		t.Fatal("stream produced no chunks")
	}
	return last
}

func serveOllamaSSE(t *testing.T, events []string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			_, _ = w.Write([]byte("data: " + e + "\n\n"))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestStreamIncomplete_TruncatedStreamEndsInError(t *testing.T) {
	srv := serveOllamaSSE(t, []string{
		`{"choices":[{"delta":{"content":"Hello"}}]}`,
		`{"choices":[{"delta":{"content":" wor"}}]}`,
	})
	p := NewProvider("test", "llama3", srv.URL, providers.ProviderDefaults{MaxTokens: 10}, false, nil)
	ch, err := p.PredictStream(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("PredictStream: %v", err)
	}
	last := lastOllamaChunk(t, ch)
	if !errors.Is(last.Error, io.ErrUnexpectedEOF) {
		t.Fatalf("last chunk error = %v, want io.ErrUnexpectedEOF", last.Error)
	}
	if last.Content != "Hello wor" {
		t.Errorf("last chunk content = %q, want accumulated %q", last.Content, "Hello wor")
	}
	if last.FinishReason != nil && *last.FinishReason == "stop" {
		t.Error("truncated stream must not finish with stop")
	}
}

func TestStreamIncomplete_FinishWithoutDoneIsComplete(t *testing.T) {
	srv := serveOllamaSSE(t, []string{
		`{"choices":[{"delta":{"content":"Hi"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	})
	p := NewProvider("test", "llama3", srv.URL, providers.ProviderDefaults{MaxTokens: 10}, false, nil)
	ch, err := p.PredictStream(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("PredictStream: %v", err)
	}
	last := lastOllamaChunk(t, ch)
	if last.Error != nil {
		t.Fatalf("unexpected error: %v", last.Error)
	}
	if last.FinishReason == nil || *last.FinishReason != "stop" {
		t.Errorf("finish reason = %v, want stop", last.FinishReason)
	}
	if last.Content != "Hi" {
		t.Errorf("content = %q, want Hi", last.Content)
	}
}

func TestStreamIncomplete_CanceledCleanEOFReportsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body := &cancelOnEOFReader{
		r: strings.NewReader(`data: {"choices":[{"delta":{"content":"Hello"},` +
			`"finish_reason":null}]}` + "\n\n"),
		cancel: cancel,
	}
	p := NewProvider("test", "llama3", "http://unused", providers.ProviderDefaults{}, false, nil)
	out := make(chan providers.StreamChunk, 8)
	p.streamResponse(ctx, body, out)

	last := lastOllamaChunk(t, out)
	if !errors.Is(last.Error, context.Canceled) {
		t.Fatalf("last chunk error = %v, want context.Canceled", last.Error)
	}
	if last.Content != "Hello" {
		t.Errorf("last chunk content = %q, want %q", last.Content, "Hello")
	}
	if last.FinishReason != nil && *last.FinishReason == "stop" {
		t.Error("canceled stream must not finish with stop")
	}
}
