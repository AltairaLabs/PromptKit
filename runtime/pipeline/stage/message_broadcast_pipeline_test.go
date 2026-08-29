package stage

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// broadcastProbe subscribes to message.created and collects payloads.
type broadcastProbe struct {
	mu  sync.Mutex
	got []*events.MessageCreatedData
}

func (p *broadcastProbe) drain(want int) []*events.MessageCreatedData {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		n := len(p.got)
		p.mu.Unlock()
		if n >= want {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond)
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

	p := &broadcastProbe{}
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
	userMsg := types.Message{Role: "user", Content: ask}
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
		msgs = append(msgs, types.Message{Role: role, Content: c, Source: "statestore"})
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
	probe, emitter := newBroadcastProbe(t)
	bcast := NewMessageBroadcastStage(emitter)

	runTurnThroughProvider(t, bcast, emitter, persistedHistory("old q", "old a"), "new q")

	got := probe.drain(2)
	for _, d := range got {
		assert.NotEqualf(t, "old q", d.Content, "replayed history was re-broadcast")
		assert.NotEqualf(t, "old a", d.Content, "replayed history was re-broadcast")
	}
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
	probe, emitter := newBroadcastProbe(t)
	bcast := NewMessageBroadcastStage(emitter)

	// Turn 1: empty transcript.
	runTurnThroughProvider(t, bcast, emitter, nil, "first")
	probe.drain(1)
	probe.reset()

	// Turn 2: turn 1 is now persisted history, so the new user message sits at
	// transcript position 2.
	runTurnThroughProvider(t, bcast, emitter, persistedHistory("first", "answer"), "second")

	got := probe.drain(1)
	require.NotEmpty(t, got)

	var user *events.MessageCreatedData
	for _, d := range got {
		if d.Content == "second" {
			user = d
		}
	}
	require.NotNil(t, user, "turn 2's user message was not broadcast")
	assert.Equal(t, 2, user.Index,
		"Index must be transcript-absolute on turn 2, not continue from turn 1")
}
