package vllm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// toolRoundTripHistory is the message history a second round of tool use
// carries: the assistant turn that made the call, followed by its result.
func toolRoundTripHistory() []types.Message {
	return []types.Message{
		{Role: "user", Content: "what is 6 times 7?"},
		{
			Role:    "assistant",
			Content: "",
			ToolCalls: []types.MessageToolCall{{
				ID:   "call_abc123",
				Name: "multiply",
				Args: json.RawMessage(`{"a":6,"b":7}`),
			}},
		},
		types.NewToolResultMessage(types.NewTextToolResult("call_abc123", "multiply", "42")),
	}
}

// findByRole returns the first serialized message with the given role.
func findByRole(t *testing.T, msgs []vllmMessage, role string) vllmMessage {
	t.Helper()
	for _, m := range msgs {
		if m.Role == role {
			return m
		}
	}
	t.Fatalf("no %q message in %+v", role, msgs)
	return vllmMessage{}
}

// The assistant's tool calls must survive serialization. Without this the model
// is told it never made the call it just made, and round 2 of any tool
// conversation is incoherent.
func TestPrepareMessages_SerializesAssistantToolCalls(t *testing.T) {
	p := &Provider{}

	msgs, err := p.prepareMessages(&providers.PredictionRequest{Messages: toolRoundTripHistory()})

	require.NoError(t, err)
	assistant := findByRole(t, msgs, "assistant")
	require.Len(t, assistant.ToolCalls, 1)
	assert.Equal(t, "call_abc123", assistant.ToolCalls[0].ID)
	assert.Equal(t, "function", assistant.ToolCalls[0].Type)
	assert.Equal(t, "multiply", assistant.ToolCalls[0].Function.Name)
	// vLLM takes arguments as a JSON *string*, not an object.
	assert.JSONEq(t, `{"a":6,"b":7}`, assistant.ToolCalls[0].Function.Arguments)
}

// The tool result must carry tool_call_id or it cannot be attached to the call
// that produced it — an OpenAI-compatible server will 400 or ignore it.
func TestPrepareMessages_LinksToolResultToItsCall(t *testing.T) {
	p := &Provider{}

	msgs, err := p.prepareMessages(&providers.PredictionRequest{Messages: toolRoundTripHistory()})

	require.NoError(t, err)
	tool := findByRole(t, msgs, "tool")
	assert.Equal(t, "call_abc123", tool.ToolCallID)
	assert.Equal(t, "42", tool.Content)
}

// The invariant that matters end to end: every tool result references a call
// the assistant actually made in the same request.
func TestPrepareMessages_ToolRoundTripIsLinked(t *testing.T) {
	p := &Provider{}

	msgs, err := p.prepareMessages(&providers.PredictionRequest{Messages: toolRoundTripHistory()})

	require.NoError(t, err)
	called := map[string]bool{}
	for _, m := range msgs {
		for _, tc := range m.ToolCalls {
			called[tc.ID] = true
		}
	}
	for _, m := range msgs {
		if m.Role == "tool" {
			assert.True(t, called[m.ToolCallID],
				"tool result %q references no tool call in the request", m.ToolCallID)
		}
	}
	assert.NotEmpty(t, called, "no tool calls were serialized at all")
}

// The multimodal serializer is the other path into the same request body. A fix
// applied to only one of the two leaves multimodal tool use broken.
func TestPrepareMultimodalMessages_SerializesToolFields(t *testing.T) {
	p := &Provider{}

	msgs, err := p.prepareMultimodalMessages(providers.PredictionRequest{Messages: toolRoundTripHistory()})

	require.NoError(t, err)
	assistant := findByRole(t, msgs, "assistant")
	require.Len(t, assistant.ToolCalls, 1)
	assert.Equal(t, "call_abc123", assistant.ToolCalls[0].ID)
	assert.Equal(t, "call_abc123", findByRole(t, msgs, "tool").ToolCallID)
}

