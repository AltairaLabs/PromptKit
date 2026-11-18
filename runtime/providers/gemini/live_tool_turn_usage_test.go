package gemini

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
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
		config:     StreamSessionConfig{Model: "gemini-3.8-live"},
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
// live on gemini-3.8-live). That turnComplete is not a model turn: reported
// as one, it reads as an empty reply after every tool call (the stage records
// an empty assistant message, and the realtime tool turn ends before the
// tool's result is answered). Its usage goes on the tool calls' chunk, so the
// provider call that made them carries it (#2761).
func TestLiveToolCallTurn_TurnCompleteAfterToolCallPutsItsUsageOnTheToolCalls(t *testing.T) {
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
	var usageOnly []providers.StreamChunk
	for _, c := range chunks {
		if c.FinishReason == nil && c.CostInfo != nil {
			usageOnly = append(usageOnly, c)
		}
	}
	assert.Equal(t, types.FinishReasonToolUse, *ends[0].FinishReason)
	require.Len(t, ends[0].ToolCalls, 1)
	assert.Equal(t, "call_1", ends[0].ToolCalls[0].ID)

	assert.Equal(t, finishReasonComplete, *ends[1].FinishReason)
	var text string
	for _, c := range chunks {
		text += c.Content
	}
	assert.Equal(t, "It is hazy.", text)
	assert.Equal(t, ends[0].Generation+1, ends[1].Generation, "the reply is a generation of its own")
	// The tool-call turn's usage is on its tool calls; the reply carries only
	// its own, and nothing is reported apart.
	assert.Empty(t, usageOnly)
	require.NotNil(t, ends[0].CostInfo)
	assert.Equal(t, 600, ends[0].CostInfo.InputTokens)
	assert.Equal(t, 12, ends[0].CostInfo.OutputTokens)
	assert.InDelta(t, priced(t, usage(600, 12)).TotalCost, ends[0].CostInfo.TotalCost, 1e-12)
	require.NotNil(t, ends[1].CostInfo)
	assert.Equal(t, 700, ends[1].CostInfo.InputTokens)
	assert.Equal(t, 20, ends[1].CostInfo.OutputTokens)
	want := priced(t, usage(700, 20)).TotalCost
	assert.Positive(t, want)
	assert.InDelta(t, want, ends[1].CostInfo.TotalCost, 1e-12)
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

// scriptedLiveServer is a fake Live server: it completes setup, then runs
// script on the connection.
func scriptedLiveServer(t *testing.T, model string, script func(conn *websocket.Conn)) *StreamSession {
	t.Helper()
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		_, _, _ = conn.ReadMessage() // setup
		data, _ := json.Marshal(ServerMessage{SetupComplete: &SetupComplete{}})
		_ = conn.WriteMessage(websocket.TextMessage, data)
		script(conn)
	})
	t.Cleanup(server.Close)
	session, err := NewStreamSession(context.Background(), server.URL(), "test-key",
		&StreamSessionConfig{Model: model})
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// writeServer sends m to the client.
func writeServer(conn *websocket.Conn, m *ServerMessage) {
	data, _ := json.Marshal(m)
	_ = conn.WriteMessage(websocket.TextMessage, data)
}

// readUntilToolResponse reads client messages until a tool response arrives.
func readUntilToolResponse(conn *websocket.Conn) bool {
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return false
		}
		if strings.Contains(string(data), "toolResponse") || strings.Contains(string(data), "tool_response") {
			return true
		}
	}
}

// drainUntilClosed returns every chunk Response() delivers until it closes.
func drainResponseUntilClosed(t *testing.T, s *StreamSession) []providers.StreamChunk {
	t.Helper()
	var out []providers.StreamChunk
	deadline := time.After(3 * time.Second)
	for {
		select {
		case c, ok := <-s.Response():
			if !ok {
				return out
			}
			out = append(out, c)
		case <-deadline:
			t.Fatal("Response() never closed")
			return out
		}
	}
}

