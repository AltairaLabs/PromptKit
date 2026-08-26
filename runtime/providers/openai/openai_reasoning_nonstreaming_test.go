package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestPredict_CarriesReasoningContent covers a path asymmetry inside this one
// provider: the streaming loop reads choice.Delta.ReasoningContent
// (openai.go), but the non-streaming response struct had no reasoning field at
// all, so Predict silently returned none.
//
// OpenAI-compatible reasoning models (o-series via chat completions,
// deepseek-r1, qwq) return the chain-of-thought summary on
// message.reasoning_content. Without this the SDK's Send() path reports no
// reasoning for those models while Stream() reports it — the same
// fix-one-path-leave-the-other-broken shape that bites elsewhere in the repo.
func TestPredict_CarriesReasoningContent(t *testing.T) {
	const thought = "The user asked for 2+2. That is 4."

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Hand-written so the wire shape is asserted, not just our own struct.
		_, _ = w.Write([]byte(`{
			"id": "cmpl-1",
			"object": "chat.completion",
			"model": "deepseek-reasoner",
			"choices": [{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": "4",
					"reasoning_content": "` + thought + `"
				},
				"finish_reason": "stop"
			}],
			"usage": {"prompt_tokens": 5, "completion_tokens": 3, "total_tokens": 8}
		}`))
	}))
	defer server.Close()

	provider := &Provider{
		BaseProvider: providers.NewBaseProvider("test", false, &http.Client{Timeout: 30 * time.Second}),
		model:        "deepseek-reasoner",
		baseURL:      server.URL,
		apiKey:       "test-key",
		defaults:     providers.ProviderDefaults{MaxTokens: 100},
	}

	resp, err := provider.Predict(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "2+2?"}},
	})
	require.NoError(t, err)

	require.NotNil(t, resp.Reasoning,
		"Predict dropped reasoning_content; the streaming path reads it but this one did not")
	assert.Equal(t, thought, resp.Reasoning.Text)

	// Reasoning must never contaminate spoken content.
	assert.Equal(t, "4", resp.Content)
	assert.NotContains(t, resp.Content, thought)
}

// TestPredict_NoReasoningContent_StaysNil keeps "the model did not reason"
// distinguishable from "the trace was dropped".
func TestPredict_NoReasoningContent_StaysNil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "cmpl-2","object":"chat.completion","model":"gpt-4",
			"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`))
	}))
	defer server.Close()

	provider := &Provider{
		BaseProvider: providers.NewBaseProvider("test", false, &http.Client{Timeout: 30 * time.Second}),
		model:        "gpt-4",
		baseURL:      server.URL,
		apiKey:       "test-key",
		defaults:     providers.ProviderDefaults{MaxTokens: 100},
	}

	resp, err := provider.Predict(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Nil(t, resp.Reasoning)
}
