package stage

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
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
// INVARIANT: no BINARY ever reaches the bus. Not "usually", and not "only when
// recording is off" — always. A megabyte of base64 per turn would swamp a
// channel whose whole job is to stay out of the pipeline's way, and handling
// binary is exactly what the opt-in recording route exists for.
//
// This is about bytes, not about content. A subscriber gets the message TEXT —
// that is the point of a live route — along with MIME type, dimensions, size
// and URL references for any media. What it never gets is the media itself.
// Held by TestMessageBroadcastStage_NeverPutsBinaryOnTheBus and its
// recording-side sibling, alongside the audio guard for #853.
//
// Because the bus is lossy under burst, a live view can miss a message. That is
// the right trade for observability and the wrong one for a transcript: the
// state store remains the source of truth.
//
// Index is transcript-absolute, and history is never re-published, so a
// subscriber sees each message once at the position the persisted transcript
// will hold it.
//
// Two things make that work, and both are load-bearing:
//
//   - The counter is LOCAL to each Process call, never a field on the stage.
//     A pipeline is built once and re-executed per turn (sdk/sdk.go:687), so a
//     counter on the stage would keep climbing across turns and would also be a
//     data race — pipeline.go documents that stage objects are shared across
//     concurrent Execute calls. Counting per execution is correct because the
//     provider re-emits the whole accumulated transcript on every turn.
//
//   - History is detected by Message.Source, NOT by Meta.FromHistory.
//     ProviderStage rebuilds every message with NewMessageElement
//     (stages_provider.go:561), which produces a zero Meta, so element metadata
//     does not survive to any stage downstream of the provider. Source travels
//     with the message value and does. isNewMessage is the same predicate
//     IncrementalSaveStage uses for the same question.
//
// PLACEMENT PRECONDITION. This stage must sit where it observes EVERY message
// element, in transcript order, within a single Process call. The pipeline is a
// DAG — PipelineBuilder offers Branch, Merge and Connect, and RouterStage does
// selective fan-out — so that is a real constraint, not a formality, and
// nothing enforces it:
//
//   - Downstream of every message producer. Assistant messages are created by
//     ProviderStage, by CompositionStage (which REPLACES ProviderStage for
//     composition states), and by the media-compose and video-frame stages. A
//     message routed down a branch this stage is not on is silently never
//     published.
//
//   - On an order-preserving path. Index is the element's position in the
//     stream this call saw. It is transcript-absolute only because the provider
//     re-emits the accumulated transcript in order down a linear chain.
//     MergeStage spawns a goroutine per input, so downstream of a fan-in the
//     interleaving — and therefore Index — is nondeterministic. Completeness
//     survives a merge; ordering does not. Both are pinned by tests.
//
// It does NOT need to be adjacent to the save stage. The SDK builder places it
// immediately before that sink only so a message broadcasts as soon as it
// exists; correctness does not depend on it.
//
// Read Index rather than arrival order. The bus dispatches through a worker
// pool, so subscribers can and do receive these out of publish order — Index is
// what lets a consumer reassemble a transcript from a stream that makes no
// ordering promise.
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

	for elem := range input {
		s.broadcast(ctx, &elem, &msgIndex)

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
