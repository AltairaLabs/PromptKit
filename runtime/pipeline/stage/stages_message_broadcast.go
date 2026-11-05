package stage

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
)

// MessageBroadcastStage publishes message.created on the EventBus for each new
// complete message that streams through it, and forwards every element
// unchanged.
//
// This is the live route for messages: asynchronous, lossy, binary stripped. It
// suits a consumer watching a conversation unfold, such as a TUI, an SSE relay or
// a log tail. It needs no EventStore and no state store, so it works on any
// pipeline that has an emitter.
//
// RecordingStage is the fidelity route for the same event type: synchronous,
// lossless, full binary, opt-in, straight to an EventStore. Both build their
// payload with events.NewMessageCreatedData, so they carry the same data for
// the same message except Parts.
//
// No binary data reaches the bus, whether or not recording is on. A subscriber
// gets the message text, along with MIME type, dimensions, size and URL
// references for any media, and never the media itself.
//
// The bus is lossy under burst, so a live view can miss a message. The state
// store remains the source of truth for the transcript.
//
// Index is transcript-absolute, and history is never re-published, so a
// subscriber sees each message once at the position the persisted transcript
// will hold it. Two details make that work:
//
//   - The counter is local to each Process call, never a field on the stage. A
//     pipeline is built once and re-executed per turn, and stage objects are
//     shared across concurrent Execute calls. Counting per execution is correct
//     because the provider re-emits the whole accumulated transcript on every
//     turn.
//
//   - History is detected by Message.Source, not by Meta.FromHistory.
//     ProviderStage rebuilds every message with NewMessageElement, which
//     produces a zero Meta, so element metadata does not survive to any stage
//     downstream of the provider. Source travels with the message value.
//     IncrementalSaveStage uses the same predicate for the same question.
//
// Placement: the stage must sit where it observes every message element, in
// transcript order, within a single Process call. The pipeline is a DAG
// (PipelineBuilder offers Branch, Merge and Connect, and RouterStage does
// selective fan-out), and nothing enforces this.
//
//   - Downstream of every message producer. Assistant messages are created by
//     ProviderStage, by CompositionStage (which replaces ProviderStage for
//     composition states), and by the media-compose and video-frame stages. A
//     message routed down a branch this stage is not on is never published.
//
//   - On an order-preserving path. Index is the element's position in the
//     stream this call saw. It is transcript-absolute because the provider
//     re-emits the accumulated transcript in order down a linear chain.
//     MergeStage spawns a goroutine per input, so downstream of a fan-in the
//     interleaving, and therefore Index, is nondeterministic. Completeness
//     survives a merge; ordering does not.
//
// The stage does not need to be adjacent to the save stage. The SDK builder
// places it immediately before that sink so a message broadcasts as soon as it
// exists.
//
// The bus delivers to each subscriber in publish order. Index is still the
// reliable way to reassemble a transcript: it stays correct when a subscriber's
// queue overflows and drops an event, or when the stage sits downstream of a
// fan-in that interleaves elements.
type MessageBroadcastStage struct {
	BaseStage
	emitter *events.Emitter
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

	// Local to this execution — see the type doc. The provider re-emits the
	// whole accumulated transcript each turn, so position within one Process
	// call IS the transcript-absolute index.
	msgIndex := 0

	// A canceled turn's partial reply and error arrive after the
	// cancellation; see cancelForwarder.
	var fwd cancelForwarder
	for elem := range input {
		s.broadcast(liveContext(ctx), &elem, &msgIndex)
		fwd.forward(ctx, output, elem)
	}

	return ctx.Err()
}

// broadcast publishes one element if it carries a new complete message.
//
// The index advances for history too, which is what keeps it aligned with the
// persisted transcript rather than counting only this turn's new messages.
func (s *MessageBroadcastStage) broadcast(ctx context.Context, elem *StreamElement, msgIndex *int) {
	if elem.Message == nil || elem.EndOfStream {
		return
	}

	idx := *msgIndex
	*msgIndex++

	if s.emitter == nil || !isNewMessage(elem.Message) {
		return
	}

	s.emitter.MessageCreatedCtx(ctx, events.NewMessageCreatedData(elem.Message, idx, true))
}
