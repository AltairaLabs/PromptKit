package evals

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func TestExtractToolsOffered_ReadsMessageMeta(t *testing.T) {
	messages := []types.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Meta: map[string]any{
			types.MetaToolsOffered: []string{"refund", "get_order"},
		}},
	}

	assert.Equal(t, []string{"get_order", "refund"}, ExtractToolsOffered(messages),
		"names come back sorted")
}

// A turn can offer a different set on each round — skill grants widen it
// mid-turn — so the union is what "this turn offered" means.
func TestExtractToolsOffered_UnionsAcrossMessages(t *testing.T) {
	messages := []types.Message{
		{Role: "assistant", Meta: map[string]any{
			types.MetaToolsOffered: []string{"get_order"},
		}},
		{Role: "assistant", Meta: map[string]any{
			types.MetaToolsOffered: []string{"get_order", "refund"},
		}},
	}

	assert.Equal(t, []string{"get_order", "refund"}, ExtractToolsOffered(messages))
}

// Meta survives a JSON round-trip through the state store, which turns
// []string into []any. Handling only the former would mean the set silently
// vanished on replay — and an eval reading it would report "the host does not
// populate this" for a host that does.
func TestExtractToolsOffered_SurvivesJSONRoundTrip(t *testing.T) {
	original := []types.Message{
		{Role: "assistant", Meta: map[string]any{
			types.MetaToolsOffered: []string{"refund"},
		}},
	}

	encoded, err := json.Marshal(original)
	require.NoError(t, err)
	var restored []types.Message
	require.NoError(t, json.Unmarshal(encoded, &restored))

	require.IsType(t, []any{}, restored[0].Meta[types.MetaToolsOffered],
		"round-trip must actually degrade the type, or this test proves nothing")
	assert.Equal(t, []string{"refund"}, ExtractToolsOffered(restored))
}

// Nil for "nothing recorded" is load-bearing: the tools_offered eval keys its
// "could not be judged" branch off an empty result. Asserting the nil alone
// would pass even if the function always returned nil, so this pins that the
// same input shape flips to a real answer once the meta is there.
func TestExtractToolsOffered_DiscriminatesRecordedFromNot(t *testing.T) {
	without := []types.Message{{Role: "assistant"}}
	assert.Nil(t, ExtractToolsOffered(without), "no meta means nothing was recorded")

	with := []types.Message{{Role: "assistant", Meta: map[string]any{
		types.MetaToolsOffered: []string{"refund"},
	}}}
	assert.Equal(t, []string{"refund"}, ExtractToolsOffered(with),
		"the same shape with meta must return the recorded set")

	// An empty recorded list is still "nothing offered", not a phantom entry.
	empty := []types.Message{{Role: "assistant", Meta: map[string]any{
		types.MetaToolsOffered: []string{},
	}}}
	assert.Nil(t, ExtractToolsOffered(empty))
}

func TestBuildEvalContext_PopulatesToolsOffered(t *testing.T) {
	messages := []types.Message{
		{Role: "user", Content: "refund me"},
		{Role: "assistant", Content: "sure", Meta: map[string]any{
			types.MetaToolsOffered: []string{"refund"},
		}},
	}

	ctx := BuildEvalContext(messages, 0, "session", "prompt", nil)
	assert.Equal(t, []string{"refund"}, ctx.ToolsOffered)
}

// twoTurnTranscript is a conversation whose first turn offered a tool the
// second did not — the shape every turn-scoping assertion below turns on.
func twoTurnTranscript() []types.Message {
	return []types.Message{
		{Role: "user", Content: "turn 1"},
		{Role: "assistant", Content: "a1", Meta: map[string]any{
			types.MetaToolsOffered: []string{"get_order", "refund"},
		}},
		{Role: "user", Content: "turn 2"},
		{Role: "assistant", Content: "a2", Meta: map[string]any{
			types.MetaToolsOffered: []string{"get_order"},
		}},
	}
}

