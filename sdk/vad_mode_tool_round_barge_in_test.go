package sdk

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// toolRoundWire is an OpenAI Chat Completions endpoint that, like OpenAI,
// rejects a request whose tool result answers no tool call before it (400).
// Its first call speaks and calls get_weather; its second, after the tool's
// result, starts an answer and holds the stream open until the caller goes
// away; later calls answer at once.
type toolRoundWire struct {
	*httptest.Server
	// silentSecondRound makes the second call hold the stream open without
	// sending anything: the model has not begun its answer when the caller
	// speaks.
	silentSecondRound bool
	mu                sync.Mutex
	calls             int
	rejected          []string
	bodies            []map[string]any
}

func newToolRoundWire(t *testing.T) *toolRoundWire {
	t.Helper()
	w := &toolRoundWire{}
	w.Server = httptest.NewServer(http.HandlerFunc(w.serve))
	t.Cleanup(w.Close)
	return w
}

func (w *toolRoundWire) serve(rw http.ResponseWriter, r *http.Request) {
	var body map[string]any
	raw, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(raw, &body)
	w.mu.Lock()
	w.calls++
	call := w.calls
	w.bodies = append(w.bodies, body)
	if problem := orphanToolResult(body); problem != "" {
		w.rejected = append(w.rejected, problem)
		w.mu.Unlock()
		rw.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(rw, `{"error":{"message":"`+problem+`","type":"invalid_request_error"}}`)
		return
	}
	w.mu.Unlock()
	rw.Header().Set("Content-Type", "text/event-stream")
	flush := func(s string) {
		_, _ = io.WriteString(rw, s)
		if f, ok := rw.(http.Flusher); ok {
			f.Flush()
		}
	}
	switch call {
	case 1:
		flush(`data: {"id":"1","choices":[{"index":0,"delta":{"role":"assistant","content":"Let me look that up. "}}]}` + "\n\n" +
			`data: {"id":"1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function",` +
			`"function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}]}}]}` + "\n\n" +
			`data: {"id":"1","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n" +
			"data: [DONE]\n\n")
	case 2:
		if !w.silentSecondRound {
			flush(`data: {"id":"2","choices":[{"index":0,"delta":{"role":"assistant","content":"It is "}}]}` + "\n\n")
		}
		select { // a long answer, still being written when the caller speaks
		case <-r.Context().Done():
		case <-time.After(streamingSpeechBound):
		}
	default:
		flush(`data: {"id":"3","choices":[{"index":0,"delta":{"role":"assistant","content":"Sure."}}]}` + "\n\n" +
			`data: {"id":"3","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n" +
			"data: [DONE]\n\n")
	}
}

// orphanToolResult names a tool message that answers no tool call made
// before it in the request, as OpenAI reports it; "" when there is none.
func orphanToolResult(body map[string]any) string {
	called := map[string]bool{}
	msgs, _ := body["messages"].([]any)
	for _, m := range msgs {
		msg, _ := m.(map[string]any)
		switch msg["role"] {
		case "assistant":
			calls, _ := msg["tool_calls"].([]any)
			for _, c := range calls {
				call, _ := c.(map[string]any)
				id, _ := call["id"].(string)
				called[id] = true
			}
		case "tool":
			if id, _ := msg["tool_call_id"].(string); !called[id] {
				return "tool message " + id + " answers no tool call"
			}
		}
	}
	return ""
}

func (w *toolRoundWire) snapshot() (calls int, rejected []string, last map[string]any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.bodies) > 0 {
		last = w.bodies[len(w.bodies)-1]
	}
	return w.calls, append([]string(nil), w.rejected...), last
}

const toolRoundPack = `{"id":"tool-round","version":"1.0.0",
	"prompts":{"main":{"id":"main","name":"Main","version":"1.0.0",
		"system_template":"You are a voice assistant.","tools":["get_weather"]}},
	"tools":{"get_weather":{"name":"get_weather","description":"The weather in a city.",
		"parameters":{"type":"object","properties":{"city":{"type":"string"}}}}}}`

