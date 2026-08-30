package stage

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// scriptedRoundProvider answers with a tool call on every round until the
// scripted number of tool-calling rounds is exhausted, then answers with text.
// This is the shape that makes round attribution observable: the same tool,
// called with the same arguments, once per round.
type scriptedRoundProvider struct {
	providers.Provider
	toolRounds int

	mu    sync.Mutex
	calls int
}

func (p *scriptedRoundProvider) ID() string              { return "scripted" }
func (p *scriptedRoundProvider) Name() string            { return "scripted" }
func (p *scriptedRoundProvider) Model() string           { return "scripted-model" }
func (p *scriptedRoundProvider) Type() base.ProviderType { return base.ProviderTypeInference }
func (p *scriptedRoundProvider) Close() error            { return nil }

// SupportsStreaming reports false so the stage takes the unary round path.
func (p *scriptedRoundProvider) SupportsStreaming() bool { return false }

func (p *scriptedRoundProvider) Predict(
	_ context.Context, _ providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	return providers.PredictionResponse{Content: "done"}, nil
}

// BuildTooling passes the descriptors through; the stub ignores their shape.
func (p *scriptedRoundProvider) BuildTooling(
	d []*providers.ToolDescriptor,
) (providers.ProviderTools, error) {
	return d, nil
}

func (p *scriptedRoundProvider) PredictStreamWithTools(
	_ context.Context, _ providers.PredictionRequest,
	_ providers.ProviderTools, _ string,
) (<-chan providers.StreamChunk, error) {
	ch := make(chan providers.StreamChunk)
	close(ch)
	return ch, nil
}

func (p *scriptedRoundProvider) PredictWithTools(
	_ context.Context, _ providers.PredictionRequest,
	_ providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.mu.Lock()
	p.calls++
	n := p.calls
	p.mu.Unlock()

	if n > p.toolRounds {
		return providers.PredictionResponse{
			Content:   "done",
			Reasoning: &types.ReasoningTrace{Text: fmt.Sprintf("round %d: I have everything; answer.", n)},
		}, nil, nil
	}
	// The SAME tool called again with corrected arguments — the motivating
	// case from #1840. Timestamp order puts these in sequence but cannot show
	// they came from two different model decisions; the round and the
	// provider-call ID can. Arguments differ per round because identical ones
	// trip the stage's repeated-call breaker.
	return providers.PredictionResponse{
			Reasoning: &types.ReasoningTrace{Text: fmt.Sprintf("round %d: I still need data; calling probe.", n)},
		}, []types.MessageToolCall{{
			ID:   "call_" + string(rune('A'+n-1)),
			Name: "probe",
			Args: json.RawMessage(fmt.Sprintf(`{"q":"attempt-%d"}`, n)),
		}}, nil
}

// staticExecutor answers any tool call with a fixed result.
type staticExecutor struct{}

func (staticExecutor) Name() string { return "local" }
func (staticExecutor) Execute(
	_ context.Context, d *tools.ToolDescriptor, _ json.RawMessage,
) (json.RawMessage, error) {
	_ = d
	return json.RawMessage(`{"ok":true}`), nil
}

// collectEvents subscribes to the given types and returns a snapshot function.
func collectEvents(bus events.Bus, types ...events.EventType) func() []*events.Event {
	var mu sync.Mutex
	var got []*events.Event
	for _, t := range types {
		bus.Subscribe(t, func(e *events.Event) {
			mu.Lock()
			got = append(got, e)
			mu.Unlock()
		})
	}
	return func() []*events.Event {
		mu.Lock()
		defer mu.Unlock()
		out := make([]*events.Event, len(got))
		copy(out, got)
		return out
	}
}

