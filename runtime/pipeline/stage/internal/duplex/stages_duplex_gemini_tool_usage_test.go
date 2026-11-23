package duplex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/gemini"
	providersmock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// The stage on a real Gemini Live session, against a server that plays the
// message order Gemini Live was seen sending (#2761). Usage is priced at $1
// per 1K tokens each way, so a call's cost is (in+out)/1000, as usd does.

// Server messages, as Gemini Live sends them.
const (
	liveToolCall           = `{"toolCall":{"functionCalls":[{"id":"c1","name":"weather","args":{"city":"Paris"}}]}}`
	liveGenerationComplete = `{"serverContent":{"generationComplete":true}}`
	liveReply              = `{"serverContent":{"modelTurn":{"parts":[{"text":"It is 21 degrees"}]}}}`
)

// liveTurnComplete is a turnComplete carrying usage of in prompt and out
// response tokens.
func liveTurnComplete(in, out int) string {
	b, _ := json.Marshal(map[string]any{
		"serverContent": map[string]any{"turnComplete": true},
		"usageMetadata": map[string]any{"promptTokenCount": in, "responseTokenCount": out},
	})
	return string(b)
}

// geminiLiveProvider creates real Gemini Live sessions on a scripted server.
type geminiLiveProvider struct {
	*providersmock.StreamingProvider
	url string
}

// ReportsUsageAfterCallEnds declares, as the Gemini provider does, that
// usage can arrive after a call ends (providers.LateUsageReporter).
func (*geminiLiveProvider) ReportsUsageAfterCallEnds() bool { return true }

func (p *geminiLiveProvider) CreateStreamSession(
	ctx context.Context, _ *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	return gemini.NewStreamSession(ctx, p.url, "test-key", &gemini.StreamSessionConfig{
		ResponseModalities: []string{"TEXT"},
		InputCostPer1K:     1,
		OutputCostPer1K:    1,
	})
}

// scriptedGeminiLive serves one Live session: it completes the setup, then
// runs script on the connection, then closes it.
func scriptedGeminiLive(t *testing.T, script func(conn *websocket.Conn)) string {
	t.Helper()
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, _, _ = conn.ReadMessage() // setup
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"setupComplete":{}}`))
		script(conn)
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// send writes server messages in order.
func send(conn *websocket.Conn, msgs ...string) {
	for _, m := range msgs {
		_ = conn.WriteMessage(websocket.TextMessage, []byte(m))
	}
}

// awaitToolResponse reads client messages until a tool response, and reports
// whether one came.
func awaitToolResponse(conn *websocket.Conn) bool {
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return false
		}
		if strings.Contains(string(data), `"toolResponse"`) {
			return true
		}
	}
}

// runGeminiLive runs the stage, with the weather tool, on a Gemini Live
// session the server plays script on, until the server ends it. A completion
// with no cost waits for its call's usage until the usage arrives, a
// completion after it goes out, or the session ends. With completedFirst
// set, the tool runs (and its response is sent) only once completedFirst
// provider.call.completed events were reported: the session has then read the
// usage they waited for before the tool response went out.
func runGeminiLive(t *testing.T, completedFirst int, script func(conn *websocket.Conn)) (*callEvents, []core.StreamElement) {
	t.Helper()
	emitter, ev := newCallEvents()
	p := &geminiLiveProvider{
		StreamingProvider: providersmock.NewStreamingProvider("gemini", "live", false),
		url:               scriptedGeminiLive(t, script),
	}
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`)}
	if completedFirst > 0 {
		exec.gate = make(chan struct{})
		go func() {
			defer close(exec.gate)
			for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
				ev.mu.Lock()
				n := len(ev.done)
				ev.mu.Unlock()
				if n >= completedFirst {
					return
				}
				time.Sleep(time.Millisecond)
			}
		}()
	}
	st := NewDuplexProviderStageWithTurnState(p, baseConfig(), emitter, &core.TurnState{AllowedTools: []string{"weather"}})
	st.SetToolExecution(newWeatherRegistry(t, exec), nil, DuplexToolConfig{}, nil)
	st.usageWait = time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	input := make(chan core.StreamElement, 1)
	output := make(chan core.StreamElement, 256)
	input <- core.StreamElement{EndOfStream: true}
	done := make(chan error, 1)
	go func() { done <- st.Process(ctx, input, output) }()
	elems := drainUntilClosed(t, output)
	require.NoError(t, <-done)
	assert.Equal(t, []string{"weather"}, exec.calls(), "the tool ran")
	return ev, elems
}