// A caller who barges in on a round that spoke and called a tool leaves the
// call and its result a pair: the model's next request (a real OpenAI
// request, which OpenAI rejects with an orphaned result) carries both, with
// the round's text cut to what was heard, and so does the stored transcript.
func TestVADMode_BargeInOnAToolRoundKeepsTheCallAndItsResult(t *testing.T) {
	if testing.Short() {
		t.Skip("drives real VAD turns")
	}
	t.Setenv("OPENAI_API_KEY", "test-key")
	wire := newToolRoundWire(t)
	ttsSvc := newSegmentTTS()
	ttsSvc.blockFirst = true // the caller speaks over "Let me look that up."
	store := statestore.NewMemoryStore()
	sttSvc := newScriptedSTT("weather in paris", "stop")
	conv, err := OpenDuplex(createTestPackFile(t, toolRoundPack), "main",
		WithLLMProvider(ProviderSpec{ID: "agent", Type: "openai", Model: "gpt-4o-mini", BaseURL: wire.URL}),
		WithSkipSchemaValidation(), WithStateStore(store), WithVADMode(sttSvc, ttsSvc, vadTestConfig()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = closeWithin(t, conv) })
	conv.OnTool("get_weather", func(map[string]any) (any, error) { return map[string]any{"sky": "sunny"}, nil })
	responses, err := conv.Response()
	require.NoError(t, err)
	rec := &speechRecorder{}
	go rec.consume(responses)
	convID := conv.ID()

	speak(t, conv)
	awaitClosed(t, ttsSvc.first, "the round's sentence was never synthesized")
	require.True(t, waitFor(func() bool { calls, _, _ := wire.snapshot(); return calls >= 2 }, streamingSpeechBound),
		"the tool's result never went back to the model")

	speak(t, conv) // talks over "Let me look that up."
	require.True(t, waitFor(func() bool { calls, _, _ := wire.snapshot(); return calls >= 3 }, streamingSpeechBound),
		"the caller's words never reached the model")
	require.True(t, waitFor(func() bool { return rec.turnCompletes() >= 2 }, streamingSpeechBound),
		"the next turn's reply never completed")

	_, rejected, next := wire.snapshot()
	assert.Empty(t, rejected, "the model's next request is one OpenAI accepts")
	var roles []string
	for _, m := range next["messages"].([]any) {
		msg := m.(map[string]any)
		role, _ := msg["role"].(string)
		if calls, ok := msg["tool_calls"].([]any); ok && len(calls) > 0 {
			role += "+call"
		}
		roles = append(roles, role)
	}
	assert.Equal(t, []string{"system", "user", "assistant+call", "tool", "user"}, roles,
		"the call and its result go back together; the unheard answer does not")

	require.NoError(t, closeWithin(t, conv))
	state, err := store.Load(t.Context(), convID)
	require.NoError(t, err)
	var stored []string
	for _, m := range state.Messages {
		entry := m.Role
		if len(m.ToolCalls) > 0 {
			entry += "+call:" + m.ToolCalls[0].ID + ":" + m.Content
		}
		if m.ToolResult != nil {
			entry += ":" + m.ToolResult.ID
		}
		stored = append(stored, entry)
	}
	require.GreaterOrEqual(t, len(stored), 4)
	assert.Equal(t, []string{"user", "assistant+call:call_1:", "tool:call_1"}, stored[:3],
		"the transcript keeps the call and its result, the round's text cut to what was heard (nothing)")
	for _, m := range state.Messages {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			assert.NotEqual(t, types.FinishReasonInterrupted, m.FinishReason, "the round stays in model context")
		}
	}
}

// toolTurnTranscript summarizes the stored messages: role, and for an
// assistant that called a tool its call's id and text, for a tool result the
// call it answers.
func toolTurnTranscript(t *testing.T, store statestore.Store, convID string) []string {
	t.Helper()
	state, err := store.Load(t.Context(), convID)
	require.NoError(t, err)
	var out []string
	for _, m := range state.Messages {
		entry := m.Role
		if len(m.ToolCalls) > 0 {
			entry += "+call:" + m.ToolCalls[0].ID + ":" + m.Content
		}
		if m.ToolResult != nil {
			entry += ":" + m.ToolResult.ID
		}
		out = append(out, entry)
	}
	return out
}

