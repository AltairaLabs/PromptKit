package conformance_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/claude"
	"github.com/AltairaLabs/PromptKit/runtime/providers/gemini"
	"github.com/AltairaLabs/PromptKit/runtime/providers/ollama"
	"github.com/AltairaLabs/PromptKit/runtime/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/providers/vllm"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// The reasoning trace only reaches a consumer if the provider puts it on
// PredictionResponse.Reasoning in the first place. Everything downstream —
// the provider stage's per-round accumulation, the message.created event, the
// SDK Response — is provider-agnostic and carries whatever it is given.
//
// So this is the cross-provider seam: for each provider that supports
// reasoning, a wire response carrying a thought must produce a non-nil
// Reasoning, and must NOT leak the thought into spoken content.
//
// These run against httptest servers rather than live endpoints so every
// provider is covered on every CI run. The live oracle for the end-to-end
// path is TestLive_ReasoningAndRounds in runtime/pipeline/stage.

const conformanceThought = "Let me work through this step by step. The answer is 4."

// reasoningCase describes one provider's non-streaming reasoning contract.
type reasoningCase struct {
	name string
	// body is the canned wire response, written by hand so the test asserts
	// the real wire shape rather than round-tripping our own structs.
	body string
	// build constructs the provider against the test server URL.
	build func(t *testing.T, url string) providers.Provider
	// wantContent is the spoken content the response should yield.
	wantContent string
}

func TestProviders_NonStreamingReasoning_ReachesResponse(t *testing.T) {
	cases := []reasoningCase{
		{
			name:        "openai_chat_completions",
			wantContent: "4",
			body: `{"id":"c1","object":"chat.completion","model":"deepseek-reasoner",
				"choices":[{"index":0,"message":{"role":"assistant","content":"4",
				"reasoning_content":"` + conformanceThought + `"},"finish_reason":"stop"}],
				"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`,
			build: buildOpenAI,
		},
		{
			name:        "ollama",
			wantContent: "4",
			body: `{"id":"c1","object":"chat.completion","model":"deepseek-r1",
				"choices":[{"index":0,"message":{"role":"assistant","content":"4",
				"reasoning_content":"` + conformanceThought + `"},"finish_reason":"stop"}],
				"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`,
			build: buildOllama,
		},
		{
			name:        "vllm",
			wantContent: "4",
			body: `{"id":"c1","object":"chat.completion","model":"qwq",
				"choices":[{"index":0,"message":{"role":"assistant","content":"4",
				"reasoning_content":"` + conformanceThought + `"},"finish_reason":"stop"}],
				"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`,
			build: buildVLLM,
		},
		{
			name:        "claude",
			wantContent: "4",
			body: `{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6",
				"content":[{"type":"thinking","thinking":"` + conformanceThought + `"},
				{"type":"text","text":"4"}],
				"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":3}}`,
			build: buildClaude,
		},
		{
			name:        "gemini",
			wantContent: "4",
			body: `{"candidates":[{"content":{"role":"model","parts":[
				{"text":"` + conformanceThought + `","thought":true},{"text":"4"}]},
				"finishReason":"STOP"}],
				"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":3,"totalTokenCount":8}}`,
			build: buildGemini,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := tc.body
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()

			p := tc.build(t, server.URL)
			defer func() { _ = p.Close() }()

			resp, err := p.Predict(context.Background(), providers.PredictionRequest{
				Messages: []types.Message{{Role: "user", Content: "2+2?"}},
			})
			require.NoError(t, err)

			require.NotNilf(t, resp.Reasoning,
				"%s dropped the reasoning trace on the non-streaming path", tc.name)
			assert.Containsf(t, resp.Reasoning.Text, "step by step",
				"%s returned a reasoning trace that is not the model's thought", tc.name)

			// The invariant that matters as much as carrying it: reasoning is
			// NOT conversational content.
			assert.Equalf(t, tc.wantContent, resp.Content,
				"%s put something unexpected in spoken content", tc.name)
			assert.NotContainsf(t, resp.Content, "step by step",
				"%s leaked reasoning into spoken content", tc.name)
		})
	}
}

// Builders point each provider at the test server. API keys are set where the
// provider requires one to send a request at all; the canned server ignores them.

func buildOpenAI(t *testing.T, url string) providers.Provider {
	t.Helper()
	t.Setenv("OPENAI_API_KEY", "test-key")
	return openai.NewProvider("openai-test", "deepseek-reasoner", url,
		providers.ProviderDefaults{MaxTokens: 100}, false)
}

func buildOllama(t *testing.T, url string) providers.Provider {
	t.Helper()
	return ollama.NewProvider("ollama-test", "deepseek-r1", url,
		providers.ProviderDefaults{MaxTokens: 100}, false, nil)
}

func buildVLLM(t *testing.T, url string) providers.Provider {
	t.Helper()
	return vllm.NewProvider("vllm-test", "qwq", url,
		providers.ProviderDefaults{MaxTokens: 100}, false, nil)
}

func buildClaude(t *testing.T, url string) providers.Provider {
	t.Helper()
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	return claude.NewProvider("claude-test", "claude-sonnet-4-6", url,
		providers.ProviderDefaults{MaxTokens: 100}, false)
}

func buildGemini(t *testing.T, url string) providers.Provider {
	t.Helper()
	t.Setenv("GEMINI_API_KEY", "test-key")
	return gemini.NewProvider("gemini-test", "gemini-2.5-flash", url,
		providers.ProviderDefaults{MaxTokens: 100}, false)
}