// On Gemini 2.5 Live a reply to a tool result can be empty: the turnComplete
// that follows the tool response carries no content. Nothing followed the
// toolCall, so the tool calls went out once the hold ran out, and the
// turnComplete is that reply's completion and ends the turn.
func TestLiveToolCallTurn_EmptyReplyAfterToolResponseEndsTheTurn(t *testing.T) {
	session := scriptedLiveServer(t, "test-live", func(conn *websocket.Conn) {
		writeServer(conn, &ServerMessage{ToolCall: &ToolCallMsg{FunctionCalls: []FunctionCall{
			{ID: "call_1", Name: "get_weather"}}}})
		if !readUntilToolResponse(conn) {
			return
		}
		writeServer(conn, &ServerMessage{ServerContent: &ServerContent{TurnComplete: true},
			UsageMetadata: &UsageMetadata{PromptTokenCount: 900, ResponseTokenCount: 0}})
		_, _, _ = conn.ReadMessage()
	})

	first := <-session.Response()
	require.NotNil(t, first.FinishReason)
	require.Equal(t, types.FinishReasonToolUse, *first.FinishReason)
	assert.Nil(t, first.CostInfo, "no usage came for the tool calls")
	require.NoError(t, session.SendToolResponses(context.Background(),
		[]providers.ToolResponse{{ToolCallID: "call_1", Result: `{"temp":21}`}}))

	select {
	case c := <-session.Response():
		require.NotNil(t, c.FinishReason, "the empty reply completes its turn")
		assert.Equal(t, finishReasonComplete, *c.FinishReason)
		require.NotNil(t, c.CostInfo)
		assert.Equal(t, 900, c.CostInfo.InputTokens)
	case <-time.After(2 * time.Second):
		t.Fatal("the empty reply's turnComplete was swallowed")
	}
}

// A tool-call turn's usage is reported on Response() before it closes when
// the session ends right after it.
func TestLiveToolCallTurn_CarriedUsageIsReportedWhenTheSessionEnds(t *testing.T) {
	session := scriptedLiveServer(t, "test-live", func(conn *websocket.Conn) {
		writeServer(conn, &ServerMessage{ToolCall: &ToolCallMsg{FunctionCalls: []FunctionCall{
			{ID: "call_1", Name: "get_weather"}}}})
		writeServer(conn, &ServerMessage{ServerContent: &ServerContent{TurnComplete: true},
			UsageMetadata: &UsageMetadata{PromptTokenCount: 600, ResponseTokenCount: 12}})
		// The server ends the session before any reply.
	})

	chunks := drainResponseUntilClosed(t, session)
	var costs []*types.CostInfo
	for _, c := range chunks {
		if c.CostInfo != nil {
			costs = append(costs, c.CostInfo)
		}
	}
	require.Len(t, costs, 1, "the held usage is reported once")
	assert.Equal(t, 600, costs[0].InputTokens)
	assert.Equal(t, 12, costs[0].OutputTokens)
	require.Len(t, finishing(chunks), 1, "only the tool-call turn completes")
}

