package conformance_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// additional_config.extra_body is merged into the request body of the
// OpenAI-compatible chat providers (#2065). Like any request field, it has to
// reach the wire on all four paths of every provider that accepts it, and it
// must not override a field the provider sets itself. Providers are built
// through CreateProviderFromSpec, as a config loader builds them, so the test
// covers the factory wiring as well as the request builders.
func TestProviders_ExtraBodyReachesWireOnAllPaths(t *testing.T) {
	const openAIReply = `{"id":"c","object":"chat.completion","model":"m","choices":` +
		`[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],` +
		`"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`

	cases := []struct {
		name     string
		spec     providers.ProviderSpec
		apiMode  string
		response string
	}{
		{name: "openai_chat_completions", spec: providers.ProviderSpec{Type: "openai", Model: "gpt-4o-mini"},
			apiMode: "completions", response: openAIReply},
		{name: "openai_responses", spec: providers.ProviderSpec{Type: "openai", Model: "gpt-4o-mini"},
			apiMode: "responses", response: `{"id":"r","object":"response","output":[]}`},
		{name: "vllm", spec: providers.ProviderSpec{Type: "vllm", Model: "qwen3"}, response: openAIReply},
		{name: "ollama", spec: providers.ProviderSpec{Type: "ollama", Model: "llama3"}, response: openAIReply},
	}
	paths := []string{"predict", "predict_stream", "predict_with_tools", "predict_stream_with_tools"}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, path := range paths {
				t.Run(path, func(t *testing.T) {
					t.Setenv("OPENAI_API_KEY", "test-key")
					cs := newCaptureServer(t, tc.response)
					spec := tc.spec
					spec.ID = tc.name + "-extra-body"
					spec.BaseURL = cs.srv.URL
					spec.Defaults = providers.ProviderDefaults{MaxTokens: 256}
					spec.AdditionalConfig = map[string]any{
						providers.ExtraBodyConfigKey: map[string]any{
							"chat_template_kwargs": map[string]any{"enable_thinking": false},
							"model":                "hijacked-model",
						},
					}
					if tc.apiMode != "" {
						spec.AdditionalConfig["api_mode"] = tc.apiMode
					}
					p, err := providers.CreateProviderFromSpec(spec)
					require.NoError(t, err)
					defer func() { _ = p.Close() }()

					exercisePath(t, p, path)

					body := cs.lastBody()
					require.NotEmptyf(t, body, "%s/%s sent no request at all", tc.name, path)
					var sent map[string]any
					require.NoError(t, json.Unmarshal([]byte(body), &sent), "request=%s", body)

					assert.Equalf(t, map[string]any{"enable_thinking": false}, sent["chat_template_kwargs"],
						"%s dropped extra_body on the %s path\nrequest=%s", tc.name, path, body)
					assert.Equalf(t, spec.Model, sent["model"],
						"extra_body overrode a field the provider sets itself on %s/%s", tc.name, path)
				})
			}
		})
	}
}
