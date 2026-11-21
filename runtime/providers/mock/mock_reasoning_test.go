package mock

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

type reasoningChunk struct{ Reasoning, Delta string }

func collectReasoning(t *testing.T, ch <-chan providers.StreamChunk) []reasoningChunk {
	t.Helper()
	var chunks []reasoningChunk
	for c := range ch {
		require.NoError(t, c.Error)
		chunks = append(chunks, reasoningChunk{Reasoning: c.Reasoning, Delta: c.Delta})
	}
	return chunks
}

// A turn that scripts its reasoning streams it one chunk per entry, as each
// chunk's Reasoning, before the reply's text, on both the plain and the
// tool-calling stream.
func TestReasoningChunks_StreamBeforeTheText(t *testing.T) {
	turn := Turn{Type: "text", ReasoningChunks: []string{"Think", "ing."}, StreamChunks: []string{"Hi."}}
	req := providers.PredictionRequest{Messages: []types.Message{{Role: "user", Content: "hi"}}}
	want := []reasoningChunk{{Reasoning: "Think"}, {Reasoning: "ing."}, {Delta: "Hi."}, {}}

	tp := NewToolProviderWithRepository("p", "m", false, &oneTurnRepo{turn: turn})
	ch, err := tp.PredictStreamWithTools(t.Context(), req, nil, "auto")
	require.NoError(t, err)
	assert.Equal(t, want, collectReasoning(t, ch))

	plain, err := NewProviderWithRepository("p", "m", false, &oneTurnRepo{turn: turn}).PredictStream(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, want, collectReasoning(t, plain))
}

// A reply that does not stream returns the scripted reasoning whole; one
// that scripts none returns no reasoning.
func TestReasoningChunks_ReturnedWholeWhenNotStreamed(t *testing.T) {
	req := providers.PredictionRequest{Messages: []types.Message{{Role: "user", Content: "hi"}}}

	tp := NewToolProviderWithRepository("p", "m", false,
		&oneTurnRepo{turn: Turn{Type: "text", Content: "Hi.", ReasoningChunks: []string{"Think", "ing."}}})
	resp, _, err := tp.PredictWithTools(t.Context(), req, nil, "auto")
	require.NoError(t, err)
	assert.Equal(t, &types.ReasoningTrace{Text: "Thinking."}, resp.Reasoning)

	called := NewToolProviderWithRepository("p", "m", false, &oneTurnRepo{turn: Turn{
		Type: "tool_calls", ToolCalls: []ToolCall{{Name: "lookup"}}, ReasoningChunks: []string{"Look it up."},
	}})
	resp, _, err = called.PredictWithTools(t.Context(), req, nil, "auto")
	require.NoError(t, err)
	assert.Equal(t, &types.ReasoningTrace{Text: "Look it up."}, resp.Reasoning)

	plain := NewProviderWithRepository("p", "m", false,
		&oneTurnRepo{turn: Turn{Type: "text", Content: "Hi.", ReasoningChunks: []string{"Hmm."}}})
	plainResp, err := plain.Predict(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, &types.ReasoningTrace{Text: "Hmm."}, plainResp.Reasoning)

	none := NewToolProviderWithRepository("p", "m", false, &oneTurnRepo{turn: Turn{Type: "text", Content: "Hi."}})
	resp, _, err = none.PredictWithTools(t.Context(), req, nil, "auto")
	require.NoError(t, err)
	assert.Nil(t, resp.Reasoning)
}
