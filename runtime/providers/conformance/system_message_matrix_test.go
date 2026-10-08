package conformance_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A system-role message in Messages reaches the API's system field on every
// path, never a message with role "system": current Claude and Gemini models
// reject that role (#2222), and before the tool paths sent it as one.
func TestProviders_SystemMessageReachesSystemFieldOnAllPaths(t *testing.T) {
	const marker = "SYSTEM-MARKER-7f3a"
	cases := []struct {
		name  string
		spec  providers.ProviderSpec
		reply string
		// roleIsField marks Chat Completions, whose system field IS a message
		// with role "system".
		roleIsField bool
	}{
		{name: "openai_chat_completions", spec: providers.ProviderSpec{Type: "openai", Model: "m",
			AdditionalConfig: map[string]any{"api_mode": "completions"}}, reply: `{"choices":[]}`, roleIsField: true},
		{name: "openai_responses", spec: providers.ProviderSpec{Type: "openai", Model: "m",
			AdditionalConfig: map[string]any{"api_mode": "responses"}}, reply: `{"id":"r","object":"response","output":[]}`},
		{name: "claude", spec: providers.ProviderSpec{Type: "claude", Model: "claude-sonnet-5-5"}, reply: `{}`},
		{name: "gemini", spec: providers.ProviderSpec{Type: "gemini", Model: "gemini-3.8-flash"}, reply: `{}`},
	}
	for _, tc := range cases {
		for _, path := range []string{"predict", "predict_stream", "predict_with_tools", "predict_stream_with_tools"} {
			t.Run(tc.name+"/"+path, func(t *testing.T) {
				t.Setenv("OPENAI_API_KEY", "k")
				t.Setenv("ANTHROPIC_API_KEY", "k")
				t.Setenv("GEMINI_API_KEY", "k")
				cs := newCaptureServer(t, tc.reply)
				spec := tc.spec
				spec.ID, spec.BaseURL = tc.name+"-system", cs.srv.URL
				p, err := providers.CreateProviderFromSpec(spec)
				require.NoError(t, err)
				defer func() { _ = p.Close() }()

				runSamplingPath(t, p, path, providers.PredictionRequest{MaxTokens: 64, Messages: []types.Message{
					{Role: "system", Content: marker},
					{Role: "user", Content: "hello"},
				}})
				body := cs.lastBody()
				require.Containsf(t, body, marker, "the system message was dropped on %s", path)
				if tc.roleIsField {
					assert.Equal(t, 1, strings.Count(body, `"role":"system"`), body)
					return
				}
				assert.Falsef(t, strings.Contains(body, `"role":"system"`),
					"sent as a system-role message on %s\nrequest=%s", path, body)
			})
		}
	}
}
