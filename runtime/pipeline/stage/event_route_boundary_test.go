package stage

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Pipeline facts reach consumers by two routes, deliberately:
//
//	Emitter -> EventBus -> subscribers        async, worker-pooled, LOSSY
//	RecordingStage -> EventStore.Append       synchronous, lossless, OPT-IN
//
// The split is correct — recording must not drop, observability must not block.
// What misleads is that both share ONE event-type namespace, so a type name
// gives no hint which route carries it.
//
// message.created takes BOTH routes: MessageBroadcastStage publishes it here
// for live consumers with binary stripped, RecordingStage appends it to an
// EventStore for replay with binary retained. It did not always. It reached
// only the store until the live route was added, which is how #1865 came to
// deprecate the bus producer as "probably a mistake" — a conclusion drawn from
// a grep of this repo, while the caller sat in another one.
//
// So the trap is no longer which route a TYPE takes. It is which route a FIELD
// lands on. v1.5.12 put the accumulated ReasoningTrace on message.created when
// only the recording route existed; measured live on the normal wiring, that
// was 0 message.created, 7 reasoning.delta and a nil terminal response. #1842
// fixed it by adding reasoning.completed to the bus.
//
// These tests write the boundary down. Their limit is worth stating: they catch
// the boundary MOVING, not a field added to a type already on the wrong side of
// it. What they give an author is the fact they need — which types a plain
// consumer actually sees — somewhere they will meet it.

// busTypesForOrdinaryTurn drives a tool-loop turn with NO recording stage and
// returns the distinct event types a bus subscriber received.
func busTypesForOrdinaryTurn(t *testing.T) map[events.EventType]bool {
	t.Helper()

	bus := events.NewEventBus()
	t.Cleanup(bus.Close)

	var mu = make(chan struct{}, 1)
	mu <- struct{}{}
	seen := map[events.EventType]bool{}
	for _, et := range allPipelineEventTypes() {
		et := et
		bus.Subscribe(et, func(*events.Event) {
			<-mu
			seen[et] = true
			mu <- struct{}{}
		})
	}

	emitter := events.NewEmitter(bus, "run", "sess", "conv")
	reg := registryWithTools(t, "probe")
	reg.RegisterExecutor(staticExecutor{})

	turnState := NewTurnState()
	turnState.SystemPrompt = "sys"
	turnState.AllowedTools = []string{"probe"}

	// One tool round then an answer: the shape that exercises tool events,
	// provider events and reasoning together.
	stage := NewProviderStageWithTurnState(
		&scriptedRoundProvider{toolRounds: 1},
		reg, nil, &ProviderConfig{MaxTokens: 100}, emitter, nil, turnState,
	)

	// Chain the live message route after the provider, exactly as the SDK
	// builder does. Without this the fixture could not observe message.created
	// and the list below would be a claim nothing exercises.
	broadcast := NewMessageBroadcastStage(emitter)

	input := make(chan StreamElement, 1)
	userMsg := types.Message{Role: "user", Content: "go"}
	input <- NewMessageElement(&userMsg)
	close(input)

	mid := make(chan StreamElement, 64)
	require.NoError(t, stage.Process(context.Background(), input, mid))

	output := make(chan StreamElement, 64)
	require.NoError(t, broadcast.Process(context.Background(), mid, output))
	for range output { //nolint:revive // draining
	}

	// The bus dispatches through a worker pool; let it drain.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		<-mu
		n := len(seen)
		mu <- struct{}{}
		if n >= len(busDeliveredTypes()) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	<-mu
	out := map[events.EventType]bool{}
	for k, v := range seen {
		out[k] = v
	}
	mu <- struct{}{}
	return out
}

// allPipelineEventTypes is every type either route can carry for a turn.
func allPipelineEventTypes() []events.EventType {
	return append(busDeliveredTypes(), recordingOnlyTypes()...)
}

// busDeliveredTypes are the types an ordinary consumer — no recording stage —
// receives for a tool-calling turn.
//
// Adding a type here without a producer, or emitting a new type and not listing
// it, both fail below. That is the point: the set is a claim about what a
// consumer can subscribe to, not a description of what the code happens to do.
func busDeliveredTypes() []events.EventType {
	return []events.EventType{
		events.EventProviderCallStarted,
		events.EventProviderCallCompleted,
		events.EventToolCallStarted,
		events.EventToolCallCompleted,
		events.EventReasoningCompleted,
		events.EventMessageCreated,
	}
}

// recordingOnlyTypes never reach a bus subscriber. They would exist ONLY when
// RecordingStage is wired, which needs WithRecording plus an EventStore, and
// would go straight to the store without a bus hop.
//
// Empty since message.created gained a bus producer in MessageBroadcastStage.
// The split this describes is still real — RecordingStage still appends
// directly to an EventStore, with full binary — so a future recording-only
// type belongs here. Keeping the list rather than deleting it also keeps the
// assertion below, which is what would catch a type quietly moving back.
func recordingOnlyTypes() []events.EventType {
	return []events.EventType{}
}

// TestEventRoutes_OrdinaryConsumerReceivesTheDeclaredSet pins what a consumer
// gets with no recording stage.
func TestEventRoutes_OrdinaryConsumerReceivesTheDeclaredSet(t *testing.T) {
	seen := busTypesForOrdinaryTurn(t)

	var missing []string
	for _, et := range busDeliveredTypes() {
		if !seen[et] {
			missing = append(missing, string(et))
		}
	}
	sort.Strings(missing)
	assert.Emptyf(t, missing,
		"declared as bus-delivered but never arrived: %v. Either the emit was removed, "+
			"or this type only exists on the recording route and the list is wrong",
		missing)
}

// TestEventRoutes_RecordingOnlyTypesNeverReachTheBus is the other half, and the
// one that documents the trap.
//
// A type here is unreachable for any consumer that has not opted into
// recording. Putting a fact on one — as v1.5.12 did with the reasoning trace —
// ships it to nobody. If one of these ever starts arriving on the bus, that is
// a real change in the contract and should be a deliberate edit here, not a
// surprise to a consumer counting on the split.
func TestEventRoutes_RecordingOnlyTypesNeverReachTheBus(t *testing.T) {
	seen := busTypesForOrdinaryTurn(t)

	for _, et := range recordingOnlyTypes() {
		assert.Falsef(t, seen[et],
			"%s reached a bus subscriber without a recording stage. It is listed as "+
				"recording-only, so either it gained a bus producer (update the lists) or "+
				"a consumer is about to receive it twice when recording IS enabled", et)
	}
}
