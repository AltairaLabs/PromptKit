package gemini

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
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

// testClock is a clock a test moves by hand.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// withClock gives s a clock the test moves.
func withClock(s *StreamSession) *testClock {
	c := newTestClock()
	s.now = c.now
	return c
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

// usageOnlyChunks returns the chunks that carry a cost and end no turn.
func usageOnlyChunks(chunks []providers.StreamChunk) []providers.StreamChunk {
	var out []providers.StreamChunk
	for _, c := range chunks {
		if c.FinishReason == nil && c.CostInfo != nil {
			out = append(out, c)
		}
	}
	return out
}

// Gemini 3.x Live answers a tool call with toolCall, generationComplete, then
// a turnComplete carrying only the usage of the tool-call generation (observed
// live on gemini-3.8-live). The tool calls go out at once. That turnComplete
// is not a model turn: reported as one, it reads as an empty reply after
// every tool call. Its usage is reported on its own, for the tool-call
// generation, and the reply carries only its own. Read before the tool
// response is sent, the generation end is the tool call's however late it is
// read: the reply cannot begin before the server has the response.
func TestLiveToolCallTurn_TurnCompleteAfterToolCallIsTheToolGenerationsUsage(t *testing.T) {
	s := newProtocolTestSession(t)
	clock := withClock(s)
	require.NoError(t, s.processServerMessage(toolCall("call_1", "Paris")))
	clock.advance(5 * time.Second) // a stalled read: no window applies before the response
	require.NoError(t, s.processServerMessage(generationComplete()))
	clock.advance(5 * time.Second)
	require.NoError(t, s.processServerMessage(turnComplete(textUsage(600, 12))))
	for _, m := range []*ServerMessage{
		{ServerContent: &ServerContent{ModelTurn: &ModelTurn{Parts: []Part{{Text: "It is hazy."}}}}},
		generationComplete(),
		turnComplete(textUsage(700, 20)),
	} {
		require.NoError(t, s.processServerMessage(m))
	}

	chunks := drainChunks(t, s)
	ends := finishing(chunks)
	require.Len(t, ends, 2, "a tool-call turn and its reply are two turns")
	assert.Equal(t, types.FinishReasonToolUse, *ends[0].FinishReason)
	require.Len(t, ends[0].ToolCalls, 1)
	assert.Equal(t, "call_1", ends[0].ToolCalls[0].ID)
	assert.Nil(t, ends[0].CostInfo, "sent at once, before its usage")
	assert.Equal(t, finishReasonComplete, *ends[1].FinishReason)
	assert.Equal(t, ends[0].Generation+1, ends[1].Generation, "the reply is a generation of its own")
	usage := usageOnlyChunks(chunks)
	require.Len(t, usage, 1)
	assert.Equal(t, 600, usage[0].CostInfo.InputTokens)
	assert.Equal(t, 12, usage[0].CostInfo.OutputTokens)
	assert.Equal(t, ends[0].Generation, usage[0].Generation, "the tool-call generation's usage")
	require.NotNil(t, ends[1].CostInfo)
	assert.Equal(t, 700, ends[1].CostInfo.InputTokens)
	assert.InDelta(t, priced(t, textUsage(700, 20)).TotalCost, ends[1].CostInfo.TotalCost, 1e-12)
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

	chunks := drainChunks(t, s)
	ends := finishing(chunks)
	require.Len(t, ends, 2)
	assert.Equal(t, types.FinishReasonToolUse, *ends[0].FinishReason)
	assert.Equal(t, finishReasonComplete, *ends[1].FinishReason)
	require.NotNil(t, ends[1].CostInfo)
	assert.Equal(t, 900, ends[1].CostInfo.InputTokens)
	assert.Equal(t, 40, ends[1].CostInfo.OutputTokens)
	assert.Empty(t, usageOnlyChunks(chunks))
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

// On Gemini 2.5 Live a reply to a tool result can be empty: a content-free
// generation end read after the tool response was sent. Unless a
// generationComplete came within toolGenEndWindow of sending the response, it
// is the reply's completion: it ends the turn, as a generation of its own.
func TestLiveToolCallTurn_EmptyReplyAfterToolResponseEndsTheTurn(t *testing.T) {
	for name, script := range map[string][]*ServerMessage{
		// The model ran on the response before its generationComplete.
		"generationComplete after the window": {generationComplete(), turnComplete(textUsage(900, 0))},
		// No generationComplete: a turnComplete alone, however soon, is the
		// reply's once the response is sent.
		"turnComplete alone": {turnComplete(textUsage(900, 0))},
	} {
		t.Run(name, func(t *testing.T) {
			clock := newTestClock()
			session := scriptedLiveServerOnClock(t, "test-live", clock, func(conn *websocket.Conn) {
				writeToolCall(conn, "call_1")
				if !readUntilToolResponse(conn) {
					return
				}
				if len(script) > 1 {
					clock.advance(toolGenEndWindow) // the response was sent before this
				}
				for _, m := range script {
					writeServer(conn, m)
				}
				_, _, _ = conn.ReadMessage()
			})

			ends, usage := toolChainEnds(t, session, 2)
			assert.Equal(t, types.FinishReasonToolUse, *ends[0].FinishReason)
			assert.Equal(t, finishReasonComplete, *ends[1].FinishReason, "the empty reply completes its turn")
			assert.Equal(t, ends[0].Generation+1, ends[1].Generation, "the reply is a generation of its own")
			require.NotNil(t, ends[1].CostInfo)
			assert.Equal(t, 900, ends[1].CostInfo.InputTokens)
			assert.Empty(t, usage)
		})
	}
}

// A toolCall whose calls are answered one at a time is answered from the
// first response: a generationComplete read toolGenEndWindow after it is the
// reply's, however recent the last response.
func TestLiveToolCallTurn_ParallelCallsAreAnsweredFromTheFirstResponse(t *testing.T) {
	clock := newTestClock()
	session := scriptedLiveServerOnClock(t, "test-live", clock, func(conn *websocket.Conn) {
		writeServer(conn, &ServerMessage{ToolCall: &ToolCallMsg{FunctionCalls: []FunctionCall{
			{ID: "call_1", Name: "get_weather"}, {ID: "call_2", Name: "get_weather"}}}})
		for range 2 {
			if !readUntilToolResponse(conn) {
				return
			}
		}
		writeServer(conn, generationComplete())
		writeServer(conn, turnComplete(textUsage(900, 0)))
		_, _, _ = conn.ReadMessage()
	})

	var ends, usage []providers.StreamChunk
	deadline := time.After(3 * time.Second)
	for len(ends) < 2 {
		select {
		case c := <-session.Response():
			switch {
			case c.FinishReason != nil:
				ends = append(ends, c)
				if len(c.ToolCalls) == 2 {
					for i, tc := range c.ToolCalls {
						if i > 0 {
							clock.advance(toolGenEndWindow)
						}
						require.NoError(t, session.SendToolResponses(context.Background(),
							[]providers.ToolResponse{{ToolCallID: tc.ID, Result: `{}`}}))
					}
				}
			case c.CostInfo != nil:
				usage = append(usage, c)
			}
		case <-deadline:
			t.Fatalf("got %d turn completions, want 2", len(ends))
		}
	}
	assert.Equal(t, finishReasonComplete, *ends[1].FinishReason, "the empty reply completes its turn")
	assert.Empty(t, usage)
}

// A response to a call of an earlier toolCall does not answer the last one:
// a generation end read then is still the last tool-call generation's.
func TestLiveToolCallTurn_AResponseToAnotherCallDoesNotAnswerTheToolCall(t *testing.T) {
	clock := newTestClock()
	session := scriptedLiveServerOnClock(t, "test-live", clock, func(conn *websocket.Conn) {
		writeToolCall(conn, "call_2")
		if !readUntilToolResponse(conn) {
			return
		}
		clock.advance(time.Minute)
		writeServer(conn, turnComplete(textUsage(600, 12)))
		_, _, _ = conn.ReadMessage()
	})

	var usage []providers.StreamChunk
	deadline := time.After(3 * time.Second)
	for len(usage) == 0 {
		select {
		case c := <-session.Response():
			if c.FinishReason != nil {
				require.Equal(t, types.FinishReasonToolUse, *c.FinishReason, "no turn but the tool call's ends")
				require.NoError(t, session.SendToolResponses(context.Background(),
					[]providers.ToolResponse{{ToolCallID: "call_1", Result: `{}`}}))
			} else if c.CostInfo != nil {
				usage = append(usage, c)
			}
		case <-deadline:
			t.Fatal("the tool-call generation's usage was never reported")
		}
	}
	assert.Equal(t, 600, usage[0].CostInfo.InputTokens)
}

// The tool-call generation's generationComplete, read before the tool
// response was sent, can be followed by its turnComplete read after it and
// much later: the turnComplete is still that generation's.
func TestLiveToolCallTurn_LateTurnCompleteAfterAnEarlyGenerationComplete(t *testing.T) {
	s := newProtocolTestSession(t)
	clock := withClock(s)
	require.NoError(t, s.processServerMessage(toolCall("call_1", "Paris")))
	require.NoError(t, s.processServerMessage(generationComplete()))
	s.noteToolResponse([]providers.ToolResponse{{ToolCallID: "call_1"}})
	clock.advance(2 * time.Second)
	require.NoError(t, s.processServerMessage(turnComplete(textUsage(600, 12))))

	chunks := drainChunks(t, s)
	require.Len(t, finishing(chunks), 1, "only the tool-call turn ends")
	usage := usageOnlyChunks(chunks)
	require.Len(t, usage, 1)
	assert.Equal(t, 600, usage[0].CostInfo.InputTokens)
}

// Review case 1: a message landing between the toolCall and its
// generationComplete (the caller's transcribed speech, a second toolCall)
// and a tool response sent before the client reads the generationComplete
// change nothing: the tool-call generation's end is still its own, and no
// empty turn is reported.
func TestLiveToolCallTurn_MessagesBeforeTheGenerationCompleteChangeNothing(t *testing.T) {
	t.Run("transcribed speech in between", func(t *testing.T) {
		s := newProtocolTestSession(t)
		for _, m := range []*ServerMessage{
			toolCall("call_1", "Paris"),
			{ServerContent: &ServerContent{InputTranscription: &Transcription{Text: "and Rome?"}}},
			generationComplete(),
			turnComplete(textUsage(600, 12)),
		} {
			require.NoError(t, s.processServerMessage(m))
		}
		chunks := drainChunks(t, s)
		require.Len(t, finishing(chunks), 1, "no empty turn")
		require.Len(t, usageOnlyChunks(chunks), 1)
		assert.Equal(t, 600, usageOnlyChunks(chunks)[0].CostInfo.InputTokens)
	})
	t.Run("a second toolCall in between", func(t *testing.T) {
		s := newProtocolTestSession(t)
		for _, m := range []*ServerMessage{
			toolCall("call_1", "Paris"), toolCall("call_2", "Rome"),
			generationComplete(), turnComplete(textUsage(600, 12)),
		} {
			require.NoError(t, s.processServerMessage(m))
		}
		chunks := drainChunks(t, s)
		ends := finishing(chunks)
		require.Len(t, ends, 2, "each toolCall, and no empty turn")
		usage := usageOnlyChunks(chunks)
		require.Len(t, usage, 1)
		assert.Equal(t, ends[1].Generation, usage[0].Generation)
	})
	t.Run("a fast tool response before the generationComplete is read", func(t *testing.T) {
		// The clock stands still: the generationComplete is read within
		// toolGenEndWindow of the response, whatever the runner's load.
		session := scriptedLiveServerOnClock(t, "test-live", newTestClock(), func(conn *websocket.Conn) {
			writeToolCall(conn, "call_1")
			if !readUntilToolResponse(conn) {
				return
			}
			// Sent by the server before it had the response; read after it.
			writeServer(conn, generationComplete())
			writeServer(conn, turnComplete(textUsage(600, 12)))
			writeServer(conn, &ServerMessage{ServerContent: &ServerContent{
				ModelTurn: &ModelTurn{Parts: []Part{{Text: "It is hazy."}}}}})
			writeServer(conn, turnComplete(textUsage(700, 20)))
			_, _, _ = conn.ReadMessage()
		})
		ends, usage := toolChainEnds(t, session, 2)
		assert.Equal(t, types.FinishReasonToolUse, *ends[0].FinishReason)
		assert.Equal(t, finishReasonComplete, *ends[1].FinishReason)
		require.NotNil(t, ends[1].CostInfo)
		assert.Equal(t, 700, ends[1].CostInfo.InputTokens, "the reply carries only its own usage")
		require.Len(t, usage, 1)
		assert.Equal(t, 600, usage[0].CostInfo.InputTokens)
	})
}

// Review case 2: a generationComplete that also carries the caller's
// transcribed speech still ends the tool-call generation; the transcript is
// delivered, and the usage is not lost.
func TestLiveToolCallTurn_GenerationCompleteWithATranscript(t *testing.T) {
	s := newProtocolTestSession(t)
	clock := withClock(s)
	require.NoError(t, s.processServerMessage(toolCall("call_1", "Paris")))
	require.NoError(t, s.processServerMessage(&ServerMessage{ServerContent: &ServerContent{
		GenerationComplete: true, InputTranscription: &Transcription{Text: "thanks"}}}))
	clock.advance(time.Second) // the turnComplete comes later than the window
	require.NoError(t, s.processServerMessage(turnComplete(textUsage(600, 12))))

	chunks := drainChunks(t, s)
	require.Len(t, finishing(chunks), 1, "no empty turn")
	usage := usageOnlyChunks(chunks)
	require.Len(t, usage, 1)
	assert.Equal(t, 600, usage[0].CostInfo.InputTokens)
	var heard string
	for _, c := range chunks {
		if c.Metadata[metaKeyType] == "input_transcription" {
			heard += c.Metadata["transcription"].(string)
		}
	}
	assert.Equal(t, "thanks", heard)
}

// Review case 3: usage held from before the toolCall (the generation had
// spoken first) goes on the tool calls, not on the reply's turnComplete.
func TestLiveToolCallTurn_UsageBeforeTheToolCallGoesOnTheToolCalls(t *testing.T) {
	s := newProtocolTestSession(t)
	for _, m := range []*ServerMessage{
		said("Let me check."), {UsageMetadata: textUsage(40, 3)},
		toolCall("call_1", "Paris"), generationComplete(), turnComplete(textUsage(600, 12)),
		said("It is hazy."), turnComplete(textUsage(700, 20)),
	} {
		require.NoError(t, s.processServerMessage(m))
	}

	chunks := drainChunks(t, s)
	assertCosts(t, chunks, [3]int{40, 3, 1}, [3]int{600, 12, 0}, [3]int{700, 20, 1})
}

// Review case 4: tool calls are sent the moment they are read, so a session
// that ends right after a toolCall has reported them.
func TestLiveToolCallTurn_ToolCallsAreReportedWhenTheSessionEnds(t *testing.T) {
	session := scriptedLiveServer(t, "test-live", func(conn *websocket.Conn) {
		writeToolCall(conn, "call_1")
		writeServer(conn, turnComplete(textUsage(600, 12)))
		// The server ends the session at once.
	})

	chunks := drainResponseUntilClosed(t, session)
	ends := finishing(chunks)
	require.Len(t, ends, 1, "only the tool-call turn completes")
	assert.Equal(t, "call_1", ends[0].ToolCalls[0].ID)
	usage := usageOnlyChunks(chunks)
	require.Len(t, usage, 1, "the usage is reported once")
	assert.Equal(t, 600, usage[0].CostInfo.InputTokens)
}

// On Gemini 3.x Live each tool call is followed by its own generation end,
// here read after the tool response was sent. In a chain of tool calls each
// one's usage is reported once, for its own generation; the reply carries
// only its own. The model's name plays no part: here it is a 2.5 name.
func TestLiveToolCallTurn_ChainedToolCallsReportEachUsage(t *testing.T) {
	session := scriptedLiveServerOnClock(t, "gemini-2.5-flash-native-audio-preview-12-2025", newTestClock(),
		func(conn *websocket.Conn) {
			for i, id := range []string{"call_1", "call_2"} {
				writeToolCall(conn, id)
				writeServer(conn, generationComplete())
				writeServer(conn, turnComplete(textUsage(100*(i+1), 10)))
				if !readUntilToolResponse(conn) {
					return
				}
			}
			writeServer(conn, &ServerMessage{ServerContent: &ServerContent{
				ModelTurn: &ModelTurn{Parts: []Part{{Text: "It is hazy."}}}}})
			writeServer(conn, turnComplete(textUsage(400, 20)))
			_, _, _ = conn.ReadMessage()
		})

	ends, usage := toolChainEnds(t, session, 3)
	assert.Equal(t, types.FinishReasonToolUse, *ends[0].FinishReason)
	assert.Equal(t, types.FinishReasonToolUse, *ends[1].FinishReason)
	assert.Equal(t, finishReasonComplete, *ends[2].FinishReason)
	require.Len(t, usage, 2)
	assert.Equal(t, 100, usage[0].CostInfo.InputTokens)
	assert.Equal(t, ends[0].Generation, usage[0].Generation)
	assert.Equal(t, 200, usage[1].CostInfo.InputTokens)
	assert.Equal(t, ends[1].Generation, usage[1].Generation)
	require.NotNil(t, ends[2].CostInfo)
	assert.Equal(t, 400, ends[2].CostInfo.InputTokens)
}

// scriptedLiveServer is a fake Live server: it completes setup, then runs
// script on the connection.
func scriptedLiveServer(t *testing.T, model string, script func(conn *websocket.Conn)) *StreamSession {
	t.Helper()
	return scriptedLiveServerOnClock(t, model, nil, script)
}

// newTestClock is a clock a test moves by hand, standing still otherwise.
func newTestClock() *testClock {
	return &testClock{t: time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)}
}

// scriptedLiveServerOnClock is scriptedLiveServer with a session on clock
// (nil: the wall clock).
func scriptedLiveServerOnClock(
	t *testing.T, model string, clock *testClock, script func(conn *websocket.Conn),
) *StreamSession {
	t.Helper()
	config := &StreamSessionConfig{Model: model}
	if clock != nil {
		config.now = clock.now
	}
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		_, _, _ = conn.ReadMessage() // setup
		data, _ := json.Marshal(ServerMessage{SetupComplete: &SetupComplete{}})
		_ = conn.WriteMessage(websocket.TextMessage, data)
		script(conn)
	})
	t.Cleanup(server.Close)
	session, err := NewStreamSession(context.Background(), server.URL(), "test-key", config)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// writeServer sends m to the client.
func writeServer(conn *websocket.Conn, m *ServerMessage) {
	data, _ := json.Marshal(m)
	_ = conn.WriteMessage(websocket.TextMessage, data)
}

// writeToolCall sends a toolCall for id.
func writeToolCall(conn *websocket.Conn, id string) {
	writeServer(conn, &ServerMessage{ToolCall: &ToolCallMsg{FunctionCalls: []FunctionCall{
		{ID: id, Name: "get_weather"}}}})
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

// drainResponseUntilClosed returns every chunk Response() delivers until it
// closes.
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

// toolChainEnds reads Response() until n turns have ended, answering each
// tool call at once, and returns the turn ends and the usage-only chunks.
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
