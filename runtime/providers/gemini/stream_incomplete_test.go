package gemini

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
)

// cancelOnEOFReader serves a fixed stream and cancels ctx the moment the
// stream reaches EOF — the shape of a body cut off by a cancellation.
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

// interactionsPartial is a stream that stops mid-answer, before
// interaction.completed, with a function call step already started.
var interactionsPartial = sse(
	`event: step.start`,
	`data: {"index":0,"step":{"type":"model_output"}}`,
	``,
	`event: step.delta`,
	`data: {"index":0,"delta":{"type":"text","text":"Hel"}}`,
	``,
	`event: step.delta`,
	`data: {"index":0,"delta":{"type":"text","text":"lo"}}`,
	``,
	`event: step.start`,
	`data: {"index":1,"step":{"id":"call_1","type":"function_call","name":"probe"}}`,
	``,
)

func drainGemini(t *testing.T, ch <-chan providers.StreamChunk) []providers.StreamChunk {
	t.Helper()
	var chunks []providers.StreamChunk
	for c := range ch {
		chunks = append(chunks, c)
	}
	require.NotEmpty(t, chunks)
	return chunks
}

func newIncompleteInteractionsProvider(t *testing.T, url string) *ToolProvider {
	t.Helper()
	t.Setenv("GEMINI_API_KEY", "test-key")
	return NewToolProvider("g-stream", "gemini-3.7-flash", url,
		providers.ProviderDefaults{MaxTokens: 512}, false)
}

func TestStreamIncomplete_InteractionsTruncatedEndsInError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(interactionsPartial))
	}))
	defer srv.Close()

	tp := newIncompleteInteractionsProvider(t, srv.URL)
	ch, err := tp.predictStreamWithInteractions(context.Background(), textRequest(), nil)
	require.NoError(t, err)

	last := drainGemini(t, ch)
	final := last[len(last)-1]
	require.ErrorIs(t, final.Error, io.ErrUnexpectedEOF)
	assert.Equal(t, "Hello", final.Content)
	require.Len(t, final.ToolCalls, 1)
	assert.Equal(t, "probe", final.ToolCalls[0].Name)
	assert.Nil(t, final.FinishReason, "an interrupted stream must not report a finish")
}

func TestStreamIncomplete_InteractionsCanceledCleanEOF(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tp := newIncompleteInteractionsProvider(t, "http://unused")
	out := make(chan providers.StreamChunk, streamChunkBuffer)
	tp.consumeInteractionsStream(ctx,
		&cancelOnEOFReader{r: strings.NewReader(interactionsPartial), cancel: cancel}, out)

	chunks := drainGemini(t, out)
	final := chunks[len(chunks)-1]
	require.ErrorIs(t, final.Error, context.Canceled)
	assert.Equal(t, "Hello", final.Content)
}

func TestStreamIncomplete_InteractionsCanceledMidStream(t *testing.T) {
	// A canceled ctx makes emit fail on the first delta; the stream must still
	// end on ctx.Err() rather than a silent close.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tp := newIncompleteInteractionsProvider(t, "http://unused")
	out := make(chan providers.StreamChunk, streamChunkBuffer)
	tp.consumeInteractionsStream(ctx, io.NopCloser(strings.NewReader(interactionsPartial)), out)

	chunks := drainGemini(t, out)
	final := chunks[len(chunks)-1]
	require.ErrorIs(t, final.Error, context.Canceled)
	// emit races out against ctx.Done, so either delta may be the last one
	// accepted; both leave the accumulated text on the terminal chunk.
	assert.True(t, strings.HasPrefix(final.Content, "Hel"),
		"content accumulated before the cancel must be kept, got %q", final.Content)
}