// On Gemini 3.x Live each tool call is followed by generationComplete and a
// turnComplete with its usage, before the tool response. In a chain of tool
// calls each one's chunk carries its own usage, once, and the reply carries
// only its own. The model's name plays no part: here it is a 2.5 name.
func TestLiveToolCallTurn_ChainedToolCallsReportEachUsage(t *testing.T) {
	session := scriptedLiveServer(t, "gemini-2.5-flash-native-audio-preview-12-2025", func(conn *websocket.Conn) {
		for i, id := range []string{"call_1", "call_2"} {
			writeServer(conn, &ServerMessage{ToolCall: &ToolCallMsg{FunctionCalls: []FunctionCall{
				{ID: id, Name: "get_weather"}}}})
			writeServer(conn, &ServerMessage{ServerContent: &ServerContent{GenerationComplete: true}})
			writeServer(conn, &ServerMessage{ServerContent: &ServerContent{TurnComplete: true},
				UsageMetadata: &UsageMetadata{PromptTokenCount: 100 * (i + 1), ResponseTokenCount: 10}})
			if !readUntilToolResponse(conn) {
				return
			}
		}
		writeServer(conn, &ServerMessage{ServerContent: &ServerContent{
			ModelTurn: &ModelTurn{Parts: []Part{{Text: "It is hazy."}}}}})
		writeServer(conn, &ServerMessage{ServerContent: &ServerContent{TurnComplete: true},
			UsageMetadata: &UsageMetadata{PromptTokenCount: 400, ResponseTokenCount: 20}})
		_, _, _ = conn.ReadMessage()
	})

	ends, usage := toolChainEnds(t, session, 3)
	assert.Equal(t, types.FinishReasonToolUse, *ends[0].FinishReason)
	assert.Equal(t, types.FinishReasonToolUse, *ends[1].FinishReason)
	assert.Equal(t, finishReasonComplete, *ends[2].FinishReason)
	assert.Empty(t, usage, "no usage is reported apart from its turn's end")
	require.NotNil(t, ends[0].CostInfo)
	assert.Equal(t, 100, ends[0].CostInfo.InputTokens)
	require.NotNil(t, ends[1].CostInfo)
	assert.Equal(t, 200, ends[1].CostInfo.InputTokens)
	require.NotNil(t, ends[2].CostInfo)
	assert.Equal(t, 400, ends[2].CostInfo.InputTokens)
	assert.Equal(t, 20, ends[2].CostInfo.OutputTokens)
}

// toolChainEnds reads Response() until n turns have ended, answering each
// tool call, and returns the turn ends and the usage-only chunks.
func toolChainEnds(t *testing.T, session *StreamSession, n int) (ends, usage []providers.StreamChunk) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for len(ends) < n {
		select {
		case c := <-session.Response():
			if c.FinishReason == nil {
				if c.CostInfo != nil {
					usage = append(usage, c)
				}
				continue
			}
			ends = append(ends, c)
			if *c.FinishReason == types.FinishReasonToolUse {
				require.NoError(t, session.SendToolResponses(context.Background(),
					[]providers.ToolResponse{{ToolCallID: c.ToolCalls[0].ID, Result: `{"temp":21}`}}))
			}
		case <-deadline:
			t.Fatalf("got %d turn completions, want %d", len(ends), n)
		}
	}
	return ends, usage
}

// gatedLiveServer is scriptedLiveServer with the hold's waits set to wait and
// maxWait before the script runs.
func gatedLiveServer(t *testing.T, wait, maxWait time.Duration, script func(conn *websocket.Conn)) *StreamSession {
	t.Helper()
	gate := make(chan struct{})
	session := scriptedLiveServer(t, "test-live", func(conn *websocket.Conn) {
		<-gate
		script(conn)
	})
	setHoldWaits(session, wait, maxWait)
	close(gate)
	return session
}

// setHoldWaits sets how long session holds a toolCall's chunk.
func setHoldWaits(session *StreamSession, wait, maxWait time.Duration) {
	session.held.mu.Lock()
	defer session.held.mu.Unlock()
	session.held.wait, session.held.maxWait = wait, maxWait
}

// A generationComplete after the toolCall says its turnComplete follows: the
// tool calls wait for it past the first wait, and carry its usage.
func TestLiveToolCallTurn_GenerationCompleteHoldsTheToolCallsForTheirUsage(t *testing.T) {
	session := gatedLiveServer(t, 20*time.Millisecond, 2*time.Second, func(conn *websocket.Conn) {
		writeServer(conn, &ServerMessage{ToolCall: &ToolCallMsg{FunctionCalls: []FunctionCall{
			{ID: "call_1", Name: "get_weather"}}}})
		writeServer(conn, &ServerMessage{ServerContent: &ServerContent{GenerationComplete: true}})
		time.Sleep(60 * time.Millisecond) // past wait, within maxWait
		writeServer(conn, &ServerMessage{ServerContent: &ServerContent{TurnComplete: true},
			UsageMetadata: &UsageMetadata{PromptTokenCount: 600, ResponseTokenCount: 12}})
		_, _, _ = conn.ReadMessage()
	})

	select {
	case c := <-session.Response():
		require.NotNil(t, c.FinishReason)
		assert.Equal(t, types.FinishReasonToolUse, *c.FinishReason)
		require.NotNil(t, c.CostInfo, "the tool calls waited for their usage")
		assert.Equal(t, 600, c.CostInfo.InputTokens)
		assert.Equal(t, 12, c.CostInfo.OutputTokens)
	case <-time.After(2 * time.Second):
		t.Fatal("the tool calls were never sent")
	}
}

