package stage

import (
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestReasoningCompleted_ReachesBusWithoutRecordingStage is the assertion that
// matters most for this event's existence.
//
// message.created — the only event carrying a complete message — is produced
// by RecordingStage, which is OPT-IN (WithRecording + an EventStore). A
// consumer that does not enable recording receives no message.created at all,
// so the reasoning trace carried there never reaches it. Such a consumer is
// left re-accumulating reasoning.delta fragments and inventing a turn boundary
// — exactly what emitting the assembled trace is meant to avoid.
//
// This test therefore runs with NO recording stage anywhere.
func TestReasoningCompleted_ReachesBusWithoutRecordingStage(t *testing.T) {
	const toolRounds = 2 // plus a final text round = 3 reasoning traces
	const wantTraces = toolRounds + 1

	got := runScriptedLoop(t, toolRounds, wantTraces, events.EventReasoningCompleted)
	require.Len(t, got, wantTraces,
		"a consumer without a recording stage received the wrong number of reasoning traces")

	byRound := map[int]*events.ReasoningCompletedData{}
	for _, e := range got {
		d, ok := e.Data.(*events.ReasoningCompletedData)
		require.True(t, ok, "unexpected payload %T", e.Data)
		require.NotNilf(t, d.Trace, "round %d emitted a nil trace", d.Round)
		assert.NotEmptyf(t, d.Trace.Text, "round %d emitted an empty trace", d.Round)
		assert.NotZero(t, d.Round, "reasoning.completed stamped round 0")
		assert.NotEmpty(t, d.ProviderCallID, "reasoning.completed carries no provider call ID")
		byRound[d.Round] = d
	}

	require.Len(t, byRound, wantTraces, "expected one trace per round")

	// Each round's trace must be its own, not the last round's repeated.
	for round, d := range byRound {
		assert.Containsf(t, d.Trace.Text, fmt.Sprintf("round %d:", round),
			"round %d carries another round's reasoning: %q", round, d.Trace.Text)
	}
}

// TestReasoningCompleted_CorrelatesWithItsRoundsToolCalls is the correlation
// this event exists to support: given a reasoning trace, which tool calls did
// that thinking lead to?
//
// Both sides carry the same ProviderCallID, so the answer is a lookup rather
// than an inference from event ordering — which a bus that drops under burst
// could not support anyway.
func TestReasoningCompleted_CorrelatesWithItsRoundsToolCalls(t *testing.T) {
	const toolRounds = 2
	// 3 reasoning traces + 2 tool.call.started
	got := runScriptedLoop(t, toolRounds, toolRounds+1+toolRounds,
		events.EventReasoningCompleted, events.EventToolCallStarted)

	reasoningByCall := map[string]string{}
	toolsByCall := map[string][]string{}
	for _, e := range got {
		switch d := e.Data.(type) {
		case *events.ReasoningCompletedData:
			reasoningByCall[d.ProviderCallID] = d.Trace.Text
		case *events.ToolCallEventData:
			toolsByCall[d.ProviderCallID] = append(toolsByCall[d.ProviderCallID], d.ToolName)
		}
	}

	require.NotEmpty(t, toolsByCall, "no tool calls were recorded")

	// Every round that called tools must have reasoning reachable by the same
	// key — that is the join a transcript needs.
	for callID, tools := range toolsByCall {
		reasoning, ok := reasoningByCall[callID]
		assert.Truef(t, ok, "tool calls %v have no reasoning reachable by their provider call ID %q",
			tools, callID)
		assert.NotEmptyf(t, reasoning, "provider call %q joined to an empty trace", callID)
	}
}

// TestReasoningCompleted_EmittedPerRoundWithCorrelation drives afterRound
// directly so each round's reasoning is controlled, and asserts the join key a
// consumer needs: which round reasoned, and which provider call — and so which
// tool calls — that reasoning belongs to.
func TestReasoningCompleted_EmittedPerRoundWithCorrelation(t *testing.T) {
	bus := events.NewEventBus()
	emitter := events.NewEmitter(bus, "run", "sess", "conv")
	snapshot := collectEvents(bus, events.EventReasoningCompleted)

	stage := &ProviderStage{emitter: emitter, config: &ProviderConfig{}}
	loop := &toolLoop{stage: stage}

	rounds := []struct {
		reasoning string
		callID    string
	}{
		{"I need the temperature before I can answer.", "pc-aaa"},
		{"Now the humidity.", "pc-bbb"},
		{"I have both; answer directly.", "pc-ccc"},
	}

	for i, r := range rounds {
		resp := types.Message{
			Role:      roleAssistant,
			Reasoning: &types.ReasoningTrace{Text: r.reasoning},
		}
		// hasToolCalls=false ends the loop immediately after the emit, which is
		// all this test needs.
		_, _, err := loop.afterRound(t.Context(), nil, &resp, false,
			roundRef{round: i + 1, providerCallID: r.callID})
		require.NoError(t, err)
	}

	got := waitForEvents(t, snapshot, len(rounds))

	byRound := map[int]*events.ReasoningCompletedData{}
	for _, e := range got {
		d, ok := e.Data.(*events.ReasoningCompletedData)
		require.True(t, ok, "unexpected payload %T", e.Data)
		byRound[d.Round] = d
	}

	require.Len(t, byRound, len(rounds), "expected one reasoning.completed per round")

	seenIDs := map[string]bool{}
	for i, r := range rounds {
		d := byRound[i+1]
		require.NotNilf(t, d, "round %d emitted no reasoning.completed", i+1)
		require.NotNil(t, d.Trace)
		assert.Equalf(t, r.reasoning, d.Trace.Text, "round %d carries another round's reasoning", i+1)
		assert.Equalf(t, r.callID, d.ProviderCallID,
			"round %d reasoning points at the wrong provider call", i+1)
		seenIDs[d.ProviderCallID] = true
	}

	// Correlation is only useful if the key actually distinguishes rounds.
	assert.Len(t, seenIDs, len(rounds), "provider call IDs are not distinct per round")

	rs := make([]int, 0, len(byRound))
	for r := range byRound {
		rs = append(rs, r)
	}
	sort.Ints(rs)
	assert.Equal(t, []int{1, 2, 3}, rs, "rounds must be 1-based and cover every round")
}

// TestReasoningCompleted_SilentWhenRoundDidNotReason keeps "this round did not
// reason" distinguishable from "the trace was dropped en route". A round with
// no reasoning must emit nothing at all rather than an empty event.
func TestReasoningCompleted_SilentWhenRoundDidNotReason(t *testing.T) {
	bus := events.NewEventBus()
	emitter := events.NewEmitter(bus, "run", "sess", "conv")
	snapshot := collectEvents(bus, events.EventReasoningCompleted)

	stage := &ProviderStage{emitter: emitter, config: &ProviderConfig{}}
	loop := &toolLoop{stage: stage}

	cases := []*types.ReasoningTrace{
		nil,                     // provider returned no reasoning
		{Text: ""},              // present but empty
		{Text: "", Opaque: nil}, // explicitly empty
	}
	for _, tr := range cases {
		resp := types.Message{Role: roleAssistant, Reasoning: tr}
		_, _, err := loop.afterRound(t.Context(), nil, &resp, false, roundRef{round: 1, providerCallID: "pc-x"})
		require.NoError(t, err)
	}

	// Give the bus a chance to deliver anything it should not have sent.
	assert.Empty(t, drainEvents(t, snapshot),
		"a round that produced no reasoning must emit nothing, not an empty trace")
}

// TestReasoningDelta_CarriesRoundAttribution covers the streaming half. A
// consumer watching thinking arrive needs to know which model turn it is
// watching, and needs the fragments to agree with the reasoning.completed that
// follows them — otherwise the two reasoning signals cannot be reconciled.
func TestReasoningDelta_CarriesRoundAttribution(t *testing.T) {
	bus := events.NewEventBus()
	emitter := events.NewEmitter(bus, "run", "sess", "conv")
	snapshot := collectEvents(bus, events.EventReasoningDelta)

	stage := &ProviderStage{emitter: emitter, config: &ProviderConfig{}}

	out := make(chan StreamElement, 8)
	chunk := &providers.StreamChunk{Reasoning: "weighing the options"}
	require.NoError(t, stage.emitChunkElement(t.Context(), chunk, out,
		roundRef{round: 3, providerCallID: "pc-zzz"}, false))

	got := waitForEvents(t, snapshot, 1)
	d, ok := got[0].Data.(*events.ReasoningDeltaData)
	require.True(t, ok, "unexpected payload %T", got[0].Data)

	assert.Equal(t, "weighing the options", d.Text)
	assert.Equal(t, 3, d.Round, "reasoning.delta stamped the wrong round")
	assert.Equal(t, "pc-zzz", d.ProviderCallID,
		"reasoning.delta must share its round's provider call ID so deltas and the "+
			"assembled trace can be reconciled")
}
