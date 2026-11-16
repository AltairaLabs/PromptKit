package gemini

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// newProtocolTestSession is a StreamSession that processes server messages
// and delivers its chunks to Response(), with no socket.
func newProtocolTestSession(t *testing.T) *StreamSession {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	emitCh := make(chan providers.StreamChunk, 32)
	pump := providers.NewStreamPump(ctx, emitCh, 32)
	pump.Start()
	return &StreamSession{
		StreamPump: pump,
		ctx:        ctx,
		emitCh:     emitCh,
		pricing:    sessionPricing(&StreamSessionConfig{Model: "gemini-3.8-live"}),
		providerID: "gemini",
	}
}

// drainChunks returns every chunk Response() delivers until none arrives for
// a short while.
func drainChunks(t *testing.T, s *StreamSession) []providers.StreamChunk {
	t.Helper()
	var out []providers.StreamChunk
	for {
		select {
		case c := <-s.Response():
			out = append(out, c)
		case <-time.After(200 * time.Millisecond):
			return out
		}
	}
}

func finishing(chunks []providers.StreamChunk) []providers.StreamChunk {
	var out []providers.StreamChunk
	for _, c := range chunks {
		if c.FinishReason != nil {
			out = append(out, c)
		}
	}
	return out
}

// Gemini 3.x Live answers a tool call with toolCall, generationComplete, then
// a turnComplete carrying only the usage of the tool-call generation (observed
// live on gemini-3.8-live). That usage-only turnComplete is not a model turn:
// reported as one, it reads as an empty reply after every tool call (the
// stage records an empty assistant message, the SDK reports "empty response,
// likely interrupted", and the realtime tool turn ends before the tool's
// result is answered). Its usage is carried into the turn that follows, as
// Gemini 2.5 reports it natively (one turnComplete after the reply, with the
// usage of both).
func TestLiveToolCallTurn_UsageOnlyTurnCompleteIsCarriedToTheReply(t *testing.T) {
	s := newProtocolTestSession(t)
	usage := func(prompt, response int) *UsageMetadata {
		return &UsageMetadata{PromptTokenCount: prompt, ResponseTokenCount: response,
			PromptTokensDetails:   []ModalityTokenCount{{Modality: "TEXT", TokenCount: prompt}},
			ResponseTokensDetails: []ModalityTokenCount{{Modality: "TEXT", TokenCount: response}}}
	}
	msgs := []*ServerMessage{
		{ToolCall: &ToolCallMsg{FunctionCalls: []FunctionCall{
			{ID: "call_1", Name: "get_weather", Args: map[string]interface{}{"city": "Paris"}}}}},
		{ServerContent: &ServerContent{GenerationComplete: true}},
		{ServerContent: &ServerContent{TurnComplete: true}, UsageMetadata: usage(600, 12)},
		{ServerContent: &ServerContent{ModelTurn: &ModelTurn{Parts: []Part{{Text: "It is hazy."}}}}},
		{ServerContent: &ServerContent{GenerationComplete: true}},
		{ServerContent: &ServerContent{TurnComplete: true}, UsageMetadata: usage(700, 20)},
	}
	for _, m := range msgs {
		require.NoError(t, s.processServerMessage(m))
	}

	chunks := drainChunks(t, s)
	ends := finishing(chunks)
	require.Len(t, ends, 2, "a tool-call turn and its reply are two turns")
	assert.Equal(t, types.FinishReasonToolUse, *ends[0].FinishReason)
	require.Len(t, ends[0].ToolCalls, 1)
	assert.Equal(t, "call_1", ends[0].ToolCalls[0].ID)

	assert.Equal(t, finishReasonComplete, *ends[1].FinishReason)
	var text string
	for _, c := range chunks {
		text += c.Content
	}
	assert.Equal(t, "It is hazy.", text)
	require.NotNil(t, ends[1].CostInfo)
	assert.Equal(t, 1300, ends[1].CostInfo.InputTokens, "the reply carries the tool-call turn's usage too")
	assert.Equal(t, 32, ends[1].CostInfo.OutputTokens)
	want := s.buildCostInfo(usage(1300, 32)).TotalCost
	assert.InDelta(t, want, ends[1].CostInfo.TotalCost, 1e-12)
	assert.Positive(t, ends[1].CostInfo.TotalCost)
}

// Gemini 2.5 Live answers with toolCall, then (after the tool response) the
// reply and one turnComplete whose usage covers both: unchanged.
func TestLiveToolCallTurn_ReplyInTheSameServerTurnKeepsItsUsage(t *testing.T) {
	s := newProtocolTestSession(t)
	msgs := []*ServerMessage{
		{ToolCall: &ToolCallMsg{FunctionCalls: []FunctionCall{{ID: "call_1", Name: "get_weather"}}}},
		{ServerContent: &ServerContent{OutputTranscription: &Transcription{Text: "It is hazy."}}},
		{ServerContent: &ServerContent{GenerationComplete: true}},
		{ServerContent: &ServerContent{TurnComplete: true},
			UsageMetadata: &UsageMetadata{PromptTokenCount: 900, ResponseTokenCount: 40}},
	}
	for _, m := range msgs {
		require.NoError(t, s.processServerMessage(m))
	}

	ends := finishing(drainChunks(t, s))
	require.Len(t, ends, 2)
	assert.Equal(t, types.FinishReasonToolUse, *ends[0].FinishReason)
	assert.Equal(t, finishReasonComplete, *ends[1].FinishReason)
	require.NotNil(t, ends[1].CostInfo)
	assert.Equal(t, 900, ends[1].CostInfo.InputTokens)
	assert.Equal(t, 40, ends[1].CostInfo.OutputTokens)
}

// A turnComplete with only usage that does not follow a tool call (a plain
// turn the model ended without content) is still a turn completion.
func TestLiveToolCallTurn_UsageOnlyTurnCompleteWithoutToolCallStillEndsTheTurn(t *testing.T) {
	s := newProtocolTestSession(t)
	require.NoError(t, s.processServerMessage(&ServerMessage{
		ServerContent: &ServerContent{TurnComplete: true},
		UsageMetadata: &UsageMetadata{PromptTokenCount: 50, ResponseTokenCount: 0},
	}))

	ends := finishing(drainChunks(t, s))
	require.Len(t, ends, 1)
	require.NotNil(t, ends[0].CostInfo)
	assert.Equal(t, 50, ends[0].CostInfo.InputTokens)
}
