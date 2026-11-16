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
// that follows the tool response carries no content. 2.5 reports no
// tool-call turnComplete, so it is that reply's completion and ends the turn.
func TestLiveToolCallTurn_EmptyReplyAfterToolResponseEndsTheTurn(t *testing.T) {
	session := scriptedLiveServer(t, "gemini-2.5-flash-native-audio-preview-12-2025", func(conn *websocket.Conn) {
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

// A tool-call turn's usage still held when the session ends (no later turn
// completion came) is reported on Response() before it closes.
func TestLiveToolCallTurn_CarriedUsageIsReportedWhenTheSessionEnds(t *testing.T) {
	session := scriptedLiveServer(t, "gemini-3.8-live", func(conn *websocket.Conn) {
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

// On Gemini 3.x Live each tool call is followed by its own usage-only
// turnComplete. It is carried to the reply even when it reaches the client
// after the tool response was sent (a fast tool), here every time; a chain of
// tool calls carries each one's usage.
func TestLiveToolCallTurn_ChainedToolCallsCarryEachUsage(t *testing.T) {
	session := scriptedLiveServer(t, "gemini-3.8-live", func(conn *websocket.Conn) {
		for i, id := range []string{"call_1", "call_2"} {
			writeServer(conn, &ServerMessage{ToolCall: &ToolCallMsg{FunctionCalls: []FunctionCall{
				{ID: id, Name: "get_weather"}}}})
			if !readUntilToolResponse(conn) {
				return
			}
			writeServer(conn, &ServerMessage{ServerContent: &ServerContent{TurnComplete: true},
				UsageMetadata: &UsageMetadata{PromptTokenCount: 100 * (i + 1), ResponseTokenCount: 10}})
		}
		writeServer(conn, &ServerMessage{ServerContent: &ServerContent{
			ModelTurn: &ModelTurn{Parts: []Part{{Text: "It is hazy."}}}}})
		writeServer(conn, &ServerMessage{ServerContent: &ServerContent{TurnComplete: true},
			UsageMetadata: &UsageMetadata{PromptTokenCount: 400, ResponseTokenCount: 20}})
		_, _, _ = conn.ReadMessage()
	})

	var ends []providers.StreamChunk
	deadline := time.After(3 * time.Second)
	for len(ends) < 3 {
		select {
		case c := <-session.Response():
			if c.FinishReason == nil {
				continue
			}
			ends = append(ends, c)
			if *c.FinishReason == types.FinishReasonToolUse {
				require.NoError(t, session.SendToolResponses(context.Background(),
					[]providers.ToolResponse{{ToolCallID: c.ToolCalls[0].ID, Result: `{"temp":21}`}}))
			}
		case <-deadline:
			t.Fatalf("got %d turn completions, want 3", len(ends))
		}
	}
	assert.Equal(t, types.FinishReasonToolUse, *ends[0].FinishReason)
	assert.Equal(t, types.FinishReasonToolUse, *ends[1].FinishReason)
	assert.Equal(t, finishReasonComplete, *ends[2].FinishReason)
	require.NotNil(t, ends[2].CostInfo)
	assert.Equal(t, 700, ends[2].CostInfo.InputTokens, "the reply carries both tool-call turns' usage")
	assert.Equal(t, 40, ends[2].CostInfo.OutputTokens)
}

// Which Live models end a tool-call turn with their own turnComplete.
func TestReportsToolCallTurnComplete(t *testing.T) {
	for model, want := range map[string]bool{
		"gemini-3.8-live": true,
		"gemini-2.5-flash-native-audio-preview-12-2025": false,
		"gemini-live-2.5-flash-preview":                 false,
		"gemini-2.0-flash-live-001":                     false,
		"gemini-10.0-live":                              true,
		"gemini-live":                                   true,
	} {
		assert.Equal(t, want, reportsToolCallTurnComplete(model), model)
	}
}
