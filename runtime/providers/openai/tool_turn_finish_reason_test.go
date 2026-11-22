package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// The fixtures under testdata/ are real gpt-5-mini replies to a prompt whose
// only sensible answer is a call to the "lookup" tool, captured from
// api.openai.com (encrypted reasoning and obfuscation padding removed).
//
// The Responses API sends no finish_reason. A reply that calls a function is
// a response with status "completed" whose output holds a function_call item;
// a reply cut short is status "incomplete" with incomplete_details.reason.
// Chat Completions, by contrast, sends finish_reason "tool_calls".

// serveFixture serves a testdata file as the body of every request.
func serveFixture(t *testing.T, name, contentType string) *httptest.Server {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// fixtureToolProvider builds an openai provider through the factory, pointed at
// srv, on the API apiMode names, with the lookup tool built for that API.
func fixtureToolProvider(t *testing.T, srv *httptest.Server, apiMode string) (providers.ToolSupport, any) {
	t.Helper()
	t.Setenv("OPENAI_API_KEY", "test-key")
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "fixture", Type: "openai", Model: "gpt-5-mini", BaseURL: srv.URL,
		Defaults:         providers.ProviderDefaults{MaxTokens: 256},
		AdditionalConfig: map[string]any{"api_mode": apiMode},
	})
	require.NoError(t, err)
	tp := p.(providers.ToolSupport)
	tools, err := tp.BuildTooling([]*providers.ToolDescriptor{{
		Name: "lookup", Description: "Look up an order's status by id",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`),
	}})
	require.NoError(t, err)
	return tp, tools
}

func orderRequest() providers.PredictionRequest {
	return providers.PredictionRequest{
		Messages:  []types.Message{{Role: "user", Content: "What is the status of order 42?"}},
		MaxTokens: 256,
	}
}

// lastStreamChunk drains a stream and returns its terminal chunk.
func lastStreamChunk(t *testing.T, ch <-chan providers.StreamChunk) providers.StreamChunk {
	t.Helper()
	var last providers.StreamChunk
	n := 0
	for c := range ch {
		require.NoError(t, c.Error)
		last = c
		n++
	}
	require.Positive(t, n, "stream produced no chunks")
	return last
}

func TestToolTurnFinishReason_ResponsesStreamToolCallIsToolUse(t *testing.T) {
	srv := serveFixture(t, "responses_stream_tool_call.sse", "text/event-stream")
	tp, tools := fixtureToolProvider(t, srv, "responses")

	ch, err := tp.PredictStreamWithTools(context.Background(), orderRequest(), tools, "auto")
	require.NoError(t, err)
	last := lastStreamChunk(t, ch)

	require.NotNil(t, last.FinishReason)
	assert.Equal(t, types.FinishReasonToolUse, *last.FinishReason,
		"a completed Responses reply that calls a function stopped to call tools")
	require.Len(t, last.ToolCalls, 1)
	assert.Equal(t, "call_H5o9EabW9GFqKe7Htw2s4IQq", last.ToolCalls[0].ID)
	assert.Equal(t, "lookup", last.ToolCalls[0].Name)
	assert.JSONEq(t, `{"id":"42"}`, string(last.ToolCalls[0].Args))
}

func TestToolTurnFinishReason_ResponsesToolCallIsToolUse(t *testing.T) {
	srv := serveFixture(t, "responses_tool_call.json", "application/json")
	tp, tools := fixtureToolProvider(t, srv, "responses")

	resp, toolCalls, err := tp.PredictWithTools(context.Background(), orderRequest(), tools, "auto")

	require.NoError(t, err)
	assert.Equal(t, types.FinishReasonToolUse, resp.FinishReason)
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "call_3MvqE8Q4xmag3jFArBee5he9", toolCalls[0].ID)
}

func TestToolTurnFinishReason_ResponsesTextReplyIsStop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","object":"response","status":"completed","incomplete_details":null,` +
			`"output":[{"type":"message","role":"assistant","content":[{"type":"output_text",` +
			`"text":"Order 42 shipped on Tuesday."}]}],"usage":{"input_tokens":5,"output_tokens":7}}`))
	}))
	t.Cleanup(srv.Close)
	tp, tools := fixtureToolProvider(t, srv, "responses")

	resp, toolCalls, err := tp.PredictWithTools(context.Background(), orderRequest(), tools, "auto")

	require.NoError(t, err)
	assert.Empty(t, toolCalls)
	assert.Equal(t, "Order 42 shipped on Tuesday.", resp.Content)
	assert.Equal(t, types.FinishReasonStop, resp.FinishReason)
}

func TestToolTurnFinishReason_ResponsesIncompleteIsMaxOutputTokens(t *testing.T) {
	srv := serveFixture(t, "responses_incomplete.json", "application/json")
	tp, tools := fixtureToolProvider(t, srv, "responses")

	resp, _, err := tp.PredictWithTools(context.Background(), orderRequest(), tools, "auto")

	require.NoError(t, err)
	assert.Equal(t, types.FinishReasonMaxOutputTokens, resp.FinishReason,
		"status incomplete with reason max_output_tokens is a reply cut short by the output budget")
}

func TestToolTurnFinishReason_ResponsesIncompleteContentFilterIsSafety(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","object":"response","status":"incomplete",` +
			`"incomplete_details":{"reason":"content_filter"},"output":[]}`))
	}))
	t.Cleanup(srv.Close)
	tp, tools := fixtureToolProvider(t, srv, "responses")

	resp, _, err := tp.PredictWithTools(context.Background(), orderRequest(), tools, "auto")

	require.NoError(t, err)
	assert.Equal(t, types.FinishReasonSafety, resp.FinishReason)
}

// Chat Completions sends finish_reason "tool_calls"; it must stay tool_use.
func TestToolTurnFinishReason_ChatStreamToolCallIsToolUse(t *testing.T) {
	srv := serveFixture(t, "chat_stream_tool_call.sse", "text/event-stream")
	tp, tools := fixtureToolProvider(t, srv, "completions")

	ch, err := tp.PredictStreamWithTools(context.Background(), orderRequest(), tools, "auto")
	require.NoError(t, err)
	last := lastStreamChunk(t, ch)

	require.NotNil(t, last.FinishReason)
	assert.Equal(t, types.FinishReasonToolUse, *last.FinishReason)
	require.Len(t, last.ToolCalls, 1)
	assert.Equal(t, "call_zrb3J0rj076OmXWqOZIf8pW1", last.ToolCalls[0].ID)
	assert.JSONEq(t, `{"id":"42"}`, string(last.ToolCalls[0].Args))
}
