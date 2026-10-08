package conformance_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// An explicitly set temperature of 0 reaches the wire as 0, even over a
// provider's non-zero default, and frequency/presence penalties reach every
// provider whose API takes them (#2212), as does top_k (#2220). Before, every
// provider replaced a zero temperature with its default, and nothing carried the
// penalties or top_k.
func TestProviders_SamplingParamsReachWireOnAllPaths(t *testing.T) {
	const openAIReply = `{"id":"c","object":"chat.completion","model":"m","choices":` +
		`[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],` +
		`"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	const claudeReply = `{"id":"m","type":"message","role":"assistant","model":"c",` +
		`"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
	const geminiReply = `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},` +
		`"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`

	cases := []struct {
		name      string
		spec      providers.ProviderSpec
		reply     string
		config    string // the object holding the sampling keys: "" for the body, else a field of it
		temp      string
		penalties [2]string // frequency, presence wire keys; "" where the API takes none
		topK      string    // top_k's wire key; "" where the API takes none
	}{
		{name: "openai_chat_completions", spec: providers.ProviderSpec{Type: "openai", Model: "gpt-4o-mini",
			AdditionalConfig: map[string]any{"api_mode": "completions"}}, reply: openAIReply,
			temp: "temperature", penalties: [2]string{"frequency_penalty", "presence_penalty"}},
		{name: "openai_responses", spec: providers.ProviderSpec{Type: "openai", Model: "gpt-4o-mini",
			AdditionalConfig: map[string]any{"api_mode": "responses"}}, reply: `{"id":"r","object":"response","output":[]}`,
			temp: "temperature"},
		{name: "claude", spec: providers.ProviderSpec{Type: "claude", Model: "claude-3-5-sonnet-20241022"},
			reply: claudeReply, temp: "temperature", topK: "top_k"},
		{name: "gemini", spec: providers.ProviderSpec{Type: "gemini", Model: "gemini-2.5-flash"},
			reply: geminiReply, config: "generationConfig", temp: "temperature", topK: "topK"},
		{name: "vllm", spec: providers.ProviderSpec{Type: "vllm", Model: "qwen3"}, reply: openAIReply,
			temp: "temperature", penalties: [2]string{"frequency_penalty", "presence_penalty"}, topK: "top_k"},
		{name: "ollama", spec: providers.ProviderSpec{Type: "ollama", Model: "llama3"}, reply: openAIReply,
			temp: "temperature", penalties: [2]string{"frequency_penalty", "presence_penalty"}},
	}
	paths := []string{"predict", "predict_stream", "predict_with_tools", "predict_stream_with_tools"}
	freq, pres, topK := float32(0.3), float32(0.4), 40

	for _, tc := range cases {
		for _, path := range paths {
			t.Run(tc.name+"/"+path, func(t *testing.T) {
				t.Setenv("OPENAI_API_KEY", "test-key")
				t.Setenv("ANTHROPIC_API_KEY", "test-key")
				t.Setenv("GEMINI_API_KEY", "test-key")
				cs := newCaptureServer(t, tc.reply)
				spec := tc.spec
				spec.ID = tc.name + "-" + path + "-sampling" // WarnUnsentParams warns once per ID
				spec.BaseURL = cs.srv.URL
				spec.Defaults = providers.ProviderDefaults{MaxTokens: 256, Temperature: 0.7}
				p, err := providers.CreateProviderFromSpec(spec)
				require.NoError(t, err)
				defer func() { _ = p.Close() }()

				var logs bytes.Buffer
				logger.SetOutput(&logs)
				t.Cleanup(func() { logger.SetOutput(nil) })
				runSamplingPath(t, p, path, providers.PredictionRequest{
					Messages:         []types.Message{{Role: "user", Content: "hello"}},
					MaxTokens:        256,
					Temperature:      0,
					TemperatureSet:   true,
					FrequencyPenalty: &freq,
					PresencePenalty:  &pres,
					TopK:             &topK,
				})

				body := cs.lastBody()
				require.NotEmptyf(t, body, "%s/%s sent no request", tc.name, path)
				var sent map[string]any
				require.NoError(t, json.Unmarshal([]byte(body), &sent), body)
				cfg := sent
				if tc.config != "" {
					cfg, _ = sent[tc.config].(map[string]any)
					require.NotNilf(t, cfg, "no %s in %s", tc.config, body)
				}

				assert.Equalf(t, float64(0), cfg[tc.temp],
					"an explicit temperature of 0 must reach the wire as 0, not the 0.7 default\nrequest=%s", body)
				for i, key := range tc.penalties {
					want := []float64{0.3, 0.4}[i]
					if key == "" {
						continue
					}
					got, ok := cfg[key].(float64)
					require.Truef(t, ok, "%s dropped %s on %s\nrequest=%s", tc.name, key, path, body)
					assert.InDelta(t, want, got, 1e-6)
				}
				if tc.penalties[0] == "" {
					assert.NotContains(t, body, "enalty", "%s takes no penalties; sending them would be rejected", tc.name)
					assert.Contains(t, logs.String(), "param="+providers.ParamFrequencyPenalty,
						"%s must not drop penalties silently", tc.name)
				}
				if tc.topK == "" {
					assert.NotContains(t, body, "top_k", "%s takes no top_k; sending it would be rejected", tc.name)
					assert.Contains(t, logs.String(), "param="+providers.ParamTopK, "%s must not drop top_k silently", tc.name)
				} else {
					assert.Equalf(t, float64(40), cfg[tc.topK], "%s dropped %s on %s\nrequest=%s", tc.name, tc.topK, path, body)
				}
			})
		}
	}
}

// runSamplingPath drives one request path with req. Provider errors are
// ignored: the capture server's reply is minimal, and the assertions read the
// request that was sent.
func runSamplingPath(t *testing.T, p providers.Provider, path string, req providers.PredictionRequest) {
	t.Helper()
	drain := func(ch <-chan providers.StreamChunk, err error) {
		if err != nil {
			return
		}
		for range ch { //nolint:revive // draining
		}
	}
	ts, hasTools := p.(providers.ToolSupport)
	var tools providers.ProviderTools
	if hasTools {
		built, err := ts.BuildTooling([]*providers.ToolDescriptor{{
			Name: "probe", Description: "a probe tool",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`),
		}})
		require.NoError(t, err)
		tools = built
	}
	ctx := context.Background()
	switch path {
	case "predict":
		_, _ = p.Predict(ctx, req)
	case "predict_stream":
		drain(p.PredictStream(ctx, req))
	case "predict_with_tools":
		require.True(t, hasTools)
		_, _, _ = ts.PredictWithTools(ctx, req, tools, "auto")
	case "predict_stream_with_tools":
		require.True(t, hasTools)
		drain(ts.PredictStreamWithTools(ctx, req, tools, "auto"))
	}
}