// When the turnComplete after a generationComplete comes once the hold ran
// out, the tool calls have gone out without it; its usage is then reported on
// its own, for the tool-call generation, and it ends no other turn.
func TestLiveToolCallTurn_TurnCompleteAfterTheHoldRanOutIsUsageOnly(t *testing.T) {
	session := gatedLiveServer(t, 10*time.Millisecond, 30*time.Millisecond, func(conn *websocket.Conn) {
		writeServer(conn, &ServerMessage{ToolCall: &ToolCallMsg{FunctionCalls: []FunctionCall{
			{ID: "call_1", Name: "get_weather"}}}})
		writeServer(conn, &ServerMessage{ServerContent: &ServerContent{GenerationComplete: true}})
		if !readUntilToolResponse(conn) {
			return
		}
		writeServer(conn, &ServerMessage{ServerContent: &ServerContent{TurnComplete: true},
			UsageMetadata: &UsageMetadata{PromptTokenCount: 600, ResponseTokenCount: 12}})
	})

	first := <-session.Response()
	require.NotNil(t, first.FinishReason)
	require.Equal(t, types.FinishReasonToolUse, *first.FinishReason)
	assert.Nil(t, first.CostInfo, "sent once the hold ran out")
	require.NoError(t, session.SendToolResponses(context.Background(),
		[]providers.ToolResponse{{ToolCallID: "call_1", Result: `{"temp":21}`}}))
	rest := drainResponseUntilClosed(t, session)
	assert.Empty(t, finishing(rest), "the tool-call generation's turnComplete ends no other turn")
	require.Len(t, rest, 1)
	require.NotNil(t, rest[0].CostInfo)
	assert.Equal(t, 600, rest[0].CostInfo.InputTokens)
	assert.Equal(t, first.Generation, rest[0].Generation, "the tool-call generation's usage")
}

// Tool calls still held when the session ends are reported before Response()
// closes, without waiting for the hold to run out.
func TestLiveToolCallTurn_HeldToolCallsAreReportedWhenTheSessionEnds(t *testing.T) {
	session := gatedLiveServer(t, time.Minute, time.Minute, func(conn *websocket.Conn) {
		writeServer(conn, &ServerMessage{ToolCall: &ToolCallMsg{FunctionCalls: []FunctionCall{
			{ID: "call_1", Name: "get_weather"}}}})
		// The server ends the session at once.
	})

	ends := finishing(drainResponseUntilClosed(t, session))
	require.Len(t, ends, 1)
	assert.Equal(t, types.FinishReasonToolUse, *ends[0].FinishReason)
	require.Len(t, ends[0].ToolCalls, 1)
	assert.Equal(t, "call_1", ends[0].ToolCalls[0].ID)
}

// writeToolCall sends a toolCall for id.
func writeToolCall(conn *websocket.Conn, id string) {
	writeServer(conn, &ServerMessage{ToolCall: &ToolCallMsg{FunctionCalls: []FunctionCall{
		{ID: id, Name: "get_weather"}}}})
}

// nextChunk returns the next chunk on Response(), failing after 2s.
func nextChunk(t *testing.T, session *StreamSession) providers.StreamChunk {
	t.Helper()
	select {
	case c := <-session.Response():
		return c
	case <-time.After(2 * time.Second):
		t.Fatal("no chunk")
		return providers.StreamChunk{}
	}
}

// answer sends the tool response for a tool-call chunk.
func answer(t *testing.T, session *StreamSession, c providers.StreamChunk) {
	t.Helper()
	require.NotEmpty(t, c.ToolCalls)
	require.NoError(t, session.SendToolResponses(context.Background(),
		[]providers.ToolResponse{{ToolCallID: c.ToolCalls[0].ID, Result: `{"temp":21}`}}))
}