// Tool-free requests must serialize exactly as before: the new fields are
// omitempty, so no tool_calls or tool_call_id key may appear on the wire.
func TestPrepareMessages_NoToolKeysForToolFreeMessages(t *testing.T) {
	p := &Provider{}

	msgs, err := p.prepareMessages(&providers.PredictionRequest{
		System:   "be brief",
		Messages: []types.Message{{Role: "user", Content: "hello"}},
	})

	require.NoError(t, err)
	body, err := json.Marshal(msgs)
	require.NoError(t, err)
	assert.NotContains(t, string(body), "tool_calls")
	assert.NotContains(t, string(body), "tool_call_id")
}

// A failed tool must still be linked to its call. The pipeline builds failure
// results the way handleToolResult does (stages_provider.go): the error text
// goes into the content and Error is set alongside as structured metadata, so
// the content path already carries it — but the link is what was missing.
func TestPrepareMessages_FailedToolResultIsStillLinked(t *testing.T) {
	p := &Provider{}
	result := types.NewTextToolResult("call_err", "divide", "Tool execution failed: division by zero")
	result.Error = "division by zero"

	msgs, err := p.prepareMessages(&providers.PredictionRequest{
		Messages: []types.Message{types.NewToolResultMessage(result)},
	})

	require.NoError(t, err)
	tool := findByRole(t, msgs, "tool")
	assert.Equal(t, "call_err", tool.ToolCallID)
	assert.Contains(t, tool.Content, "division by zero")
}

// PredictWithTools is one of the two entry points the runtime uses; the fields
// must survive all the way to the request body, not just the serializer.
func TestPredictWithTools_SendsToolFieldsOnTheWire(t *testing.T) {
	var got map[string]any
	srv := newCapturingServer(t, &got)
	p := newTestProvider(srv.URL)

	_, _, err := p.PredictWithTools(t.Context(),
		providers.PredictionRequest{Messages: toolRoundTripHistory()}, nil, "")
	require.NoError(t, err)

	assistant, tool := findWireMessages(t, got)
	assert.NotEmpty(t, assistant["tool_calls"], "assistant tool_calls missing from request body")
	assert.Equal(t, "call_abc123", tool["tool_call_id"])
}

func TestPredictStreamWithTools_SendsToolFieldsOnTheWire(t *testing.T) {
	var got map[string]any
	srv := newCapturingSSEServer(t, &got)
	p := newTestProvider(srv.URL)

	ch, err := p.PredictStreamWithTools(t.Context(),
		providers.PredictionRequest{Messages: toolRoundTripHistory()}, nil, "")
	require.NoError(t, err)
	for range ch { //nolint:revive // drain the stream so the request completes
	}

	assistant, tool := findWireMessages(t, got)
	assert.NotEmpty(t, assistant["tool_calls"], "assistant tool_calls missing from streamed request body")
	assert.Equal(t, "call_abc123", tool["tool_call_id"])
}

func newTestProvider(url string) *Provider {
	return NewProvider("vllm-test", "test-model", url, providers.ProviderDefaults{}, false, nil)
}

// newCapturingServer records the decoded request body and returns a minimal
// non-tool completion.
func newCapturingServer(t *testing.T, into *map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(into))
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(vllmChatResponse{
			ID: "chatcmpl-1", Model: "vllm-test",
			Choices: []vllmChatChoice{{
				Message:      vllmMessage{Role: "assistant", Content: "ok"},
				FinishReason: "stop",
			}},
		}))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newCapturingSSEServer records the decoded request body and returns a minimal
// terminated stream.
func newCapturingSSEServer(t *testing.T, into *map[string]any) *httptest.Server {
	t.Helper()
	const sse = "data: {\"id\":\"c1\",\"model\":\"vllm-test\",\"choices\":" +
		"[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(into))
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sse))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// findWireMessages pulls the assistant and tool messages out of a captured
// request body.
func findWireMessages(t *testing.T, body map[string]any) (assistant, tool map[string]any) {
	t.Helper()
	raw, ok := body["messages"].([]any)
	require.True(t, ok, "request body has no messages array: %v", body)
	for _, m := range raw {
		msg, ok := m.(map[string]any)
		require.True(t, ok)
		switch msg["role"] {
		case "assistant":
			assistant = msg
		case "tool":
			tool = msg
		}
	}
	require.NotNil(t, assistant, "no assistant message on the wire")
	require.NotNil(t, tool, "no tool message on the wire")
	return assistant, tool
}
