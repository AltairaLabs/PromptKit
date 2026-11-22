package stage

import (
	"context"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// heardSettleTimeout bounds how long the provider stage waits, before the
// turn after a barge-in, for the speech-out stage to report what the caller
// heard of the reply it cut. The speech-out stage reports it as it handles the
// barge-in's Interrupt, milliseconds after the provider stage passed it on; a
// variable so tests need not wait it out.
var heardSettleTimeout = time.Second

// HeardReplies carries what the caller heard of a cut reply from a cascaded
// voice pipeline's speech-out stage (TTSStageWithInterruption) back to its
// provider stage, so the model's next turn goes on from what was heard, as a
// realtime model goes on from the reply its provider truncated to it.
//
// The provider stage keeps the conversation's history itself and wrote the
// whole reply into it; the speech-out stage, downstream, is the one that knows
// how much of it played. Share one HeardReplies between the two
// (ProviderConfig.HeardReplies, TTSStageWithInterruptionConfig.HeardReplies):
// the speech-out stage records each reply it cut, and the provider stage
// swaps it into its history before the next turn. A nil *HeardReplies does
// nothing.
type HeardReplies struct {
	mu sync.Mutex
	// expected counts the Interrupts the provider stage passed on, settled
	// those the speech-out stage handled; a cut is recorded before its
	// Interrupt settles.
	expected, settled int
	changed           chan struct{}
	cuts              []heardCut
}

// heardCut is a reply the caller did not hear in full: its text as the
// provider stage wrote it, and the message as heard.
type heardCut struct {
	written string
	heard   types.Message
}

// NewHeardReplies returns an empty HeardReplies.
func NewHeardReplies() *HeardReplies {
	return &HeardReplies{changed: make(chan struct{})}
}

// record notes that the reply written as written was heard as heard.
func (h *HeardReplies) record(written string, heard *types.Message) {
	if h == nil || heard == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cuts = append(h.cuts, heardCut{written: written, heard: *heard})
}

// expect notes an Interrupt the provider stage passed on.
func (h *HeardReplies) expect() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.expected++
}

// settle notes an Interrupt the speech-out stage handled: what it cut before
// it is recorded.
func (h *HeardReplies) settle() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.settled++
	close(h.changed)
	h.changed = make(chan struct{})
}

// await waits until every Interrupt passed on has been handled downstream, at
// most heardSettleTimeout, or ctx ends.
func (h *HeardReplies) await(ctx context.Context) {
	if h == nil {
		return
	}
	timeout := time.NewTimer(heardSettleTimeout)
	defer timeout.Stop()
	for {
		h.mu.Lock()
		done, changed := h.settled >= h.expected, h.changed
		h.mu.Unlock()
		if done {
			return
		}
		select {
		case <-changed:
		case <-timeout.C:
			return
		case <-ctx.Done():
			return
		}
	}
}

// apply returns history with each reply recorded as cut replaced by what was
// heard of it, the latest match for each, on a copy (the messages history
// holds are shared with elements already handed on); history itself when
// nothing was cut.
func (h *HeardReplies) apply(history []types.Message) []types.Message {
	if h == nil {
		return history
	}
	h.mu.Lock()
	cuts := h.cuts
	h.cuts = nil
	h.mu.Unlock()
	if len(cuts) == 0 {
		return history
	}
	out := append([]types.Message(nil), history...)
	for c := range cuts {
		for i := len(out) - 1; i >= 0; i-- {
			if out[i].Role == core.RoleAssistant && out[i].Content == cuts[c].written {
				out[i] = cuts[c].heard
				break
			}
		}
	}
	return out
}
