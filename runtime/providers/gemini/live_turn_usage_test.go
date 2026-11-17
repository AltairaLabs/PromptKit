package gemini

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// These cases replay the message shapes captured live from
// gemini-2.5-flash-native-audio-preview-12-2025 and gemini-3.8-live (a text
// turn, a tool-call turn, a two-tool chain): usage arrives only on
// turnComplete, and each turnComplete carries the usage of the generation it
// ends (see live_usage.go).

func textUsage(prompt, response int) *UsageMetadata {
	return &UsageMetadata{PromptTokenCount: prompt, ResponseTokenCount: response,
		PromptTokensDetails:   []ModalityTokenCount{{Modality: "TEXT", TokenCount: prompt}},
		ResponseTokensDetails: []ModalityTokenCount{{Modality: "TEXT", TokenCount: response}}}
}

// priced is what usage costs on a fresh protocol test session.
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

func toolCall(id, city string) *ServerMessage {
	return &ServerMessage{ToolCall: &ToolCallMsg{FunctionCalls: []FunctionCall{
		{ID: id, Name: "get_weather", Args: map[string]interface{}{"city": city}}}}}
}

func generationComplete() *ServerMessage {
	return &ServerMessage{ServerContent: &ServerContent{GenerationComplete: true}}
}

func turnComplete(u *UsageMetadata) *ServerMessage {
	return &ServerMessage{ServerContent: &ServerContent{TurnComplete: true}, UsageMetadata: u}
}

func said(text string) *ServerMessage {
	return &ServerMessage{ServerContent: &ServerContent{OutputTranscription: &Transcription{Text: text}}}
}

// assertCosts asserts each cost chunk's input/output tokens, in order, and
// whether it ends a turn.
func assertCosts(t *testing.T, chunks []providers.StreamChunk, want ...[3]int) {
	t.Helper()
	got := costs(chunks)
	require.Len(t, got, len(want), "cost chunks")
	for i, w := range want {
		assert.Equal(t, w[0], got[i].CostInfo.InputTokens, "chunk %d input", i)
		assert.Equal(t, w[1], got[i].CostInfo.OutputTokens, "chunk %d output", i)
		assert.Equal(t, w[2] == 1, got[i].FinishReason != nil, "chunk %d ends a turn", i)
	}
}

// Gemini 2.5: one turnComplete after a two-tool chain and its reply carries
// the whole server turn's usage; the tool calls carry none.
func TestLiveUsage_25ChainIsReportedOnTheReply(t *testing.T) {
	s := newProtocolTestSession(t)
	s.config.Model = "gemini-2.5-flash-native-audio-preview-12-2025"
	process(t, s, toolCall("c1", "Rome"), toolCall("c2", "Oslo"), said("Rome is hazy, Oslo too."),
		generationComplete(), turnComplete(textUsage(932, 244)))

	chunks := drainChunks(t, s)
	ends := finishing(chunks)
	require.Len(t, ends, 3)
	assert.Equal(t, types.FinishReasonToolUse, *ends[0].FinishReason)
	assert.Equal(t, types.FinishReasonToolUse, *ends[1].FinishReason)
	assertCosts(t, chunks, [3]int{932, 244, 1})
	assert.InDelta(t, priced(t, textUsage(932, 244)).TotalCost, ends[2].CostInfo.TotalCost, 1e-12)
}

// Gemini 3.8: each tool-call generation ends with its own turnComplete. Each
// is the generation's own usage (not a running total), reported once, on its
// own, as it arrives; the reply carries only its own.
func TestLiveUsage_38ChainReportsEachGenerationOnce(t *testing.T) {
	s := newProtocolTestSession(t)
	process(t, s,
		toolCall("c1", "Rome"), generationComplete(), turnComplete(textUsage(919, 12)),
		toolCall("c2", "Oslo"), generationComplete(), turnComplete(textUsage(952, 13)),
		said("Rome is hazy, Oslo too."), generationComplete(), turnComplete(textUsage(986, 129)))

	chunks := drainChunks(t, s)
	require.Len(t, finishing(chunks), 3)
	assertCosts(t, chunks, [3]int{919, 12, 0}, [3]int{952, 13, 0}, [3]int{986, 129, 1})
}

// Usage on a toolCall message is never counted: the generation's
// turnComplete reports it.
func TestLiveUsage_ToolCallUsageIsNotCountedTwice(t *testing.T) {
	s := newProtocolTestSession(t)
	s.config.Model = "gemini-2.5-flash-native-audio-preview-12-2025"
	m := toolCall("c1", "Paris")
	m.UsageMetadata = textUsage(500, 9)
	process(t, s, m, said("It is hazy."), turnComplete(textUsage(697, 116)))

	assertCosts(t, drainChunks(t, s), [3]int{697, 116, 1})
}

// Usage on a message of its own is the turn's: summed into its end while the
// turn is under way, reported at once right after the turn ended.
func TestLiveUsage_UsageOnlyMessages(t *testing.T) {
	s := newProtocolTestSession(t)
	process(t, s,
		said("Hello"), &ServerMessage{UsageMetadata: textUsage(20, 5)},
		&ServerMessage{ServerContent: &ServerContent{ModelTurn: &ModelTurn{Parts: []Part{{Text: "there."}}},
			TurnComplete: true}, UsageMetadata: textUsage(3, 7)},
		&ServerMessage{UsageMetadata: textUsage(0, 4)},
	)

	assertCosts(t, drainChunks(t, s), [3]int{23, 12, 1}, [3]int{0, 4, 0})
}

func TestLiveUsage_InterruptedTurnCarriesItsUsage(t *testing.T) {
	s := newProtocolTestSession(t)
	process(t, s,
		said("Once upon"), &ServerMessage{UsageMetadata: textUsage(80, 4)},
		&ServerMessage{ServerContent: &ServerContent{Interrupted: true}},
		turnComplete(textUsage(0, 6)),
	)

	chunks := drainChunks(t, s)
	got := costs(chunks)
	require.Len(t, got, 2)
	assert.True(t, got[0].Interrupted, "the interruption carries the usage so far")
	assertCosts(t, chunks, [3]int{80, 4, 0}, [3]int{0, 6, 1})
}
