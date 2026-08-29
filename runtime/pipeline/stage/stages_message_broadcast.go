package stage

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/events"
)

// MessageBroadcastStage publishes message.created on the EventBus for each new
// complete message that streams through it, and forwards every element
// unchanged.
//
// This is the LIVE route for messages: async, lossy, binary stripped. It is
// what a consumer watching a conversation unfold wants — a TUI, an SSE relay, a
// log tail. It needs no EventStore and no state store, so it works on any
// pipeline that has an emitter.
//
// RecordingStage is the FIDELITY route for the same event type: synchronous,
// lossless, full binary, opt-in, straight to an EventStore. Both build their
// payload with events.NewMessageCreatedData, so they carry the same data for
// the same message except Parts.
//
// Because the bus is lossy under burst, a live view can miss a message. That is
// the right trade for observability and the wrong one for a transcript: the
// state store remains the source of truth.
//
// Index is transcript-absolute. Replayed history (Meta.FromHistory) is counted
// for position but never re-published, so a subscriber sees each message once,
// at the position the persisted transcript will hold it.
//
// Read Index rather than arrival order. The bus dispatches through a worker
// pool, so subscribers can and do receive these out of publish order — Index is
// what lets a consumer reassemble a transcript from a stream that makes no
// ordering promise.
type MessageBroadcastStage struct {
	BaseStage
	emitter *events.Emitter
	// msgIndex is the transcript-absolute position of the next message.
	msgIndex int
}

// NewMessageBroadcastStage creates a message broadcast stage. A nil emitter
// makes the stage an inert pass-through.
func NewMessageBroadcastStage(emitter *events.Emitter) *MessageBroadcastStage {
	return &MessageBroadcastStage{
		BaseStage: NewBaseStage("message_broadcast", StageTypeTransform),
		emitter:   emitter,
	}
}

// Process publishes each new complete message and forwards all elements.
func (s *MessageBroadcastStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	for elem := range input {
		s.broadcast(ctx, &elem)

		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// broadcast publishes one element if it carries a new complete message.
//
// The index advances for history too, which is what keeps it aligned with the
// persisted transcript rather than counting only this turn's messages.
func (s *MessageBroadcastStage) broadcast(ctx context.Context, elem *StreamElement) {
	if elem.Message == nil || elem.EndOfStream {
		return
	}

	idx := s.msgIndex
	s.msgIndex++

	if s.emitter == nil || elem.Meta.FromHistory {
		return
	}

	s.emitter.MessageCreatedCtx(ctx, events.NewMessageCreatedData(elem.Message, idx, true))
}
