package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// PredictionRequest.ActiveTools restricts a call to some of the tools it is
// sent, without changing the tools array, through tool_choice allowed_tools.
// The two APIs nest it differently:
//
//   - Chat Completions: {"type":"allowed_tools","allowed_tools":{"mode":...,
//     "tools":[{"type":"function","function":{"name":...}}]}}
//     (https://developers.openai.com/api/reference/resources/chat/subresources/completions/methods/create,
//     ChatCompletionAllowedToolChoice)
//   - Responses: {"type":"allowed_tools","mode":...,
//     "tools":[{"type":"function","name":...}]}
//     (https://developers.openai.com/api/reference/resources/responses/methods/create,
//     ToolChoiceAllowed)
//
// mode is "auto" or "required" on both.

const (
	chatOKBody      = `{"id":"1","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`
	responsesOKBody = `{"id":"resp_1","object":"response","status":"completed",` +
		`"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],` +
		`"usage":{"input_tokens":5,"output_tokens":1,"total_tokens":6}}`
)

// toServer sends every request to server, whatever host it names, so the
// adapter can be configured with OpenAI's own base URL.
type toServer struct{ server *httptest.Server }

func (s toServer) RoundTrip(r *http.Request) (*http.Response, error) {
	target, _ := url.Parse(s.server.URL)
	r = r.Clone(r.Context())
	r.URL.Scheme, r.URL.Host, r.Host = target.Scheme, target.Host, target.Host
	return http.DefaultTransport.RoundTrip(r)
}

// openAIBaseURL is OpenAI's own API, the only host allowed_tools is sent to.
const openAIBaseURL = "https://api.openai.com/v1"

// allowedToolsBody sends one tool call through the real adapter, in apiMode,
// against OpenAI's own API, and returns the request body the server received.
func allowedToolsBody(
	t *testing.T, apiMode APIMode, platform string, active []string, choice string,
) map[string]any {
	t.Helper()
	return allowedToolsBodyAt(t, openAIBaseURL, apiMode, platform, active, choice)
}

// allowedToolsBodyAt is allowedToolsBody for a provider configured with
// baseURL.
func allowedToolsBodyAt(
	t *testing.T, baseURL string, apiMode APIMode, platform string, active []string, choice string,
) map[string]any {
	t.Helper()
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		if apiMode == APIModeResponses {
			_, _ = w.Write([]byte(responsesOKBody))
			return
		}
		_, _ = w.Write([]byte(chatOKBody))
	}))
	t.Cleanup(server.Close)
	t.Setenv("OPENAI_API_KEY", "k")
	tp := NewToolProvider("allowed", "gpt-test", baseURL, providers.ProviderDefaults{MaxTokens: 64}, false, nil, nil)
	tp.SetHTTPTransport(toServer{server: server})
	tp.apiMode = apiMode
	tp.platform = platform
	tools, err := tp.BuildTooling([]*providers.ToolDescriptor{
		{Name: "lookup_order", Description: "l", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "refund", Description: "r", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "search", Description: "s", InputSchema: json.RawMessage(`{"type":"object"}`)},
	})
	require.NoError(t, err)
	_, _, err = tp.PredictWithTools(context.Background(), providers.PredictionRequest{
		Messages:    []types.Message{{Role: "user", Content: "hi"}},
		ActiveTools: active,
	}, tools, choice)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(body, &got))
	require.Len(t, got["tools"], 3, "the whole array is sent")
	return got
}

func toolChoiceJSON(t *testing.T, body map[string]any) string {
	t.Helper()
	b, err := json.Marshal(body["tool_choice"])
	require.NoError(t, err)
	return string(b)
}

func TestAllowedTools_ChatCompletions(t *testing.T) {
	active := []string{"lookup_order", "refund"}
	for _, tc := range []struct {
		name, choice, want string
	}{
		{"auto", "auto", `{"type":"allowed_tools","allowed_tools":{"mode":"auto","tools":[` +
			`{"type":"function","function":{"name":"lookup_order"}},{"type":"function","function":{"name":"refund"}}]}}`},
		{"unset", "", `{"type":"allowed_tools","allowed_tools":{"mode":"auto","tools":[` +
			`{"type":"function","function":{"name":"lookup_order"}},{"type":"function","function":{"name":"refund"}}]}}`},
		{"required", "required", `{"type":"allowed_tools","allowed_tools":{"mode":"required","tools":[` +
			`{"type":"function","function":{"name":"lookup_order"}},{"type":"function","function":{"name":"refund"}}]}}`},
		{"forced tool", "refund", `{"type":"function","function":{"name":"refund"}}`},
		{"none", "none", `"none"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := allowedToolsBody(t, APIModeCompletions, "", active, tc.choice)
			assert.JSONEq(t, tc.want, toolChoiceJSON(t, body))
		})
	}
}

func TestAllowedTools_Responses(t *testing.T) {
	active := []string{"lookup_order", "refund"}
	for _, tc := range []struct {
		name, choice, want string
	}{
		{"auto", "auto", `{"type":"allowed_tools","mode":"auto","tools":[` +
			`{"type":"function","name":"lookup_order"},{"type":"function","name":"refund"}]}`},
		{"required", "required", `{"type":"allowed_tools","mode":"required","tools":[` +
			`{"type":"function","name":"lookup_order"},{"type":"function","name":"refund"}]}`},
		{"forced tool", "refund", `{"type":"function","name":"refund"}`},
		{"none", "none", `"none"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := allowedToolsBody(t, APIModeResponses, "", active, tc.choice)
			assert.JSONEq(t, tc.want, toolChoiceJSON(t, body))
		})
	}
}

// An empty restriction leaves the model no tool to call: tool_choice none,
// never an allowed_tools list with nothing in it.
func TestAllowedTools_EmptyActiveIsNone(t *testing.T) {
	for _, mode := range []APIMode{APIModeCompletions, APIModeResponses} {
		body := allowedToolsBody(t, mode, "", []string{}, "auto")
		assert.Equal(t, `"none"`, toolChoiceJSON(t, body), mode)
	}
}

// Without a restriction the request is what it always was: no tool_choice
// for auto.
func TestAllowedTools_NilActiveSendsNoRestriction(t *testing.T) {
	for _, mode := range []APIMode{APIModeCompletions, APIModeResponses} {
		body := allowedToolsBody(t, mode, "", nil, "auto")
		assert.NotContains(t, body, "tool_choice", mode)
	}
}

// On a hyperscaler platform allowed_tools is not sent: whether Azure and
// Bedrock take it is not documented, and the pipeline refuses a call outside
// the restriction anyway.
func TestAllowedTools_NotSentOnPlatforms(t *testing.T) {
	body := allowedToolsBody(t, APIModeCompletions, azurePlatform, []string{"refund"}, "auto")
	assert.NotContains(t, body, "tool_choice")
}

// An OpenAI-compatible server (vLLM, Groq, OpenRouter, LiteLLM, ...) is not
// sent allowed_tools: whether it takes it is up to each server, and the
// pipeline refuses a call outside the restriction anyway.
func TestAllowedTools_NotSentToOtherHosts(t *testing.T) {
	for _, mode := range []APIMode{APIModeCompletions, APIModeResponses} {
		body := allowedToolsBodyAt(t, "https://openrouter.example/api/v1", mode, "", []string{"refund"}, "auto")
		assert.NotContains(t, body, "tool_choice", mode)
	}
}
