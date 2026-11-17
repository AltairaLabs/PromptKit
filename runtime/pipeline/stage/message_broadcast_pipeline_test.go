package stage

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// broadcastProbe subscribes to message.created and collects payloads.
type broadcastProbe struct {
	bus *events.EventBus
	mu  sync.Mutex
	got []*events.MessageCreatedData
}

// count returns how many payloads have arrived.
func (p *broadcastProbe) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.got)
}

// waitFor waits until want payloads have arrived, or two seconds pass, and
// reports whether they did.
func (p *broadcastProbe) waitFor(want int) bool {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && p.count() < want {
		time.Sleep(time.Millisecond)
	}
	return p.count() >= want
}

// broadcastSummary is what a test asserts of one message.created payload.
type broadcastSummary struct {
	Role, Content string
	Index         int
}

// summarize returns role, content and Index of each payload, in Index order.
func summarize(got []*events.MessageCreatedData) []broadcastSummary {
	out := make([]broadcastSummary, len(got))
	for i, d := range got {
		out[i] = broadcastSummary{Role: d.Role, Content: d.GetContent(), Index: d.Index}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out
}

// snapshot returns the payloads that have arrived so far.
func (p *broadcastProbe) snapshot() []*events.MessageCreatedData {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*events.MessageCreatedData(nil), p.got...)
}

// drain closes the bus, which delivers everything already published (the
// stages have returned, so that is everything), and returns the payloads by
// Index. An unexpected extra event is caught; the bus takes no more.
func (p *broadcastProbe) drain() []*events.MessageCreatedData {
	p.bus.Close()
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*events.MessageCreatedData, len(p.got))
	copy(out, p.got)
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out
}

func (p *broadcastProbe) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.got = nil
}

// newBroadcastProbe wires a probe onto a fresh bus and returns the emitter to
// drive stages with.
func newBroadcastProbe(t *testing.T) (*broadcastProbe, *events.Emitter) {
	t.Helper()
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)

	p := &broadcastProbe{bus: bus}
	bus.Subscribe(events.EventMessageCreated, func(e *events.Event) {
		if d, ok := e.Data.(*events.MessageCreatedData); ok {
			p.mu.Lock()
			p.got = append(p.got, d)
			p.mu.Unlock()
		}
	})
	return p, events.NewEmitter(bus, "run", "sess", "conv")
}

// runTurnThroughProvider drives history + a live user message through a real
// ProviderStage into the broadcast stage, the way the SDK builder orders them.
func runTurnThroughProvider(
	t *testing.T, bcast *MessageBroadcastStage, emitter *events.Emitter, history []types.Message, ask string,
) {
	t.Helper()

	reg := registryWithTools(t, "probe")
	reg.RegisterExecutor(staticExecutor{})
	turnState := NewTurnState()
	turnState.SystemPrompt = "sys"

	ps := NewProviderStageWithTurnState(
		&scriptedRoundProvider{toolRounds: 0},
		reg, nil, &ProviderConfig{MaxTokens: 100}, emitter, nil, turnState,
	)

	in := make(chan StreamElement, len(history)+2)
	for i := range history {
		elem := NewMessageElement(&history[i])
		elem.Meta.FromHistory = true
		in <- elem
	}
	userMsg := realMessage("user", ask)
	in <- NewMessageElement(&userMsg)
	close(in)

	mid := make(chan StreamElement, 64)
	require.NoError(t, ps.Process(context.Background(), in, mid))

	out := make(chan StreamElement, 64)
	require.NoError(t, bcast.Process(context.Background(), mid, out))
	for range out { //nolint:revive // draining
	}
}

// persistedHistory mirrors what StateStoreLoadStage replays: messages stamped
// with Source "statestore".
func persistedHistory(pairs ...string) []types.Message {
	msgs := make([]types.Message, 0, len(pairs))
	for i, c := range pairs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		m := realMessage(role, c)
		m.Source = "statestore"
		msgs = append(msgs, m)
	}
	return msgs
}

// TestMessageBroadcast_DoesNotRepublishHistoryThroughProvider is the real-
// pipeline version of the history test.
//
// The unit test passes history elements straight to the broadcast stage, where
// Meta.FromHistory survives. In the real pipeline the broadcast stage sits
// DOWNSTREAM of ProviderStage, which rebuilds every message with
// NewMessageElement (stages_provider.go:561) — producing a zero Meta — and
// emits the whole accumulated transcript, not just the new reply. So the
// FromHistory skip never fires and every turn re-broadcasts the conversation.
func TestMessageBroadcast_DoesNotRepublishHistoryThroughProvider(t *testing.T) {
	t.Parallel()
	probe, emitter := newBroadcastProbe(t)
	bcast := NewMessageBroadcastStage(emitter)

	runTurnThroughProvider(t, bcast, emitter, persistedHistory("old q", "old a"), "new q")

	// Only the new turn is broadcast, at its transcript positions; the
	// replayed history ("old q", "old a") is not.
	assert.Equal(t, []broadcastSummary{
		{Role: "user", Content: "new q", Index: 2},
		{Role: "assistant", Content: "done", Index: 3},
	}, summarize(probe.drain()))
}

// TestMessageBroadcast_IndexIsAbsoluteAcrossTurns pins that Index keeps meaning
// something on turn 2.
//
// The SDK builds the pipeline ONCE at Open (sdk/sdk.go:687) and every Send
// re-executes the same stage objects, so a counter held on the stage never
// resets — while the load stage replays the transcript each turn. Turn 2's
// messages must land at their real transcript positions, not continue from
// turn 1's high-water mark.
func TestMessageBroadcast_IndexIsAbsoluteAcrossTurns(t *testing.T) {
	t.Parallel()
	probe, emitter := newBroadcastProbe(t)
	bcast := NewMessageBroadcastStage(emitter)

	// Turn 1: empty transcript.
	runTurnThroughProvider(t, bcast, emitter, nil, "first")
	require.True(t, probe.waitFor(2), "turn 1 broadcast the user message and the reply")
	require.Equal(t, []broadcastSummary{
		{Role: "user", Content: "first", Index: 0},
		{Role: "assistant", Content: "done", Index: 1},
	}, summarize(probe.snapshot()))
	probe.reset()

	// Turn 2: turn 1 is now persisted history, so the new user message sits at
	// transcript position 2. Turn 1 broadcast exactly its two messages, so a
	// late delivery of a third would show here as an extra payload.
	runTurnThroughProvider(t, bcast, emitter, persistedHistory("first", "done"), "second")

	assert.Equal(t, []broadcastSummary{
		{Role: "user", Content: "second", Index: 2},
		{Role: "assistant", Content: "done", Index: 3},
	}, summarize(probe.drain()), "Index must be transcript-absolute on turn 2, not continue from turn 1")
}
