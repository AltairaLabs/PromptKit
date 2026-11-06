package providers

import (
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/safe"
)

// streamTap is the last step of the RunStreamingRequest relay: every chunk the
// caller receives passes through send. It gives the final chunk of a clean
// stream its FinalResult and reports each chunk to the provider's
// StreamObserver. It is used from the single relay goroutine only.
type streamTap struct {
	out      chan<- StreamChunk
	observer StreamObserver
	start    time.Time

	tokens int          // last TokenCount reported
	last   *StreamChunk // last chunk delivered
	failed bool         // the current attempt ended on an error chunk
}

// send completes chunk, reports it to the observer and delivers it.
func (t *streamTap) send(chunk StreamChunk) {
	if chunk.Reset {
		t.failed = false // a retry starts; its own outcome decides
	}
	if chunk.Error != nil {
		t.failed = true
	} else if chunk.FinishReason != nil && chunk.FinalResult == nil {
		chunk.FinalResult = finalPredictionResponse(&chunk, time.Since(t.start))
	}
	if chunk.TokenCount > 0 {
		t.tokens = chunk.TokenCount
	}
	t.last = &chunk
	t.notifyChunk(chunk)
	t.out <- chunk
}

// finish reports the end of a stream that did not end on an error.
func (t *streamTap) finish() {
	if t.observer == nil || t.failed {
		return
	}
	t.notify(StreamEvent{Type: StreamEventComplete, Chunk: t.last}, func() {
		t.observer.OnComplete(t.tokens, time.Since(t.start))
	})
}

func (t *streamTap) notifyChunk(chunk StreamChunk) {
	if t.observer == nil {
		return
	}
	if chunk.Error != nil {
		t.notify(StreamEvent{Type: StreamEventError, Chunk: &chunk, Error: chunk.Error}, func() {
			t.observer.OnError(chunk.Error)
		})
		return
	}
	t.notify(StreamEvent{Type: StreamEventChunk, Chunk: &chunk}, func() {
		t.observer.OnChunk(chunk)
	})
}

// notify delivers ev to a StreamEventObserver, then runs callback. A panic in
// either is recovered so an observer bug cannot end the stream.
func (t *streamTap) notify(ev StreamEvent, callback func()) {
	if eo, ok := t.observer.(StreamEventObserver); ok {
		ev.Timestamp = time.Now()
		safe.Run("stream-event-observer", func() { eo.OnStreamEvent(ev) }, nil)
	}
	safe.Run("stream-observer", callback, nil)
}

// finalPredictionResponse builds the FinalResult of a stream's final chunk.
func finalPredictionResponse(chunk *StreamChunk, latency time.Duration) *PredictionResponse {
	return &PredictionResponse{
		Content:      chunk.Content,
		CostInfo:     chunk.CostInfo,
		Latency:      latency,
		ToolCalls:    chunk.ToolCalls,
		FinishReason: *chunk.FinishReason,
	}
}
