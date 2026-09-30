package openai

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

type streamFunc func(ctx context.Context, body io.ReadCloser, out chan<- providers.StreamChunk)

// runOverHTTP serves body from an httptest server and feeds the live
// response body to the stream consumer, returning every emitted chunk.
func runOverHTTP(t *testing.T, body string, consume streamFunc) []providers.StreamChunk {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	resp, err := http.Get(srv.URL) //nolint:noctx // test-only request to a local server
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	out := make(chan providers.StreamChunk, 32)
	consume(context.Background(), resp.Body, out)
	return drainOpenAI(t, out)
}

func runCanceledAtEOF(t *testing.T, body string, consume streamFunc) []providers.StreamChunk {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan providers.StreamChunk, 32)
	consume(ctx, &cancelOnEOFReader{r: strings.NewReader(body), cancel: cancel}, out)
	return drainOpenAI(t, out)
}

func drainOpenAI(t *testing.T, out <-chan providers.StreamChunk) []providers.StreamChunk {
	t.Helper()
	var chunks []providers.StreamChunk
	for c := range out {
		chunks = append(chunks, c)
	}
	if len(chunks) == 0 {
		t.Fatal("stream produced no chunks")
	}
	return chunks
}

func newIncompleteTestProvider() *Provider {
	return NewProvider("test", "gpt-4o", "http://unused", providers.ProviderDefaults{}, false)
}

const (
	chatPartial = "data: {\"choices\":[{\"delta\":{\"content\":\"Hel\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c1\",\"type\":\"function\"," +
		"\"function\":{\"name\":\"lookup\",\"arguments\":\"{}\"}}]}}]}\n\n"
	responsesPartial = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"Hel\"}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"lo\"}\n\n"
)

func TestStreamIncomplete_ChatTruncatedEndsInError(t *testing.T) {
	chunks := runOverHTTP(t, chatPartial, newIncompleteTestProvider().streamResponse)
	last := chunks[len(chunks)-1]
	if !errors.Is(last.Error, io.ErrUnexpectedEOF) {
		t.Fatalf("last chunk error = %v, want io.ErrUnexpectedEOF", last.Error)
	}
	if last.Content != "Hello" {
		t.Errorf("content = %q, want Hello", last.Content)
	}
	if len(last.ToolCalls) != 1 || last.ToolCalls[0].Name != "lookup" {
		t.Errorf("tool calls = %+v, want the accumulated lookup call", last.ToolCalls)
	}
}

func TestStreamIncomplete_ChatFinishWithoutDoneIsComplete(t *testing.T) {
	body := chatPartial + "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"
	chunks := runOverHTTP(t, body, newIncompleteTestProvider().streamResponse)
	last := chunks[len(chunks)-1]
	if last.Error != nil {
		t.Fatalf("unexpected error: %v", last.Error)
	}
	if last.FinishReason == nil || *last.FinishReason != types.FinishReasonToolUse {
		t.Errorf("finish reason = %v, want %q", last.FinishReason, types.FinishReasonToolUse)
	}
	if last.Content != "Hello" {
		t.Errorf("content = %q, want Hello", last.Content)
	}
}

func TestStreamIncomplete_ChatDoneCompletes(t *testing.T) {
	chunks := runOverHTTP(t, chatPartial+"data: [DONE]\n\n", newIncompleteTestProvider().streamResponse)
	if last := chunks[len(chunks)-1]; last.Error != nil {
		t.Fatalf("unexpected error: %v", last.Error)
	}
}

func TestStreamIncomplete_ChatCanceledReportsCancellation(t *testing.T) {
	chunks := runCanceledAtEOF(t, chatPartial, newIncompleteTestProvider().streamResponse)
	last := chunks[len(chunks)-1]
	if !errors.Is(last.Error, context.Canceled) {
		t.Fatalf("last chunk error = %v, want context.Canceled", last.Error)
	}
	if last.Content != "Hello" {
		t.Errorf("content = %q, want Hello", last.Content)
	}
}

func TestStreamIncomplete_ResponsesTruncatedEndsInError(t *testing.T) {
	chunks := runOverHTTP(t, responsesPartial, newIncompleteTestProvider().streamResponsesResponse)
	last := chunks[len(chunks)-1]
	if !errors.Is(last.Error, io.ErrUnexpectedEOF) {
		t.Fatalf("last chunk error = %v, want io.ErrUnexpectedEOF", last.Error)
	}
	if last.Content != "Hello" {
		t.Errorf("content = %q, want Hello", last.Content)
	}
}

func TestStreamIncomplete_ResponsesCanceledReportsCancellation(t *testing.T) {
	chunks := runCanceledAtEOF(t, responsesPartial, newIncompleteTestProvider().streamResponsesResponse)
	last := chunks[len(chunks)-1]
	if !errors.Is(last.Error, context.Canceled) {
		t.Fatalf("last chunk error = %v, want context.Canceled", last.Error)
	}
	if last.Content != "Hello" {
		t.Errorf("content = %q, want Hello", last.Content)
	}
}

func TestStreamIncomplete_ResponsesCompletedIsClean(t *testing.T) {
	body := responsesPartial + "data: {\"type\":\"response.completed\",\"response\":{\"usage\":" +
		"{\"input_tokens\":3,\"output_tokens\":2}}}\n\n"
	chunks := runOverHTTP(t, body, newIncompleteTestProvider().streamResponsesResponse)
	last := chunks[len(chunks)-1]
	if last.Error != nil {
		t.Fatalf("unexpected error: %v", last.Error)
	}
	if last.FinishReason == nil || *last.FinishReason != "stop" {
		t.Errorf("finish reason = %v, want stop", last.FinishReason)
	}
}

func TestStreamIncomplete_ResponsesIncompleteEventIsLengthFinish(t *testing.T) {
	body := responsesPartial + "data: {\"type\":\"response.incomplete\",\"response\":{\"status\":\"incomplete\"," +
		"\"incomplete_details\":{\"reason\":\"max_output_tokens\"}}}\n\n"
	chunks := runOverHTTP(t, body, newIncompleteTestProvider().streamResponsesResponse)
	last := chunks[len(chunks)-1]
	if last.Error != nil {
		t.Fatalf("unexpected error: %v", last.Error)
	}
	if last.FinishReason == nil || *last.FinishReason != types.FinishReasonMaxOutputTokens {
		t.Errorf("finish reason = %v, want %q", last.FinishReason, types.FinishReasonMaxOutputTokens)
	}
	if last.Content != "Hello" {
		t.Errorf("content = %q, want Hello", last.Content)
	}
}

func TestStreamIncomplete_ResponsesFailedEventIsError(t *testing.T) {
	body := responsesPartial + "data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\"," +
		"\"error\":{\"code\":\"server_error\",\"message\":\"boom\"}}}\n\n"
	chunks := runOverHTTP(t, body, newIncompleteTestProvider().streamResponsesResponse)
	last := chunks[len(chunks)-1]
	if last.Error == nil || !strings.Contains(last.Error.Error(), "boom") {
		t.Fatalf("last chunk error = %v, want the failure message", last.Error)
	}
	if last.Content != "Hello" {
		t.Errorf("content = %q, want Hello", last.Content)
	}
}
