package integration

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/sdk"
)

// TestStream_ChunkSequence verifies that Stream returns at least one text chunk
// followed by exactly one ChunkDone as the final chunk with a populated Response.
func TestStream_ChunkSequence(t *testing.T) {
	conv := openTestConv(t)
	ctx := context.Background()

	ch := conv.Stream(ctx, "Hello")

	var chunks []sdk.StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	require.NotEmpty(t, chunks, "expected at least one chunk from Stream")

	// Check for errors in any chunk
	for i, chunk := range chunks {
		require.NoError(t, chunk.Error, "chunk %d had an error", i)
	}

	// Verify at least one ChunkText with non-empty text
	hasText := false
	for _, chunk := range chunks {
		if chunk.Type == sdk.ChunkText && chunk.Text != "" {
			hasText = true
			break
		}
	}
	assert.True(t, hasText, "expected at least one ChunkText with non-empty text")

	// Verify exactly one ChunkDone as the last chunk
	doneCount := 0
	for _, chunk := range chunks {
		if chunk.Type == sdk.ChunkDone {
			doneCount++
		}
	}
	assert.Equal(t, 1, doneCount, "expected exactly one ChunkDone")

	lastChunk := chunks[len(chunks)-1]
	assert.Equal(t, sdk.ChunkDone, lastChunk.Type, "last chunk should be ChunkDone")

	// Verify ChunkDone has a populated Message (Response) with non-empty text
	require.NotNil(t, lastChunk.Message, "ChunkDone should have a populated Message")
	assert.NotEmpty(t, lastChunk.Message.Text(), "ChunkDone.Message.Text() should be non-empty")
}

// TestStream_WithCallback verifies that StreamWithCallback dispatches the expected
// event types and returns a valid Response.
func TestStream_WithCallback(t *testing.T) {
	conv := openTestConv(t)
	ctx := context.Background()

	var mu sync.Mutex
	var collectedEvents []sdk.StreamEvent

	conv.OnStreamEvent(func(event sdk.StreamEvent) {
		mu.Lock()
		defer mu.Unlock()
		collectedEvents = append(collectedEvents, event)
	})

	resp, err := conv.StreamWithCallback(ctx, "Hello")
	require.NoError(t, err)
	require.NotNil(t, resp, "StreamWithCallback should return a non-nil Response")
	assert.NotEmpty(t, resp.Text(), "Response.Text() should be non-empty")

	mu.Lock()
	events := make([]sdk.StreamEvent, len(collectedEvents))
	copy(events, collectedEvents)
	mu.Unlock()

	// Verify TextDeltaEvent received with text
	hasTextDelta := false
	for _, ev := range events {
		if td, ok := ev.(sdk.TextDeltaEvent); ok && td.Delta != "" {
			hasTextDelta = true
			break
		}
	}
	assert.True(t, hasTextDelta, "expected at least one TextDeltaEvent with non-empty Delta")

	// Verify StreamDoneEvent received with Response
	hasDone := false
	for _, ev := range events {
		if done, ok := ev.(sdk.StreamDoneEvent); ok {
			hasDone = true
			require.NotNil(t, done.Response, "StreamDoneEvent should have a non-nil Response")
			assert.NotEmpty(t, done.Response.Text(), "StreamDoneEvent.Response.Text() should be non-empty")
			break
		}
	}
	assert.True(t, hasDone, "expected at least one StreamDoneEvent")
}

// TestStream_ContextCancellation verifies that cancelling the context during
// streaming causes the channel to close without hanging or leaking goroutines.
func TestStream_ContextCancellation(t *testing.T) {
	conv := openTestConv(t)
	ctx, cancel := context.WithCancel(context.Background())

	ch := conv.Stream(ctx, "Hello")

	// Read the first chunk (if available) and then cancel
	select {
	case _, ok := <-ch:
		if ok {
			cancel()
		}
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("timed out waiting for first chunk")
	}

	// Capture goroutine count after cancellation
	goroutinesBefore := runtime.NumGoroutine()

	// Drain remaining chunks — the channel must close
	drained := make(chan struct{})
	go func() {
		for range ch {
			// discard remaining chunks
		}
		close(drained)
	}()

	select {
	case <-drained:
		// Channel closed as expected
	case <-time.After(5 * time.Second):
		t.Fatal("stream channel did not close after context cancellation")
	}

	// Allow goroutines to settle
	time.Sleep(100 * time.Millisecond)

	goroutinesAfter := runtime.NumGoroutine()
	delta := goroutinesAfter - goroutinesBefore
	assert.LessOrEqual(t, delta, 5,
		"goroutine delta after cancellation should be <= 5 (got before=%d, after=%d, delta=%d)",
		goroutinesBefore, goroutinesAfter, delta)
}

// TestStream_AccumulatedTextMatchesFinalResponse streams a response, accumulates
// all text chunks, and verifies the accumulated text matches the final
// ChunkDone.Message.Text().
func TestStream_AccumulatedTextMatchesFinalResponse(t *testing.T) {
	conv := openTestConv(t)
	ctx := context.Background()

	ch := conv.Stream(ctx, "Hello")

	var accumulated string
	var finalText string

	for chunk := range ch {
		require.NoError(t, chunk.Error, "unexpected error in stream chunk")

		if chunk.Type == sdk.ChunkText {
			accumulated += chunk.Text
		}
		if chunk.Type == sdk.ChunkDone && chunk.Message != nil {
			finalText = chunk.Message.Text()
		}
	}

	assert.NotEmpty(t, accumulated, "accumulated text should not be empty")
	assert.NotEmpty(t, finalText, "final response text should not be empty")
	assert.Equal(t, accumulated, finalText,
		"accumulated text chunks should match the final response text")
}