// runScriptedLoop drives a provider stage through toolRounds tool-calling
// rounds plus a final text round, returning a snapshot of the captured events.
func runScriptedLoop(t *testing.T, toolRounds, wantEvents int, evtTypes ...events.EventType) []*events.Event {
	t.Helper()

	bus := events.NewEventBus()
	emitter := events.NewEmitter(bus, "run", "sess", "conv")
	snapshot := collectEvents(bus, evtTypes...)

	reg := registryWithTools(t, "probe")
	reg.RegisterExecutor(staticExecutor{})

	turnState := NewTurnState()
	turnState.SystemPrompt = "sys"
	turnState.AllowedTools = []string{"probe"}
	stage := NewProviderStageWithTurnState(
		&scriptedRoundProvider{toolRounds: toolRounds},
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

	// The bus dispatches through a worker pool, so events arrive asynchronously
	// AND out of publish order. Wait for the expected count; never assume the
	// order they land in.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(snapshot()) >= wantEvents {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	got := snapshot()
	require.GreaterOrEqualf(t, len(got), wantEvents,
		"timed out waiting for %d events, got %d", wantEvents, len(got))
	return got
}

// TestToolCallEvents_CarryRoundAndProviderCallID is the core assertion for
// #1840: a tool call is made BY a turn, and the event says which.
//
// Deriving the round by counting provider.call.completed events fails
// silently — a round's tool calls are dispatched BEFORE that round's provider
// call completes, so the count reads 0 on round 1 and lags by one after. That
// arithmetic passes unit tests and stamps zero in production, which is the
// argument for emitting the real value.
func TestToolCallEvents_CarryRoundAndProviderCallID(t *testing.T) {
	const toolRounds = 3
	got := runScriptedLoop(t, toolRounds, toolRounds, events.EventToolCallStarted)
	require.Len(t, got, toolRounds, "expected one tool.call.started per round")

	seenRounds := make([]int, 0, toolRounds)
	seenCallIDs := make(map[string]bool)

	for _, e := range got {
		data, ok := e.Data.(*events.ToolCallEventData)
		require.True(t, ok, "unexpected payload type %T", e.Data)

		assert.NotZerof(t, data.Round,
			"tool call %q stamped round 0 — the silent-zero failure mode", data.CallID)
		assert.NotEmptyf(t, data.ProviderCallID,
			"tool call %q carries no provider call ID", data.CallID)

		seenRounds = append(seenRounds, data.Round)
		seenCallIDs[data.ProviderCallID] = true
	}

	// Sort before comparing: the bus dispatches through a worker pool, so
	// arrival order is not publish order. What matters is the SET of rounds.
	sort.Ints(seenRounds)
	assert.Equal(t, []int{1, 2, 3}, seenRounds, "rounds must be 1-based and cover every round")

	// The whole point: same tool, same arguments, different model decisions.
	// Each round must carry a DISTINCT provider call ID.
	assert.Lenf(t, seenCallIDs, toolRounds,
		"expected %d distinct provider call IDs, got %d — rounds are not distinguishable",
		toolRounds, len(seenCallIDs))
}

// TestProviderCallEvents_CarryRoundAndCallID verifies the provider side of the
// linkage, and settles a premise in #1840: provider.call.started IS emitted.
func TestProviderCallEvents_CarryRoundAndCallID(t *testing.T) {
	const toolRounds = 2
	// toolRounds tool-calling rounds plus one final text round, each emitting
	// a started and a completed event.
	got := runScriptedLoop(t, toolRounds, (toolRounds+1)*2,
		events.EventProviderCallStarted, events.EventProviderCallCompleted)

	var started, completed []*events.Event
	for _, e := range got {
		switch e.Type {
		case events.EventProviderCallStarted:
			started = append(started, e)
		case events.EventProviderCallCompleted:
			completed = append(completed, e)
		}
	}

	require.NotEmpty(t, started, "provider.call.started was not emitted at all")
	require.NotEmpty(t, completed, "provider.call.completed was not emitted")

	startIDs := make(map[string]int)
	for _, e := range started {
		d := e.Data.(*events.ProviderCallStartedData)
		assert.NotZero(t, d.Round, "provider.call.started stamped round 0")
		require.NotEmpty(t, d.CallID, "provider.call.started carries no call ID")
		startIDs[d.CallID] = d.Round
	}

	// Every completed call must match a started call by ID, so a consumer can
	// pair them rather than guessing from ordering.
	for _, e := range completed {
		d := e.Data.(*events.ProviderCallCompletedData)
		assert.NotZero(t, d.Round, "provider.call.completed stamped round 0")
		require.NotEmpty(t, d.CallID, "provider.call.completed carries no call ID")
		round, ok := startIDs[d.CallID]
		assert.Truef(t, ok, "completed call %q has no matching started call", d.CallID)
		assert.Equalf(t, round, d.Round,
			"call %q reports different rounds on started vs completed", d.CallID)
	}
}

// TestToolCallEvents_ProviderCallIDMatchesItsRound ties the two halves
// together: the ID on a tool event must be the ID of the provider call from
// the SAME round, not merely some provider call.
func TestToolCallEvents_ProviderCallIDMatchesItsRound(t *testing.T) {
	const toolRounds = 3
	// toolRounds tool events plus toolRounds+1 provider-started events.
	got := runScriptedLoop(t, toolRounds, toolRounds+(toolRounds+1),
		events.EventToolCallStarted, events.EventProviderCallStarted)

	roundToProviderID := make(map[int]string)
	for _, e := range got {
		if d, ok := e.Data.(*events.ProviderCallStartedData); ok {
			roundToProviderID[d.Round] = d.CallID
		}
	}
	require.NotEmpty(t, roundToProviderID)

	checked := 0
	for _, e := range got {
		d, ok := e.Data.(*events.ToolCallEventData)
		if !ok || e.Type != events.EventToolCallStarted {
			continue
		}
		want, found := roundToProviderID[d.Round]
		require.Truef(t, found, "no provider call recorded for round %d", d.Round)
		assert.Equalf(t, want, d.ProviderCallID,
			"tool call in round %d points at another round's provider call", d.Round)
		checked++
	}
	assert.Equal(t, toolRounds, checked, "did not check every round's tool call")
}

// waitForEvents polls snapshot until at least want events have arrived, then
// returns them. The bus dispatches through a worker pool, so events arrive
// asynchronously AND out of publish order — never assume either.
func waitForEvents(t *testing.T, snapshot func() []*events.Event, want int) []*events.Event {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(snapshot()) >= want {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	got := snapshot()
	require.GreaterOrEqualf(t, len(got), want, "timed out waiting for %d events, got %d", want, len(got))
	return got
}

// drainEvents waits a short fixed interval and returns whatever arrived. Use it
// to assert an event was NOT emitted: unlike waitForEvents there is nothing to
// poll for, so it must give the bus real time to deliver a wrong send.
func drainEvents(t *testing.T, snapshot func() []*events.Event) []*events.Event {
	t.Helper()
	time.Sleep(300 * time.Millisecond)
	return snapshot()
}
