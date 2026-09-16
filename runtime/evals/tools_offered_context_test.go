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