// Once a held toolCall was not followed by a generationComplete, the session
// holds no more: the next toolCall is sent at once (the hold here would last a
// minute). A generationComplete that then comes after a toolCall, before the
// tool response, still marks its turnComplete as the tool-call generation's:
// its usage is reported on its own, on that generation, and ends no turn.
func TestLiveToolCallTurn_NoHoldOnceAToolCallWentWithoutGenerationComplete(t *testing.T) {
	session := gatedLiveServer(t, 30*time.Millisecond, time.Minute, func(conn *websocket.Conn) {
		writeToolCall(conn, "call_1") // nothing follows it
		if !readUntilToolResponse(conn) {
			return
		}
		writeToolCall(conn, "call_2")
		if !readUntilToolResponse(conn) {
			return
		}
		writeToolCall(conn, "call_3")
		writeServer(conn, &ServerMessage{ServerContent: &ServerContent{GenerationComplete: true}})
		writeServer(conn, &ServerMessage{ServerContent: &ServerContent{TurnComplete: true},
			UsageMetadata: &UsageMetadata{PromptTokenCount: 300, ResponseTokenCount: 3}})
		_, _, _ = conn.ReadMessage()
	})

	first := nextChunk(t, session)
	assert.Equal(t, "call_1", first.ToolCalls[0].ID)
	setHoldWaits(session, time.Minute, time.Minute) // a hold now would outlast the test
	answer(t, session, first)
	second := nextChunk(t, session)
	require.NotEmpty(t, second.ToolCalls, "sent without a hold")
	assert.Equal(t, "call_2", second.ToolCalls[0].ID)
	answer(t, session, second)
	third := nextChunk(t, session)
	require.NotEmpty(t, third.ToolCalls)
	assert.Equal(t, "call_3", third.ToolCalls[0].ID)
	assert.Nil(t, third.CostInfo, "sent without a hold, before its usage")
	usage := nextChunk(t, session)
	assert.Nil(t, usage.FinishReason, "the tool-call generation's turnComplete ends no turn")
	require.NotNil(t, usage.CostInfo)
	assert.Equal(t, 300, usage.CostInfo.InputTokens)
	assert.Equal(t, third.Generation, usage.Generation)
}

// A session whose tool calls are followed by a generationComplete keeps
// holding them: a later toolCall with nothing after it is still held, for the
// whole wait (here a minute: it goes out only when the reply begins).
func TestLiveToolCallTurn_HoldContinuesWhileToolCallsEndTheirGeneration(t *testing.T) {
	session := gatedLiveServer(t, time.Minute, time.Minute, func(conn *websocket.Conn) {
		writeToolCall(conn, "call_1")
		writeServer(conn, &ServerMessage{ServerContent: &ServerContent{GenerationComplete: true}})
		writeServer(conn, &ServerMessage{ServerContent: &ServerContent{TurnComplete: true},
			UsageMetadata: &UsageMetadata{PromptTokenCount: 100, ResponseTokenCount: 1}})
		if !readUntilToolResponse(conn) {
			return
		}
		writeToolCall(conn, "call_2")
		time.Sleep(100 * time.Millisecond)
		writeServer(conn, &ServerMessage{ServerContent: &ServerContent{
			ModelTurn: &ModelTurn{Parts: []Part{{Text: "It is hazy."}}}}})
		_, _, _ = conn.ReadMessage()
	})

	first := nextChunk(t, session)
	require.NotNil(t, first.CostInfo)
	assert.Equal(t, 100, first.CostInfo.InputTokens)
	answer(t, session, first)
	start := time.Now()
	second := nextChunk(t, session)
	assert.Equal(t, "call_2", second.ToolCalls[0].ID)
	assert.GreaterOrEqual(t, time.Since(start), 90*time.Millisecond, "held until the reply began")
	reply := nextChunk(t, session)
	assert.Equal(t, "It is hazy.", reply.Content)
}
