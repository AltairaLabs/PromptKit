package stage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/composition"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// historyFlags runs one stage over elements whose FromHistory flags are given
// and returns the flags observed downstream, in order.
func historyFlags(t *testing.T, s Stage, msgs []types.Message, from []bool) []bool {
	t.Helper()
	require.Len(t, from, len(msgs))

	in := make(chan StreamElement, len(msgs)+1)
	for i := range msgs {
		elem := NewMessageElement(&msgs[i])
		elem.Meta.FromHistory = from[i]
		in <- elem
	}
	close(in)

	out := make(chan StreamElement, len(msgs)+4)
	require.NoError(t, s.Process(context.Background(), in, out))

	var got []bool
	for elem := range out {
		if elem.Message != nil {
			got = append(got, elem.Meta.FromHistory)
		}
	}
	return got
}

func threeMessages() []types.Message {
	return []types.Message{
		{Role: "user", Content: "old question"},
		{Role: "assistant", Content: "old answer"},
		{Role: "user", Content: "new question"},
	}
}

// TestContextBuilderStage_PreservesPerMessageFromHistory is the regression for
// the live bug: emitMessages stamped the FIRST element's Meta onto every
// message, so a conversation with history marked this turn's new user message
// as history too. CompositionStage skips FromHistory elements, so composition
// silently stopped running from turn 2 whenever WithTokenBudget was set.
func TestContextBuilderStage_PreservesPerMessageFromHistory(t *testing.T) {
	s := NewContextBuilderStageWithTurnState(&ContextBuilderPolicy{
		TokenBudget: 100000, // large enough that no truncation occurs
	}, NewTurnState())

	got := historyFlags(t, s, threeMessages(), []bool{true, true, false})

	require.Equal(t, []bool{true, true, false}, got,
		"FromHistory must stay per-message; the new user message must not inherit "+
			"the first history element's flag")
}

// TestTokenBudgetStage_PreservesPerMessageFromHistory covers the other stage
// that rebuilds message elements. collectInput kept only *elem.Message, so
// emitResults produced FromHistory:false for everything.
func TestTokenBudgetStage_PreservesPerMessageFromHistory(t *testing.T) {
	s := NewTokenBudgetStageWithTurnState(&TokenBudgetConfig{}, NewTurnState())

	got := historyFlags(t, s, threeMessages(), []bool{true, true, false})

	require.Equal(t, []bool{true, true, false}, got,
		"FromHistory must survive collect/re-emit")
}

// TestContextBuilderStage_TruncatedPathDerivesFromSource covers the branch the
// per-element flags cannot serve. Truncation drops, reorders and (when
// summarizing) synthesizes messages, so index alignment is gone and
// historyFlagAt falls back to the message's own Source.
//
// The assertion deliberately does not depend on which messages survive the
// token math: whatever comes out, its FromHistory must agree with its Source.
func TestContextBuilderStage_TruncatedPathDerivesFromSource(t *testing.T) {
	msgs := []types.Message{
		{Role: "user", Content: "old question about a long topic", Source: "statestore"},
		{Role: "assistant", Content: "an old and fairly wordy answer", Source: "statestore"},
		{Role: "user", Content: "new question"},
	}

	// Budget small enough to force truncation, but not zero.
	s := NewContextBuilderStageWithTurnState(&ContextBuilderPolicy{
		TokenBudget: 12,
		Strategy:    TruncateOldest,
	}, NewTurnState())

	in := make(chan StreamElement, len(msgs)+1)
	for i := range msgs {
		elem := NewMessageElement(&msgs[i])
		// Deliberately WRONG element flags: if the truncated path used these
		// instead of Source, every assertion below would fail.
		elem.Meta.FromHistory = false
		in <- elem
	}
	close(in)

	out := make(chan StreamElement, len(msgs)+4)
	require.NoError(t, s.Process(context.Background(), in, out))

	var seen int
	for elem := range out {
		if elem.Message == nil {
			continue
		}
		seen++
		wantHistory := !isNewMessage(elem.Message)
		require.Equalf(t, wantHistory, elem.Meta.FromHistory,
			"message %q (source %q): FromHistory must be derived from Source once "+
				"truncation has broken index alignment",
			elem.Message.Content, elem.Message.Source)
	}
	require.NotZero(t, seen, "expected at least one message to survive truncation")
	require.Less(t, seen, len(msgs), "budget was too generous — truncation did not happen, "+
		"so this test is not exercising the nil-flags path")
}

// TestCompositionStage_RunsAfterContextBuilderWithHistory chains the two stages
// the way the SDK builder does — ContextBuilderStage at builder.go:398, the
// composition stages at :426 — and asserts the composition still executes on a
// turn that has history behind it.
//
// This is the user-facing bug. ContextBuilderStage stamped the first (history)
// element's Meta onto every message, so CompositionStage saw FromHistory on the
// live message too and forwarded it unchanged. Composition silently never ran
// from turn 2 onward whenever WithTokenBudget was configured. Nothing errored.
func TestCompositionStage_RunsAfterContextBuilderWithHistory(t *testing.T) {
	reg := tools.NewRegistry()
	registerEchoTool(t, reg, "echo")

	comp := &composition.Composition{
		Version: 1, Output: "a",
		Steps: []*composition.Step{
			{ID: "a", Kind: composition.KindTool, Tool: "echo", Args: map[string]any{"v": "${input.x}"}},
		},
	}

	// Budget large enough that nothing truncates: the bug is in Meta
	// propagation, not in truncation.
	cb := NewContextBuilderStageWithTurnState(&ContextBuilderPolicy{
		TokenBudget: 100000,
	}, NewTurnState())
	cs := NewCompositionStage("after-ctx", comp, CompositionExecutorDeps{ToolRegistry: reg})

	in := make(chan StreamElement, 4)
	histMsg := &types.Message{Role: "user", Content: `{"x":"history"}`, Source: "statestore"}
	histElem := NewMessageElement(histMsg)
	histElem.Meta.FromHistory = true
	in <- histElem

	liveMsg := &types.Message{Role: "user", Content: `{"x":"live"}`}
	in <- NewMessageElement(liveMsg)
	close(in)

	// ContextBuilderStage accumulates all input before emitting, so with a
	// buffered channel the two stages can run in sequence.
	mid := make(chan StreamElement, 16)
	require.NoError(t, cb.Process(context.Background(), in, mid))

	out := make(chan StreamElement, 16)
	require.NoError(t, cs.Process(context.Background(), mid, out))

	var sawAssistant bool
	elems := drainChannel(out)
	for _, e := range elems {
		if e.Message != nil && e.Message.Role == roleAssistant {
			sawAssistant = true
		}
	}
	require.Truef(t, sawAssistant,
		"composition never executed downstream of ContextBuilderStage: %+v", elems)
}
