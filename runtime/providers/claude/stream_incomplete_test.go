package claude

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

const claudeTruncatedEvents = "event: message_start\ndata: {\"type\":\"message_start\"," +
	"\"message\":{\"id\":\"msg_1\",\"role\":\"assistant\"}}\n\n" +
	"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0," +
	"\"delta\":{\"type\":\"text_delta\",\"text\":\"partial\"}}\n\n" +
	"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1," +
	"\"content_block\":{\"type\":\"tool_use\",\"id\":\"tu_1\",\"name\":\"lookup\"}}\n\n"

func lastClaudeChunk(t *testing.T, ch <-chan providers.StreamChunk) providers.StreamChunk {
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

func streamClaudeSSE(t *testing.T, body string) providers.StreamChunk {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	ch, err := newFinishReasonTestProvider(srv.URL).PredictStream(context.Background(),
		providers.PredictionRequest{Messages: []types.Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("PredictStream: %v", err)
	}
	return lastClaudeChunk(t, ch)
}

func TestStreamIncomplete_EOFWithoutMessageStopIsError(t *testing.T) {
	last := streamClaudeSSE(t, claudeTruncatedEvents)
	if !errors.Is(last.Error, io.ErrUnexpectedEOF) {
		t.Fatalf("last chunk error = %v, want io.ErrUnexpectedEOF", last.Error)
	}
	if last.Content != "partial" {
		t.Errorf("content = %q, want %q", last.Content, "partial")
	}
	if len(last.ToolCalls) != 1 || last.ToolCalls[0].Name != "lookup" {
		t.Errorf("tool calls = %+v, want the accumulated lookup call", last.ToolCalls)
	}
}

func TestStreamIncomplete_MessageStopCompletes(t *testing.T) {
	last := streamClaudeSSE(t, claudeTruncatedEvents+
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	if last.Error != nil {
		t.Fatalf("unexpected error: %v", last.Error)
	}
	if last.FinishReason == nil {
		t.Fatal("expected a finish reason on the final chunk")
	}
}

func TestStreamIncomplete_CanceledCleanEOFReportsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body := &cancelOnEOFReader{r: strings.NewReader(claudeTruncatedEvents), cancel: cancel}
	p := newFinishReasonTestProvider("http://unused")
	out := make(chan providers.StreamChunk, 16)
	p.streamResponse(ctx, body, providers.NewSSEScanner(body), out)

	last := lastClaudeChunk(t, out)
	if !errors.Is(last.Error, context.Canceled) {
		t.Fatalf("last chunk error = %v, want context.Canceled", last.Error)
	}
	if last.Content != "partial" {
		t.Errorf("content = %q, want %q", last.Content, "partial")
	}
}

// TestStreamErrorEvent_SurfacesServerError covers Anthropic's mid-stream error
// event: the stream ends on the server's error, with the text produced so far.
func TestStreamErrorEvent_SurfacesServerError(t *testing.T) {
	body := claudeTruncatedEvents +
		"event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n"
	last := streamClaudeSSE(t, body)
	if !errors.Is(last.Error, ErrClaudeStreamError) {
		t.Fatalf("last chunk error = %v, want ErrClaudeStreamError", last.Error)
	}
	if !strings.Contains(last.Error.Error(), "overloaded_error") || !strings.Contains(last.Error.Error(), "Overloaded") {
		t.Errorf("error %q lost the server's type or message", last.Error)
	}
	if last.Content != "partial" {
		t.Errorf("content = %q, want the text produced before the error", last.Content)
	}
}

func TestParseClaudeStreamError_UnparseableKeepsRawEvent(t *testing.T) {
	err := parseClaudeStreamError([]byte(`{"type":"error"}`))
	if !errors.Is(err, ErrClaudeStreamError) || !strings.Contains(err.Error(), `{"type":"error"}`) {
		t.Fatalf("err = %v, want ErrClaudeStreamError carrying the raw event", err)
	}
}
