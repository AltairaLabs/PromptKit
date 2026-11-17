package gemini

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Gemini Live can report a turn's usage on any of its server messages: a
// usage-only message, a model turn before the last, the toolCall. All of it
// is reported once, on the chunk that ends the turn.

func textUsage(prompt, response int) *UsageMetadata {
	return &UsageMetadata{PromptTokenCount: prompt, ResponseTokenCount: response,
		PromptTokensDetails:   []ModalityTokenCount{{Modality: "TEXT", TokenCount: prompt}},
		ResponseTokensDetails: []ModalityTokenCount{{Modality: "TEXT", TokenCount: response}}}
}

// priced is what usage costs on a fresh session like s.
func priced(t *testing.T, usage *UsageMetadata) *types.CostInfo {
	t.Helper()
	return newProtocolTestSession(t).buildCostInfo(usage)
}

// costs returns the chunks that carry a cost.
func costs(chunks []providers.StreamChunk) []providers.StreamChunk {
	var out []providers.StreamChunk
	for _, c := range chunks {
		if c.CostInfo != nil {
			out = append(out, c)
		}
	}
	return out
}

func TestLiveTurnUsage_AccumulatedOntoTheTurnsFinalChunk(t *testing.T) {
	s := newProtocolTestSession(t)
	process(t, s,
		&ServerMessage{UsageMetadata: textUsage(100, 0)},
		&ServerMessage{ServerContent: &ServerContent{ModelTurn: &ModelTurn{Parts: []Part{{Text: "It is "}}}},
			UsageMetadata: textUsage(20, 5)},
		&ServerMessage{ServerContent: &ServerContent{ModelTurn: &ModelTurn{Parts: []Part{{Text: "hazy."}}},
			TurnComplete: true}, UsageMetadata: textUsage(3, 7)},
	)

	got := costs(drainChunks(t, s))
	require.Len(t, got, 1, "the turn's usage is reported once")
	require.NotNil(t, got[0].FinishReason, "on the chunk that ends the turn")
	assert.Equal(t, 123, got[0].CostInfo.InputTokens)
	assert.Equal(t, 12, got[0].CostInfo.OutputTokens)
	want := priced(t, textUsage(123, 12)).TotalCost
	assert.Positive(t, want)
	assert.InDelta(t, want, got[0].CostInfo.TotalCost, 1e-12)
}

func TestLiveTurnUsage_ToolCallMessageUsageIsOnTheToolCallTurn(t *testing.T) {
	s := newProtocolTestSession(t)
	process(t, s,
		&ServerMessage{ServerContent: &ServerContent{ModelTurn: &ModelTurn{Parts: []Part{{Text: "Checking."}}}},
			UsageMetadata: textUsage(40, 2)},
		&ServerMessage{ToolCall: &ToolCallMsg{FunctionCalls: []FunctionCall{{ID: "call_1", Name: "get_weather"}}},
			UsageMetadata: textUsage(500, 9)},
	)

	ends := finishing(drainChunks(t, s))
	require.Len(t, ends, 1)
	assert.Equal(t, types.FinishReasonToolUse, *ends[0].FinishReason)
	require.NotNil(t, ends[0].CostInfo, "the tool-call turn is charged")
	assert.Equal(t, 540, ends[0].CostInfo.InputTokens)
	assert.Equal(t, 11, ends[0].CostInfo.OutputTokens)
	assert.InDelta(t, priced(t, textUsage(540, 11)).TotalCost, ends[0].CostInfo.TotalCost, 1e-12)
}

func TestLiveTurnUsage_InterruptedTurnCarriesItsUsage(t *testing.T) {
	s := newProtocolTestSession(t)
	process(t, s,
		&ServerMessage{ServerContent: &ServerContent{ModelTurn: &ModelTurn{Parts: []Part{{Text: "Once upon"}}}},
			UsageMetadata: textUsage(80, 4)},
		&ServerMessage{ServerContent: &ServerContent{Interrupted: true}},
		&ServerMessage{ServerContent: &ServerContent{TurnComplete: true}, UsageMetadata: textUsage(0, 6)},
	)

	chunks := drainChunks(t, s)
	got := costs(chunks)
	require.Len(t, got, 2)
	assert.True(t, got[0].Interrupted, "the interruption carries the usage so far")
	assert.Equal(t, 80, got[0].CostInfo.InputTokens)
	assert.Equal(t, 4, got[0].CostInfo.OutputTokens)
	require.NotNil(t, got[1].FinishReason, "the turn's completion carries the rest")
	assert.Equal(t, 6, got[1].CostInfo.OutputTokens)
	assert.Zero(t, got[1].CostInfo.InputTokens)
}