// ToolsOffered is "what THIS turn offered". Messages is the history up to the
// current turn, so extracting over all of it answers a different question —
// one where `absent: true` can never fail after the tool has been offered
// once anywhere in the conversation (#2037).
func TestBuildEvalContext_ToolsOfferedIsScopedToTheCurrentTurn(t *testing.T) {
	ctx := BuildEvalContext(twoTurnTranscript(), 2, "session", "prompt", nil)

	assert.Equal(t, []string{"get_order"}, ctx.ToolsOffered,
		"turn 2 offered only get_order; turn 1's set is not this turn's")
	assert.Len(t, ctx.Messages, 4,
		"scoping must narrow the offered set only — Messages stays the full history")
}

// The guardrail context judges a message mid-turn and is handed the same full
// history, so it needs the same scoping.
func TestBuildGuardrailEvalContext_ToolsOfferedIsScopedToTheCurrentTurn(t *testing.T) {
	ctx := BuildGuardrailEvalContext(twoTurnTranscript(), "a2", nil)

	assert.Equal(t, []string{"get_order"}, ctx.ToolsOffered)
}

// A turn's rounds still union: the grant that widens the set mid-turn is the
// whole point of recording it. Scoping must cut at the turn boundary, not at
// the last assistant message.
func TestBuildEvalContext_ToolsOfferedUnionsRoundsWithinTheTurn(t *testing.T) {
	messages := []types.Message{
		{Role: "user", Content: "turn 1"},
		{Role: "assistant", Content: "a1", Meta: map[string]any{
			types.MetaToolsOffered: []string{"audit"},
		}},
		{Role: "user", Content: "turn 2"},
		// Round 1 of turn 2, before the skill activates.
		{Role: "assistant", Meta: map[string]any{
			types.MetaToolsOffered: []string{"get_order"},
		}},
		{Role: "tool", Content: "{}"},
		// Round 2, after it grants refund.
		{Role: "assistant", Content: "done", Meta: map[string]any{
			types.MetaToolsOffered: []string{"get_order", "refund"},
		}},
	}

	ctx := BuildEvalContext(messages, 2, "session", "prompt", nil)
	assert.Equal(t, []string{"get_order", "refund"}, ctx.ToolsOffered,
		"both of this turn's rounds count; turn 1's audit does not")
}

// The boundary is the user message, not the last assistant one. A guardrail
// judging the user's input fires before this turn has an assistant message at
// all, so cutting at the last assistant hands it the PREVIOUS turn's set —
// which reads as "this turn offers refund" when this turn has offered nothing
// yet. Nil is the honest answer, and the tools_offered handler says so.
//
// Asserting that nil alone would pass if ToolsOffered were never populated at
// all, so the same transcript is run twice: once with the turn still empty and
// once with its round recorded. The pair is what pins the boundary — the first
// case fails if the cut moves to the last assistant message, the second fails
// if scoping drops the current turn's own evidence.
func TestBuildGuardrailEvalContext_ToolsOfferedStartsEmptyThenRecords(t *testing.T) {
	midTurn := []types.Message{
		{Role: "user", Content: "turn 1"},
		{Role: "assistant", Content: "a1", Meta: map[string]any{
			types.MetaToolsOffered: []string{"refund"},
		}},
		{Role: "user", Content: "turn 2"},
	}

	before := BuildGuardrailEvalContext(midTurn, "turn 2", nil)
	assert.Nil(t, before.ToolsOffered,
		"turn 1's set is not evidence about turn 2")

	// Turn 2's first round lands. Same transcript, one message longer.
	afterRound1 := append(midTurn, types.Message{
		Role: "assistant", Content: "a2", Meta: map[string]any{
			types.MetaToolsOffered: []string{"get_order"},
		},
	})

	after := BuildGuardrailEvalContext(afterRound1, "a2", nil)
	assert.Equal(t, []string{"get_order"}, after.ToolsOffered,
		"once this turn records a set, that set is the answer — and it is not turn 1's")
}
