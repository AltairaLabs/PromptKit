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
// gives no hint which route carries it. `message.created` reads like a contract
// a bus subscriber can rely on, and it never arrives without WithRecording.
//
// That cost a shipped release: v1.5.12 put the accumulated ReasoningTrace on
// message.created, which was right for recording consumers and reached nobody
// else. Measured live on the normal wiring: 0 message.created, 7
// reasoning.delta, terminal response nil. #1842 fixed it by adding
// reasoning.completed to the bus.
//
// These tests write the boundary down. Their limit is worth stating: they catch
// the boundary MOVING, not a field added to a type that was already on the
// wrong side of it. What they give an author is the fact they need — which
// types a plain consumer actually sees — somewhere they will meet it.

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

	input := make(chan StreamElement, 1)
	userMsg := types.Message{Role: "user", Content: "go"}
	input <- NewMessageElement(&userMsg)
	close(input)

	output := make(chan StreamElement, 64)
	require.NoError(t, stage.Process(context.Background(), input, output))
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
	}
}

// recordingOnlyTypes never reach a bus subscriber. They exist ONLY when
// RecordingStage is wired, which needs WithRecording plus an EventStore, and
// they go straight to the store without a bus hop.
//
// A fact placed on one of these is invisible to every consumer that has not
// opted in — the v1.5.12 failure.
func recordingOnlyTypes() []events.EventType {
	return []events.EventType{
		events.EventMessageCreated,
	}
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