// Gemini Live (3.x) ends the tool-call generation with its own turnComplete,
// carrying that generation's usage, right after the toolCall. The tool calls
// end the call at once, before that usage arrives: the call's completion
// waits for it and carries it, never as an empty reply's call and never on
// the reply's, and no provider.call.usage follows. The tool runs only once
// that completion was reported, so the session read the generation's end
// before it sent the tool response: no clock decides it.
func TestDuplexGeminiLive_ToolCallTurnUsageIsReportedOnItsCall(t *testing.T) {
	ev, elems := runGeminiLive(t, 1, func(conn *websocket.Conn) {
		send(conn, liveToolCall, liveGenerationComplete, liveTurnComplete(935, 6))
		if !awaitToolResponse(conn) {
			return
		}
		send(conn, liveReply, liveGenerationComplete, liveTurnComplete(1019, 74))
	})
	done, usage := ev.await(t, 2, 0)

	require.Len(t, done, 2, "the tool-call call and the reply's; no empty call")
	assert.Equal(t, 1, done[0].ToolCallCount)
	assertCall(t, done[0], 935, 6)
	assertCall(t, done[1], 1019, 74)
	assert.Empty(t, usage, "the completion carried the usage")
	msgs := costedReplies(elems)
	require.Len(t, msgs, 2)
	assert.Equal(t, "It is 21 degrees", msgs[1].Content)
	assertCostInfo(t, usd(935+1019, 6+74), sumCosts(msgs))
}

// Gemini Live (2.5) sends nothing after the toolCall: one turnComplete after
// the reply carries the usage of the whole server turn. The tool-call call
// completes with no cost once the reply begins, and the reply's carries it.
func TestDuplexGeminiLive_ToolCallWithoutItsOwnTurnComplete(t *testing.T) {
	ev, elems := runGeminiLive(t, 0, func(conn *websocket.Conn) {
		send(conn, liveToolCall)
		if !awaitToolResponse(conn) {
			return
		}
		send(conn, liveReply, liveGenerationComplete, liveTurnComplete(1169, 155))
	})
	done, usage := ev.await(t, 2, 0)

	require.Len(t, done, 2)
	assert.Equal(t, 1, done[0].ToolCallCount)
	assertCall(t, done[0], 0, 0)
	assertCall(t, done[1], 1169, 155)
	assert.Empty(t, usage)
	msgs := costedReplies(elems)
	require.Len(t, msgs, 2)
	assert.Nil(t, msgs[0].CostInfo)
	assertCostInfo(t, usd(1169, 155), msgs[1].CostInfo)
}

// Gemini Live (2.5) can answer a tool result with an empty reply: a lone
// turnComplete after the tool response. It ends the reply's turn, on a call
// of its own that carries its usage.
func TestDuplexGeminiLive_EmptyReplyToTheToolResultEndsItsTurn(t *testing.T) {
	ev, elems := runGeminiLive(t, 0, func(conn *websocket.Conn) {
		send(conn, liveToolCall)
		if !awaitToolResponse(conn) {
			return
		}
		send(conn, liveTurnComplete(1169, 0))
	})
	done, usage := ev.await(t, 2, 0)

	require.Len(t, done, 2, "the tool-call call and the empty reply's")
	assert.Equal(t, 1, done[0].ToolCallCount)
	assertCall(t, done[0], 0, 0)
	assertCall(t, done[1], 1169, 0)
	assert.Empty(t, usage)
	assert.Equal(t, []string{"", ""}, endedTurnContents(elems), "the tool-call turn and the empty reply both end")
}

// A content-free turn end on the generation of a call that already completed
// (a provider reporting a tool-call generation's end, with its usage, as a
// turn end) ends no turn: it is that call's usage, which its completion,
// waiting for it, carries. No empty assistant message reaches the caller, and
// the reply that follows is its own call.
func TestDuplexGeminiLive_ContentlessEndOfACompletedCallIsItsUsage(t *testing.T) {
	h, ev := costHarnessWaiting(t, time.Minute)
	h.emit(gen(1, toolCallTurn(weatherCall("c1"))))
	h.awaitSent(1)
	stop := "stop"
	h.emit(gen(1, []providers.StreamChunk{{FinishReason: &stop, CostInfo: usd(935, 6)}}))
	h.emit(gen(2, textTurn("It is 21 degrees", usd(1019, 74))))
	done, usage := ev.await(t, 2, 0)
	elems := h.finish()

	require.Len(t, done, 2, "the tool-call call and the reply's; no empty call")
	assert.Equal(t, 1, done[0].ToolCallCount)
	assertCall(t, done[0], 935, 6)
	assertCall(t, done[1], 1019, 74)
	assert.Empty(t, usage)
	assert.Empty(t, streamErrors(elems))
	assert.Equal(t, []string{"", "It is 21 degrees"}, endedTurnContents(elems),
		"the tool-call turn and the reply: no empty reply between them")
}

// endedTurnContents returns the content of each assistant message that ends
// a turn.
func endedTurnContents(elems []core.StreamElement) []string {
	var out []string
	for _, e := range elems {
		if e.EndOfStream && e.Message != nil && e.Message.Role == core.RoleAssistant {
			out = append(out, e.Message.Content)
		}
	}
	return out
}
