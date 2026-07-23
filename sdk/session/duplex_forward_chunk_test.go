package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newForwardTestSession builds a bare duplexSession with a small output buffer,
// enough to exercise forwardChunk in isolation.
func newForwardTestSession(bufferSize int) *duplexSession {
	return &duplexSession{
		streamOutput: make(chan providers.StreamChunk, bufferSize),
	}
}

// TestForwardChunk_UnreadNeverBlocks is #1638: a caller that never takes
// Response() must not stall the pipeline. When nobody is reading, forwardChunk
// buffers while there is room and then drops — it never blocks — so the
// forwarding loop keeps running and the session stays alive.
func TestForwardChunk_UnreadNeverBlocks(t *testing.T) {
	s := newForwardTestSession(2)
	// responseTaken defaults to false — Response() was never called.

	ctx := context.Background()
	for i := range 5 {
		keepGoing := s.forwardChunk(ctx, providers.StreamChunk{Delta: "x"})
		assert.True(t, keepGoing, "unread forwarding must keep the loop running, never block (chunk %d)", i)
	}

	assert.Equal(t, int64(3), s.droppedChunks.Load(),
		"with buffer 2 and 5 sends and no reader, the 3 overflow chunks must be dropped, not blocked on")
}

// TestForwardChunk_TakenDeliversToReader is the regression guard: once Response()
// is taken, chunks are delivered to the consumer rather than dropped.
func TestForwardChunk_TakenDeliversToReader(t *testing.T) {
	s := newForwardTestSession(4)
	s.responseTaken.Store(true)

	ctx := context.Background()
	for range 3 {
		assert.True(t, s.forwardChunk(ctx, providers.StreamChunk{Delta: "y"}))
	}
	close(s.streamOutput)

	var got int
	for range s.streamOutput {
		got++
	}
	assert.Equal(t, 3, got, "a taken consumer must receive every chunk")
	assert.Equal(t, int64(0), s.droppedChunks.Load(), "nothing is dropped when Response() is taken")
}

// TestForwardChunk_TakenHonorsContextCancel proves a taken-but-stalled consumer
// does not wedge the loop forever: a full buffer plus a cancelled context makes
// forwardChunk return false so the forwarding loop can unwind.
func TestForwardChunk_TakenHonorsContextCancel(t *testing.T) {
	s := newForwardTestSession(1)
	s.responseTaken.Store(true)

	ctx, cancel := context.WithCancel(context.Background())
	assert.True(t, s.forwardChunk(ctx, providers.StreamChunk{Delta: "fills-buffer"}))

	cancel() // consumer never reads; buffer stays full
	keepGoing := s.forwardChunk(ctx, providers.StreamChunk{Delta: "would-block"})
	assert.False(t, keepGoing, "a cancelled context must release a blocked send so the loop can stop")
}

// TestForwardChunk_TakenSlowConsumerWarnsThenDelivers covers the taken-but-behind
// path: with the buffer full and the context alive, forwardChunk waits, warns
// once past the (test-lowered) threshold, and then delivers the moment the
// consumer drains — never losing the chunk.
func TestForwardChunk_TakenSlowConsumerWarnsThenDelivers(t *testing.T) {
	prev := outputBlockWarnThreshold
	outputBlockWarnThreshold = 10 * time.Millisecond
	defer func() { outputBlockWarnThreshold = prev }()

	s := newForwardTestSession(1)
	s.responseTaken.Store(true)
	s.streamOutput <- providers.StreamChunk{Delta: "fills-buffer"} // buffer now full

	ctx := context.Background()
	done := make(chan bool, 1)
	go func() { done <- s.forwardChunk(ctx, providers.StreamChunk{Delta: "waits-then-delivers"}) }()

	// Let it pass the warn threshold while blocked, then drain so the send lands.
	time.Sleep(40 * time.Millisecond)
	<-s.streamOutput // free a slot

	select {
	case keepGoing := <-done:
		assert.True(t, keepGoing, "a slow consumer that eventually drains must still receive the chunk")
	case <-time.After(2 * time.Second):
		t.Fatal("forwardChunk did not deliver after the buffer drained")
	}
	assert.True(t, s.warnedSlowConsumer.Load(), "a consumer stalled past the threshold must be warned")
	assert.Equal(t, int64(0), s.droppedChunks.Load(), "a taken consumer never drops")

	got := <-s.streamOutput
	assert.Equal(t, "waits-then-delivers", got.Delta)
}

// TestHandleToolCalls_UnreadFullBufferDoesNotBlock guards the wiring: the
// forwarding path in handleToolCalls must go through forwardChunk, so a session
// whose Response() is never read and whose output buffer is already full drops
// the chunk and returns rather than blocking the pipeline. Without the fix this
// call blocks forever (test would hang).
func TestHandleToolCalls_UnreadFullBufferDoesNotBlock(t *testing.T) {
	s := &duplexSession{
		id:           "test",
		toolRegistry: nil, // no registry → forwards the element via forwardChunk
		stageInput:   make(chan stage.StreamElement, 1),
		streamOutput: make(chan providers.StreamChunk, 1),
	}
	// Response() was never taken, and the buffer is already full.
	s.streamOutput <- providers.StreamChunk{Delta: "already-buffered"}

	elem := &stage.StreamElement{
		Message: &types.Message{
			ToolCalls: []types.MessageToolCall{{ID: "c1", Name: "t", Args: json.RawMessage(`{}`)}},
		},
	}

	done := make(chan error, 1)
	go func() { done <- s.handleToolCalls(context.Background(), elem) }()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("handleToolCalls blocked on a full, unread output channel — the stall is not fixed")
	}
	assert.Equal(t, int64(1), s.droppedChunks.Load(), "the un-deliverable chunk must be dropped, not blocked on")
}
