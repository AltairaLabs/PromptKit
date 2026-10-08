package conformance_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// rejectingServer answers 400 with the API's own error text for the first
// rejected parameter a request still carries, and reply once it carries none:
// the way current OpenAI and Claude models treat sampling parameters.
type rejectingServer struct {
	mu     sync.Mutex
	bodies []string
	srv    *httptest.Server
}

// rejection pairs a wire key with the 400 body an API sends for it.
type rejection struct{ key, body string }

func newRejectingServer(t *testing.T, reply string, rejections []rejection) *rejectingServer {
	t.Helper()
	rs := &rejectingServer{}
	rs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := string(raw)
		rs.mu.Lock()
		rs.bodies = append(rs.bodies, body)
		rs.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		for _, rej := range rejections {
			if strings.Contains(body, `"`+rej.key+`"`) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(rej.body))
				return
			}
		}
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(rs.srv.Close)
	return rs
}

func (rs *rejectingServer) requests() []string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return append([]string(nil), rs.bodies...)
}

// A provider whose model rejects a sampling parameter learns it from the 400,
// retries without it, and never sends it again: the call the prompt made
// succeeds instead of failing (#2220). The error bodies are the ones the live
// APIs returned for gpt-6.1-sol and claude-sonnet-5-5.
func TestProviders_RetryWithoutRejectedSamplingParams(t *testing.T) {
	const openAITemp = `{"error":{"message":"Unsupported value: 'temperature' does not support 0 with this model. ` +
		`Only the default (1) value is supported.","type":"invalid_request_error","param":"temperature"}}`
	const openAITopP = `{"error":{"message":"Unsupported parameter: 'top_p' is not supported with this model.",` +
		`"type":"invalid_request_error","param":"top_p"}}`
	const claudeTemp = "{\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\"," +
		"\"message\":\"`temperature` is deprecated for this model.\"}}"
	const claudeTopK = "{\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\"," +
		"\"message\":\"`top_k` is deprecated for this model.\"}}"
	const openAIReply = `{"id":"c","object":"chat.completion","model":"m","choices":` +
		`[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],` +
		`"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	const claudeReply = `{"id":"m","type":"message","role":"assistant","model":"c",` +
		`"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`

	cases := []struct {
		name       string
		spec       providers.ProviderSpec
		reply      string
		rejections []rejection
	}{
		{name: "openai_chat_completions", spec: providers.ProviderSpec{Type: "openai", Model: "gpt-6.1-sol",
			AdditionalConfig: map[string]any{"api_mode": "completions"}}, reply: openAIReply,
			rejections: []rejection{{"temperature", openAITemp}, {"top_p", openAITopP}}},
		{name: "openai_responses", spec: providers.ProviderSpec{Type: "openai", Model: "gpt-6.1-sol",
			AdditionalConfig: map[string]any{"api_mode": "responses"}}, reply: `{"id":"r","object":"response","output":[]}`,
			rejections: []rejection{{"temperature", openAITemp}, {"top_p", openAITopP}}},
		{name: "claude", spec: providers.ProviderSpec{Type: "claude", Model: "claude-sonnet-5-5"}, reply: claudeReply,
			rejections: []rejection{{"temperature", claudeTemp}, {"top_k", claudeTopK}}},
	}
	paths := []string{"predict", "predict_stream", "predict_with_tools", "predict_stream_with_tools"}
	topK := 20

	for _, tc := range cases {
		for _, path := range paths {
			t.Run(tc.name+"/"+path, func(t *testing.T) {
				t.Setenv("OPENAI_API_KEY", "test-key")
				t.Setenv("ANTHROPIC_API_KEY", "test-key")
				rs := newRejectingServer(t, tc.reply, tc.rejections)
				spec := tc.spec
				spec.ID = tc.name + "-" + path + "-rejecting"
				spec.BaseURL = rs.srv.URL
				spec.Defaults = providers.ProviderDefaults{MaxTokens: 256}
				p, err := providers.CreateProviderFromSpec(spec)
				require.NoError(t, err)
				defer func() { _ = p.Close() }()

				req := providers.PredictionRequest{
					Messages:  []types.Message{{Role: "user", Content: "hello"}},
					MaxTokens: 256, Temperature: 0, TemperatureSet: true, TopP: 0.9, TopK: &topK,
				}
				runSamplingPath(t, p, path, req)
				first := rs.requests()
				require.Len(t, first, 3, "one 400 per rejected parameter, then the call that succeeds")
				for _, rej := range tc.rejections {
					assert.NotContainsf(t, first[2], `"`+rej.key+`"`, "the successful retry still sent %s", rej.key)
				}

				runSamplingPath(t, p, path, req)
				assert.Len(t, rs.requests(), 4, "a learned rejection is not sent, so no second 400")
			})
		}
	}
}