// requestRoles lists a Chat Completions request's message roles, an
// assistant message that calls tools as "assistant+call".
func requestRoles(body map[string]any) []string {
	var roles []string
	for _, m := range body["messages"].([]any) {
		msg := m.(map[string]any)
		role, _ := msg["role"].(string)
		if calls, ok := msg["tool_calls"].([]any); ok && len(calls) > 0 {
			role += "+call"
		}
		roles = append(roles, role)
	}
	return roles
}

// A caller who barges in while the tool runs, or once its result is back
// but before the model has said anything more, does not lose the round: the
// tool ran, so its call and result stay in the conversation, as a pair, with
// the round's text as heard, and the model's next request carries them.
func TestVADMode_BargeInDuringOrAfterTheToolKeepsTheRound(t *testing.T) {
	if testing.Short() {
		t.Skip("drives real VAD turns")
	}
	for _, tc := range []struct {
		name       string
		duringTool bool
	}{{"while the tool runs", true}, {"before the model answers", false}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OPENAI_API_KEY", "test-key")
			wire := newToolRoundWire(t)
			wire.silentSecondRound = true
			ttsSvc := newSegmentTTS()
			store := statestore.NewMemoryStore()
			unpaced := vadTestConfig()
			unpaced.NoPlaybackPacing = true // the round's sentence counts as heard once handed on
			sttSvc := newScriptedSTT("weather in paris", "stop")
			conv, err := OpenDuplex(createTestPackFile(t, toolRoundPack), "main",
				WithLLMProvider(ProviderSpec{ID: "agent", Type: "openai", Model: "gpt-4o-mini", BaseURL: wire.URL}),
				WithSkipSchemaValidation(), WithStateStore(store), WithVADMode(sttSvc, ttsSvc, unpaced))
			require.NoError(t, err)
			t.Cleanup(func() { _ = closeWithin(t, conv) })
			toolStarted, toolRelease := make(chan struct{}), make(chan struct{})
			var once sync.Once
			conv.OnTool("get_weather", func(map[string]any) (any, error) {
				once.Do(func() { close(toolStarted) })
				if tc.duringTool {
					select { // a slow tool the caller talks over
					case <-toolRelease:
					case <-time.After(streamingSpeechBound):
					}
				}
				return map[string]any{"sky": "sunny"}, nil
			})
			responses, err := conv.Response()
			require.NoError(t, err)
			rec := &speechRecorder{}
			go rec.consume(responses)
			convID := conv.ID()

			speak(t, conv)
			awaitClosed(t, toolStarted, "the tool never ran")
			require.True(t, waitFor(func() bool { return len(rec.audioMarkers()) >= 1 }, streamingSpeechBound),
				"the round's sentence was never spoken")
			if !tc.duringTool {
				require.True(t, waitFor(func() bool { calls, _, _ := wire.snapshot(); return calls >= 2 },
					streamingSpeechBound), "the tool's result never went back to the model")
			}

			speak(t, conv) // talks over the tool, or the model about to answer
			if tc.duringTool {
				close(toolRelease) // the tool finishes after all
			}
			require.True(t, waitFor(func() bool { _, _, last := wire.snapshot(); return asksAbout(last, "stop") },
				streamingSpeechBound), "the caller's words never reached the model")

			_, rejected, next := wire.snapshot()
			assert.Empty(t, rejected, "the model's next request is one OpenAI accepts")
			assert.Equal(t, []string{"system", "user", "assistant+call", "tool", "user"}, requestRoles(next),
				"the round the tool ran in goes back to the model with its result")

			require.NoError(t, closeWithin(t, conv))
			stored := toolTurnTranscript(t, store, convID)
			require.GreaterOrEqual(t, len(stored), 4)
			assert.Equal(t, []string{"user", "assistant+call:call_1:Let me look that up. ", "tool:call_1"}, stored[:3],
				"the transcript keeps the round, its sentence as heard, and the tool's result")
		})
	}
}

// asksAbout reports whether a Chat Completions request's last message is the
// user saying text.
func asksAbout(body map[string]any, text string) bool {
	msgs, _ := body["messages"].([]any)
	if len(msgs) == 0 {
		return false
	}
	last, _ := msgs[len(msgs)-1].(map[string]any)
	if last["role"] != "user" {
		return false
	}
	raw, _ := json.Marshal(last["content"])
	return strings.Contains(string(raw), text)
}
