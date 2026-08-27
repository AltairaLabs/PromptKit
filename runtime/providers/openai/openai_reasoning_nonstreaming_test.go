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
const thoughtText = "The user asked for 2+2. That is 4."

func TestPredict_CarriesReasoningContent(t *testing.T) {
	const thought = thoughtText

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

// TestPredict_ReasoningPresenceIsDiscriminated pins that nil means "the model
// did not reason" and is not merely the zero value we would get from ignoring
// the field.
//
// Asserting nil on a no-reasoning response alone is not a test: it passes
// against an implementation that never reads reasoning_content at all. Driving
// BOTH inputs through the same provider is, because the present case fails the
// moment the field stops being read, and the absent case fails if the parser
// manufactures an empty non-nil trace.
func TestPredict_ReasoningPresenceIsDiscriminated(t *testing.T) {
	cases := []struct {
		name        string
		messageJSON string
		wantTrace   bool
	}{
		{
			name:        "reasoning present",
			messageJSON: `{"role":"assistant","content":"4","reasoning_content":"` + thoughtText + `"}`,
			wantTrace:   true,
		},
		{
			name:        "reasoning absent",
			messageJSON: `{"role":"assistant","content":"4"}`,
			wantTrace:   false,
		},
		{
			name:        "reasoning present but empty",
			messageJSON: `{"role":"assistant","content":"4","reasoning_content":""}`,
			wantTrace:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"id":"c1","object":"chat.completion","model":"deepseek-reasoner",
				"choices":[{"index":0,"message":` + tc.messageJSON + `,"finish_reason":"stop"}],
				"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
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

			if tc.wantTrace {
				require.NotNil(t, resp.Reasoning, "reasoning_content was present but produced no trace")
				assert.Equal(t, thoughtText, resp.Reasoning.Text)
			} else {
				assert.Nil(t, resp.Reasoning,
					"absent or empty reasoning_content must stay nil, not an empty trace")
			}
			// Spoken content is unaffected either way.
			assert.Equal(t, "4", resp.Content)
		})
	}
}
