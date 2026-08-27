package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// The SSE payloads below are copied from live gemini-3.7-flash responses. The
// exact delta type strings are not guessable — a thought signature arrives as
// "thought_signature", not "signature", and getting that wrong silently drops
// the signature, which makes the NEXT round's history invalid.

func sse(lines ...string) string { return strings.Join(lines, "\n") + "\n" }

func streamFromServer(t *testing.T, body string, req providers.PredictionRequest,
) []providers.StreamChunk {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	t.Setenv("GEMINI_API_KEY", "test-key")
	tp := NewToolProvider("g-stream", "gemini-3.7-flash", srv.URL,
		providers.ProviderDefaults{MaxTokens: 512}, false)

	ch, err := tp.predictStreamWithInteractions(context.Background(), req, nil)
	require.NoError(t, err)

	var chunks []providers.StreamChunk
	for c := range ch {
		require.NoError(t, c.Error)
		chunks = append(chunks, c)
	}
	return chunks
}

func textRequest() providers.PredictionRequest {
	return providers.PredictionRequest{
		Messages: []types.Message{{Role: roleUser, Content: "hi"}},
	}
}

// TestInteractionsStream_TextAnswer covers the answering path: text deltas
// accumulate, and the terminal chunk reports a completed turn.
func TestInteractionsStream_TextAnswer(t *testing.T) {
	body := sse(
		`event: interaction.created`,
		`data: {"interaction":{"id":"i1","status":"in_progress"}}`,
		``,
		`event: step.start`,
		`data: {"index":0,"step":{"type":"model_output"}}`,
		``,
		`event: step.delta`,
		`data: {"index":0,"delta":{"type":"text","text":"{\"ok\":"}}`,
		``,
		`event: step.delta`,
		`data: {"index":0,"delta":{"type":"text","text":"true}"}}`,
		``,
		`event: interaction.completed`,
		`data: {"interaction":{"id":"i1","status":"completed","usage":{"total_input_tokens":5,"total_output_tokens":3}}}`,
		``,
	)

	chunks := streamFromServer(t, body, textRequest())
	require.NotEmpty(t, chunks)

	final := chunks[len(chunks)-1]
	assert.Equal(t, `{"ok":true}`, final.Content, "text deltas must accumulate")
	assert.Empty(t, final.ToolCalls)
	require.NotNil(t, final.FinishReason)
	assert.Equal(t, "stop", *final.FinishReason)
	require.NotNil(t, final.CostInfo, "usage on the completion event must produce cost info")

	// Deltas are surfaced incrementally, not only at the end.
	var sawDelta bool
	for _, c := range chunks {
		if c.Delta != "" {
			sawDelta = true
		}
	}
	assert.True(t, sawDelta, "callers streaming text need per-delta chunks")
}

// TestInteractionsStream_ThoughtSignature pins the delta type that cost a live
// round trip: "thought_signature", not "signature". The signature must reach
// the caller as OpaqueReasoning, since replaying it is mandatory on the next
// round.
func TestInteractionsStream_ThoughtSignature(t *testing.T) {
	body := sse(
		`event: step.start`,
		`data: {"index":0,"step":{"type":"thought"}}`,
		``,
		`event: step.delta`,
		`data: {"index":0,"delta":{"type":"thought_signature","signature":"SIG-XYZ"}}`,
		``,
		`event: step.stop`,
		`data: {"index":0}`,
		``,
		`event: step.start`,
		`data: {"index":1,"step":{"type":"model_output"}}`,
		``,
		`event: step.delta`,
		`data: {"index":1,"delta":{"type":"text","text":"done"}}`,
		``,
		`event: interaction.completed`,
		`data: {"interaction":{"id":"i1","status":"completed"}}`,
		``,
	)

	chunks := streamFromServer(t, body, textRequest())

	var sigs []types.OpaqueReasoning
	for _, c := range chunks {
		sigs = append(sigs, c.OpaqueReasoning...)
	}
	require.Len(t, sigs, 1, "the thought signature must reach the caller")
	assert.Equal(t, kindInteractionsThought, sigs[0].Kind)
	assert.Equal(t, "SIG-XYZ", sigs[0].Data)

	// A signature is opaque, so it must never be mistaken for spoken content.
	assert.Equal(t, "done", chunks[len(chunks)-1].Content)
	for _, c := range chunks {
		assert.NotContains(t, c.Content, "SIG-XYZ")
		assert.NotContains(t, c.Delta, "SIG-XYZ")
	}
}

// TestInteractionsStream_ToolCall covers the hand-off: arguments arrive as
// deltas and are only complete at interaction.completed, so the call is
// assembled there rather than at step.stop.
func TestInteractionsStream_ToolCall(t *testing.T) {
	body := sse(
		`event: step.start`,
		`data: {"index":0,"step":{"type":"thought"}}`,
		``,
		`event: step.delta`,
		`data: {"index":0,"delta":{"type":"thought_signature","signature":"SIG"}}`,
		``,
		`event: step.start`,
		`data: {"index":1,"step":{"id":"call_7","type":"function_call","name":"get_temperature","arguments":{}}}`,
		``,
		`event: step.delta`,
		`data: {"index":1,"delta":{"type":"arguments_delta","arguments":"{\"city\":"}}`,
		``,
		`event: step.delta`,
		`data: {"index":1,"delta":{"type":"arguments_delta","arguments":"\"Bristol\"}"}}`,
		``,
		`event: interaction.completed`,
		`data: {"interaction":{"id":"i1","status":"requires_action"}}`,
		``,
	)

	chunks := streamFromServer(t, body, textRequest())
	final := chunks[len(chunks)-1]

	require.Len(t, final.ToolCalls, 1)
	assert.Equal(t, "call_7", final.ToolCalls[0].ID)
	assert.Equal(t, "get_temperature", final.ToolCalls[0].Name)
	assert.JSONEq(t, `{"city":"Bristol"}`, string(final.ToolCalls[0].Args),
		"argument deltas must be concatenated before the call is emitted")

	require.NotNil(t, final.FinishReason)
	assert.Equal(t, "tool_calls", *final.FinishReason,
		"a caller must be able to tell a tool hand-off from a finished answer")
}

