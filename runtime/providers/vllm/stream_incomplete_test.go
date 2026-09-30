package vllm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

const (
	vllmContentEvent = "data: {\"choices\":[{\"delta\":{\"content\":\"Hel\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"lo\"},\"finish_reason\":null}]}\n\n"
	vllmToolCallEvent = "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\"," +
		"\"type\":\"function\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"{}\"}}]}," +
		"\"finish_reason\":null}]}\n\n"
)

func drainVLLM(ch <-chan providers.StreamChunk) []providers.StreamChunk {
	var out []providers.StreamChunk
	for c := range ch {
		out = append(out, c)
	}
	return out
}

func serveVLLMSSE(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func streamVLLMTools(t *testing.T, body string) []providers.StreamChunk {
	t.Helper()
	srv := serveVLLMSSE(t, body)
	p := NewProvider("vllm-test", "m", srv.URL, providers.ProviderDefaults{}, false, nil)
	ch, err := p.PredictStreamWithTools(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	}, nil, "")
	require.NoError(t, err)
	return drainVLLM(ch)
}

func TestStreamIncomplete_ToolStreamCarriesContent(t *testing.T) {
	chunks := streamVLLMTools(t, vllmContentEvent+
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	require.NotEmpty(t, chunks)
	last := chunks[len(chunks)-1]
	require.NoError(t, last.Error)
	require.NotNil(t, last.FinishReason)
	assert.Equal(t, "Hello", last.Content, "tool-stream text must reach Content")
	for _, c := range chunks {
		if c.Delta != "" {
			assert.NotEmpty(t, c.Content, "content delta chunk must carry accumulated Content")
		}
	}
}

func TestStreamIncomplete_ToolStreamTruncatedEndsInError(t *testing.T) {
	chunks := streamVLLMTools(t, vllmContentEvent+vllmToolCallEvent)
	require.NotEmpty(t, chunks)
	last := chunks[len(chunks)-1]
	require.ErrorIs(t, last.Error, io.ErrUnexpectedEOF)
	assert.Equal(t, "Hello", last.Content)
	require.Len(t, last.ToolCalls, 1)
	assert.Equal(t, "get_weather", last.ToolCalls[0].Name)
}

func TestStreamIncomplete_ToolStreamCanceledReportsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body := &cancelOnEOFReader{r: strings.NewReader(vllmContentEvent + vllmToolCallEvent), cancel: cancel}
	p := newTestVLLMProvider(t)
	ch := make(chan providers.StreamChunk, 16)
	p.streamToolResponse(ctx, body, ch)
	chunks := drainVLLM(ch)
	require.NotEmpty(t, chunks)
	last := chunks[len(chunks)-1]
	require.ErrorIs(t, last.Error, context.Canceled)
	assert.Equal(t, "Hello", last.Content)
	require.Len(t, last.ToolCalls, 1)
}

func TestStreamIncomplete_ToolStreamCancelMidStream(t *testing.T) {
	// Pre-canceled ctx: the in-loop check must still end on ctx.Err(),
	// not a silent return.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := newTestVLLMProvider(t)
	ch := make(chan providers.StreamChunk, 16)
	p.streamToolResponse(ctx, io.NopCloser(strings.NewReader(vllmContentEvent)), ch)
	chunks := drainVLLM(ch)
	require.NotEmpty(t, chunks)
	require.ErrorIs(t, chunks[len(chunks)-1].Error, context.Canceled)
}

func TestStreamIncomplete_PlainStreamTruncatedEndsInError(t *testing.T) {
	srv := serveVLLMSSE(t, vllmContentEvent)
	p := NewProvider("vllm-test", "m", srv.URL, providers.ProviderDefaults{}, false, nil)
	ch, err := p.PredictStream(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	chunks := drainVLLM(ch)
	require.NotEmpty(t, chunks)
	last := chunks[len(chunks)-1]
	require.ErrorIs(t, last.Error, io.ErrUnexpectedEOF)
	assert.Equal(t, "Hello", last.Content)
}

func TestStreamIncomplete_PlainStreamFinishWithoutDoneIsComplete(t *testing.T) {
	srv := serveVLLMSSE(t, vllmContentEvent+"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
	p := NewProvider("vllm-test", "m", srv.URL, providers.ProviderDefaults{}, false, nil)
	ch, err := p.PredictStream(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	chunks := drainVLLM(ch)
	require.NotEmpty(t, chunks)
	for _, c := range chunks {
		require.NoError(t, c.Error, "a finish_reason without [DONE] is a normal completion")
	}
	assert.Equal(t, "Hello", chunks[len(chunks)-1].Content, "the whole reply arrives")
}

func TestStreamIncomplete_PlainStreamCanceledKeepsContent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body := &cancelOnEOFReader{r: strings.NewReader(vllmContentEvent), cancel: cancel}
	p := newTestVLLMProvider(t)
	ch := make(chan providers.StreamChunk, 16)
	p.streamResponse(ctx, body, ch)
	chunks := drainVLLM(ch)
	require.NotEmpty(t, chunks)
	last := chunks[len(chunks)-1]
	require.ErrorIs(t, last.Error, context.Canceled)
	assert.Equal(t, "Hello", last.Content)
}
