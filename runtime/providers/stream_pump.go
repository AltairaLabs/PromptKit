package providers

import (
	"context"
	"sync/atomic"
)

// StreamPump is the shared core behind every streaming provider's barge-in
// behavior. It decouples a session's single-threaded receive loop from the
// (real-time-paced) consumer of Response(), and implements the barge-in audio
// drop — so a new provider gets working, consistent barge-in by wiring its
// wire-protocol signals, not by reimplementing the concurrency.
//
// The session owns the input channel (so it controls when no more chunks are
// coming, by closing it); the pump owns the output channel and an unbounded
// internal queue between them. Because the queue is unbounded, a slow consumer
// back-pressures only Response() and the queue — never the receive loop — so
// control events (barge-in) are handled promptly instead of waiting for the
// buffered audio backlog to drain.
//
// Lifecycle: NewStreamPump(ctx, in, buf) then Start(); the receive goroutine
// feeds the input channel and, on exit, closes it and calls Wait() (which lets
// the pump drain and close Response()) before canceling ctx — so a terminal
// chunk is delivered before Done() fires. On a detected barge-in the session
// calls Barge() (fires the out-of-band BargeIn() signal AND drops queued audio)
// and skips still-arriving audio while Dropping() is true, clearing it at the
// next response boundary with ClearDrop().
type StreamPump struct {
	// BargeInSignal provides the StreamInputSession.BargeIn() channel, fired by
	// Barge(). Embedded so it (and BargeIn()) are promoted to the session.
	BargeInSignal

	ctx      context.Context //nolint:containedctx // session-scoped; bounds the pump's blocking sends
	in       <-chan StreamChunk
	respCh   chan StreamChunk
	dropCh   chan struct{}
	pumpDone chan struct{}
	dropping atomic.Bool
}

// NewStreamPump creates a pump reading from in (owned and closed by the caller)
// and exposing a Response() channel buffered to buf. Call Start to run it.
func NewStreamPump(ctx context.Context, in <-chan StreamChunk, buf int) *StreamPump {
	return &StreamPump{
		BargeInSignal: NewBargeInSignal(),
		ctx:           ctx,
		in:            in,
		respCh:        make(chan StreamChunk, buf),
		dropCh:        make(chan struct{}, 1),
		pumpDone:      make(chan struct{}),
	}
}

// Start launches the pump goroutine. Call exactly once.
func (p *StreamPump) Start() { go p.run() }

// Response returns the consumer-facing channel; the pump closes it after the
// input channel closes and the queue drains (or the context is canceled).
func (p *StreamPump) Response() <-chan StreamChunk { return p.respCh }

// Wait blocks until the pump has finished draining and closed Response(). Call
// it from the receive loop's exit, after closing the input channel and before
// canceling the context, so any terminal chunk is delivered first.
func (p *StreamPump) Wait() { <-p.pumpDone }

// Dropping reports whether the interrupted response's audio should be skipped.
func (p *StreamPump) Dropping() bool { return p.dropping.Load() }

// ClearDrop stops skipping audio — call when a new response begins or the
// interrupted response completes.
func (p *StreamPump) ClearDrop() { p.dropping.Store(false) }

// Barge handles a detected barge-in: fire the out-of-band signal so a paced
// consumer flushes immediately, start skipping still-arriving audio, and drop
// the audio already queued for the interrupted response. Non-blocking; safe from
// the receive goroutine.
func (p *StreamPump) Barge() {
	p.dropping.Store(true)
	p.SignalBargeIn()
	select {
	case p.dropCh <- struct{}{}:
	default:
	}
}

func (p *StreamPump) run() {
	defer close(p.pumpDone)
	defer close(p.respCh)

	var queue []StreamChunk
	in := p.in // set to nil once closed, disabling its select case
	for in != nil || len(queue) > 0 {
		// out is nil (its send case disabled) while the queue is empty, so the
		// loop waits on `in`; once there's a head chunk, out carries it.
		var out chan<- StreamChunk
		var head StreamChunk
		if len(queue) > 0 {
			out, head = p.respCh, queue[0]
		}
		select {
		case chunk, ok := <-in:
			if !ok {
				in = nil // producer (receive loop) done; drain remaining queue
			} else {
				queue = append(queue, chunk)
			}
		case out <- head:
			queue[0] = StreamChunk{} // release for GC
			queue = queue[1:]
		case <-p.dropCh:
			// Barge-in: discard queued audio so the interrupted response stops
			// playing. head (if any) was a copy and is simply not sent.
			queue = dropAudioChunks(queue)
		case <-p.ctx.Done():
			return
		}
	}
}

// dropAudioChunks removes audio chunks from q in place, keeping text/transcript/
// control chunks (so an interrupted turn's partial transcript still flows). It
// reuses q's backing array and zeroes the freed tail for GC.
func dropAudioChunks(q []StreamChunk) []StreamChunk {
	kept := q[:0]
	for i := range q {
		if q[i].MediaData != nil && len(q[i].MediaData.Data) > 0 {
			continue
		}
		kept = append(kept, q[i])
	}
	for i := len(kept); i < len(q); i++ {
		q[i] = StreamChunk{}
	}
	return kept
}
