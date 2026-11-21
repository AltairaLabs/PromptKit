package hooks

import (
	"context"
	"time"
)

// StreamHoldMode is how much of a streamed reply an output guardrail holds
// back from the caller until it has judged it (the guardrail's stream_hold
// param). Text the caller receives has passed every guardrail whose mode
// holds it.
type StreamHoldMode string

const (
	// StreamHoldNone holds nothing: text reaches the caller as it arrives, and
	// the guardrail judges the completed reply. When it fails that reply, its
	// message follows the streamed text, marked as replacing it.
	StreamHoldNone StreamHoldMode = "none"
	// StreamHoldTail holds the end of the reply that could be the start of
	// something the guardrail's chunk check blocks (Decision.Hold).
	StreamHoldTail StreamHoldMode = "tail"
	// StreamHoldSegment holds every byte not yet in a segment the guardrail
	// has passed (StreamHolder.JudgePartialReply). Segments are judged while
	// the model writes on; the last is judged with the completed reply.
	StreamHoldSegment StreamHoldMode = "segment"
	// StreamHoldTurn holds the whole reply until the guardrail passes the
	// completed reply.
	StreamHoldTurn StreamHoldMode = "turn"
)

// Decision.Metadata keys a firing on a partial reply carries, placing it in
// the streamed reply.
const (
	// MetadataKeySegmentIndex is the 1-based index of the last segment the
	// judged text covered.
	MetadataKeySegmentIndex = "segment_index"
	// MetadataKeySegmentEnd is the byte offset in the round's reply text
	// where the judged text ended.
	MetadataKeySegmentEnd = "segment_end"
)

// StreamHolder is implemented by an output provider hook that can hold a
// streamed reply back from the caller until it has judged it. The provider
// stage asks it how much it holds once per round. A StreamHoldTail or
// StreamHoldNone holder holds what its chunk check says (Decision.Hold), as a
// hook that does not implement this does.
type StreamHolder interface {
	// StreamHold is how much of a streamed reply the hook holds back.
	StreamHold() StreamHoldMode

	// JudgePartialReply judges reply.Text, the reply to req so far, while it
	// streams (StreamHoldSegment): the stage calls it as segments become
	// ready, at most once at a time per hook, concurrently with the model
	// writing the rest, and cancels ctx when the reply is cut or the round
	// ends. The earlier text is context: only what is new needs a verdict. A
	// judgment that errors or runs out of time fails closed. An Enforced
	// decision carries the text that replaces the reply in Content.
	JudgePartialReply(ctx context.Context, req *ProviderRequest, reply PartialReply) Decision
}

// PartialReply is a streamed reply's text so far, judged by a StreamHolder.
type PartialReply struct {
	// Text is the round's reply up to the end of the last segment judged.
	Text string
	// SegmentIndex is the 1-based index of that segment.
	SegmentIndex int
	// Timeout is the host's bound on the judgment; a hook may set its own.
	Timeout time.Duration
}

// StreamHolders returns the provider hooks that hold a streamed reply past
// the tail their chunk check holds (StreamHoldSegment, StreamHoldTurn), in
// registration order.
func (r *Registry) StreamHolders() []StreamHolder {
	if r == nil {
		return nil
	}
	var out []StreamHolder
	for _, h := range r.providerHooks {
		sh, ok := h.(StreamHolder)
		if !ok {
			continue
		}
		if m := sh.StreamHold(); m == StreamHoldSegment || m == StreamHoldTurn {
			out = append(out, sh)
		}
	}
	return out
}