// TestInteractionsStream_ToolCallWithoutArgumentDeltas keeps a zero-argument
// tool call valid JSON rather than an empty string the caller cannot parse.
func TestInteractionsStream_ToolCallWithoutArgumentDeltas(t *testing.T) {
	body := sse(
		`event: step.start`,
		`data: {"index":0,"step":{"id":"c1","type":"function_call","name":"ping"}}`,
		``,
		`event: interaction.completed`,
		`data: {"interaction":{"id":"i1","status":"requires_action"}}`,
		``,
	)
	final := streamFromServer(t, body, textRequest())
	require.NotEmpty(t, final)
	calls := final[len(final)-1].ToolCalls
	require.Len(t, calls, 1)
	assert.JSONEq(t, `{}`, string(calls[0].Args))
}

// TestInteractionsStream_HTTPErrorSurfaces keeps a failed stream an error
// rather than an empty, successful-looking turn.
func TestInteractionsStream_HTTPErrorSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Unknown parameter 'call_id'"}}`))
	}))
	defer srv.Close()

	t.Setenv("GEMINI_API_KEY", "test-key")
	tp := NewToolProvider("g-stream", "gemini-3.7-flash", srv.URL,
		providers.ProviderDefaults{MaxTokens: 512}, false)

	_, err := tp.predictStreamWithInteractions(context.Background(), textRequest(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "call_id")
}

// TestInteractionsStream_MalformedEventsSkipped keeps one bad frame from
// aborting a turn that is otherwise fine.
func TestInteractionsStream_MalformedEventsSkipped(t *testing.T) {
	body := sse(
		`event: step.start`,
		`data: {not json`,
		``,
		`event: step.start`,
		`data: {"index":0,"step":{"type":"model_output"}}`,
		``,
		`event: step.delta`,
		`data: {"index":0,"delta":{"type":"text","text":"ok"}}`,
		``,
		`event: step.delta`,
		`data: {"index":9,"delta":{"type":"text","text":"orphan"}}`,
		``,
		`event: interaction.completed`,
		`data: {"interaction":{"id":"i1","status":"completed"}}`,
		``,
	)
	chunks := streamFromServer(t, body, textRequest())
	final := chunks[len(chunks)-1]
	assert.Equal(t, "ok", final.Content,
		"a delta for an unknown step index must be ignored, not appended")
}

// TestCollectStreamToolCalls_OrdersByIndex pins that calls come back in the
// order the model emitted them, since map iteration is random.
func TestCollectStreamToolCalls_OrdersByIndex(t *testing.T) {
	steps := map[int]*streamStep{
		2: {kind: stepTypeFunctionCall, callID: "second", name: "b"},
		0: {kind: stepTypeThought},
		1: {kind: stepTypeFunctionCall, callID: "first", name: "a"},
	}
	calls := collectStreamToolCalls(steps)
	require.Len(t, calls, 2)
	assert.Equal(t, "first", calls[0].ID)
	assert.Equal(t, "second", calls[1].ID)

	assert.Nil(t, collectStreamToolCalls(map[int]*streamStep{}))
}

func TestFirstNonEmpty(t *testing.T) {
	assert.Equal(t, "a", firstNonEmpty("", "a", "b"))
	assert.Empty(t, firstNonEmpty("", ""))
}

// TestPredictStreamWithTools_RoutesToInteractions proves the streaming routing
// is reachable from the public entry point. The pipeline streams by default, so
// a decision nothing acts on would leave real traffic unconstrained.
func TestPredictStreamWithTools_RoutesToInteractions(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse(
			`event: interaction.completed`,
			`data: {"interaction":{"id":"i1","status":"completed"}}`, ``)))
	}))
	defer srv.Close()

	t.Setenv("GEMINI_API_KEY", "test-key")
	tp := NewToolProvider("g-stream", "gemini-3.7-flash", srv.URL,
		providers.ProviderDefaults{MaxTokens: 512}, false)
	tools, err := tp.BuildTooling([]*providers.ToolDescriptor{{
		Name: "probe", Description: "p", InputSchema: json.RawMessage(`{"type":"object"}`)}})
	require.NoError(t, err)

	ch, err := tp.PredictStreamWithTools(context.Background(), providers.PredictionRequest{
		Messages:       []types.Message{{Role: roleUser, Content: "hi"}},
		ResponseFormat: schemaFormat(),
	}, tools, "auto")
	require.NoError(t, err)
	for range ch { //nolint:revive // draining
	}

	assert.Contains(t, gotPath, "/interactions",
		"a streaming schema+tools turn must reach the Interactions endpoint")
}
