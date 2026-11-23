package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func schemaFormat() *providers.ResponseFormat {
	return &providers.ResponseFormat{
		Type:       providers.ResponseFormatJSONSchema,
		JSONSchema: json.RawMessage(`{"type":"object","properties":{"a":{"type":"string"}}}`),
	}
}

// TestConfiguredAPIMode mirrors the OpenAI provider's config-first ordering: a
// provider config is the source of truth, and anything automatic is only a
// default for undeclared configs.
func TestConfiguredAPIMode(t *testing.T) {
	cases := []struct {
		name string
		cfg  map[string]any
		want APIMode
	}{
		{"nil config", nil, apiModeUnset},
		{"absent key", map[string]any{"other": "x"}, apiModeUnset},
		{"non-string", map[string]any{"api_mode": 3}, apiModeUnset},
		{"empty string", map[string]any{"api_mode": ""}, apiModeUnset},
		{"interactions", map[string]any{"api_mode": "interactions"}, APIModeInteractions},
		{"uppercase", map[string]any{"api_mode": "INTERACTIONS"}, APIModeInteractions},
		{"padded", map[string]any{"api_mode": "  interactions "}, APIModeInteractions},
		{"generate_content", map[string]any{"api_mode": "generate_content"}, APIModeGenerateContent},
		{"generatecontent", map[string]any{"api_mode": "generatecontent"}, APIModeGenerateContent},
		{"legacy alias", map[string]any{"api_mode": "legacy"}, APIModeGenerateContent},
		// Unrecognized must not silently pick an API for the caller.
		{"unknown value", map[string]any{"api_mode": "batch"}, apiModeUnset},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, configuredAPIMode(tc.cfg))
		})
	}
}

// TestApplyAPIModeConfig_WiredFromSpec covers the factory path. A
// config-reached feature applied on only one constructor is unreachable for
// half of callers while every constructor test still passes.
func TestApplyAPIModeConfig_WiredFromSpec(t *testing.T) {
	p := &Provider{}
	applyAPIModeConfig(p, providers.ProviderSpec{
		AdditionalConfig: map[string]any{"api_mode": "interactions"}})
	assert.Equal(t, APIModeInteractions, p.apiMode)

	applyAPIModeConfig(p, providers.ProviderSpec{})
	assert.Equal(t, apiModeUnset, p.apiMode, "an absent config clears rather than sticks")
}

// TestResolveAPIMode_ConfigOnly pins that only configuration picks the API:
// no model and no request shape moves a request to the Interactions API
// (#2771).
func TestResolveAPIMode_ConfigOnly(t *testing.T) {
	p := &Provider{model: "gemini-2.5-flash", apiMode: APIModeInteractions}
	assert.Equal(t, APIModeInteractions, p.resolveAPIMode(), "explicit interactions is honored")

	p = &Provider{model: "gemini-3.7-flash", apiMode: APIModeGenerateContent}
	assert.Equal(t, APIModeGenerateContent, p.resolveAPIMode(), "explicit generate_content is honored")

	p = &Provider{model: "gemini-3.7-flash"}
	assert.Equal(t, APIModeGenerateContent, p.resolveAPIMode(), "unset means generateContent")
}

// TestInteractionsURL pins that neither platform carries a credential in the
// URL. Both authenticate by header now — Vertex with a Bearer token, AI Studio
// with x-goog-api-key. The key used to ride in the query string here, which put
// it inside *url.Error on any transport failure and so into the logs.
func TestInteractionsURL(t *testing.T) {
	p := &Provider{baseURL: "https://generativelanguage.googleapis.com/v1beta", apiKey: "K"}
	url := p.interactionsURL()
	assert.True(t, strings.HasSuffix(url, "/interactions"), "got %s", url)
	assert.Contains(t, url, "/v1beta/interactions")
	assert.NotContains(t, url, "key=", "no credential in the URL: %s", url)
}

// okUnaryBody and okStreamBody are a minimal completed interaction.
const okUnaryBody = `{"id":"i1","status":"completed","steps":[
  {"type":"model_output","content":[{"type":"text","text":"OK"}]}]}`

const okStreamBody = "event: step.start\n" +
	`data: {"index":0,"step":{"type":"model_output"}}` + "\n\n" +
	"event: step.delta\n" +
	`data: {"index":0,"delta":{"type":"text","text":"OK"}}` + "\n\n" +
	"event: interaction.completed\n" +
	`data: {"interaction":{"id":"i1","status":"completed"}}` + "\n\n"

// TestInteractions_SendsSystemPromptAsSystemInstruction pins that the system
// prompt goes out in the API's system_instruction field (a string), as
// generateContent's systemInstruction, and not as a turn of the transcript,
// where the model reads it as something the user said.
func TestInteractions_SendsSystemPromptAsSystemInstruction(t *testing.T) {
	for _, stream := range []bool{false, true} {
		name := map[bool]string{false: "unary", true: "stream"}[stream]
		t.Run(name, func(t *testing.T) {
			srv := newInteractionsParityServer(t, okUnaryBody, okStreamBody)
			tp := newInteractionsParityProvider(t, uniqueProviderID("int-system"), srv.url, nil)
			runInteractionsTurn(t, tp, providers.PredictionRequest{
				System: "be terse", Messages: []types.Message{{Role: roleUser, Content: "hi"}},
			}, stream)

			var body struct {
				System *string `json:"system_instruction"`
				Input  []any   `json:"input"`
			}
			require.NoError(t, json.Unmarshal(srv.lastBody, &body))
			require.NotNil(t, body.System, "no system_instruction in %s", srv.lastBody)
			assert.Equal(t, "be terse", *body.System)
			assert.NotContains(t, canonicalJSON(t, body.Input), "be terse",
				"the system prompt must not also be a turn of the transcript")
		})
	}
}

// A request without a system prompt sends no system_instruction.
func TestInteractions_NoSystemPromptNoSystemInstruction(t *testing.T) {
	srv := newInteractionsParityServer(t, okUnaryBody, okStreamBody)
	tp := newInteractionsParityProvider(t, uniqueProviderID("int-nosystem"), srv.url, nil)
	runInteractionsTurn(t, tp, providers.PredictionRequest{
		Messages: []types.Message{{Role: roleUser, Content: "hi"}},
	}, false)
	assert.NotContains(t, string(srv.lastBody), "system_instruction")
}

func TestBuildInteractionsRequest_NoSystemNoExtraTurn(t *testing.T) {
	tp := NewToolProvider("g", "gemini-3.7-flash",
		"https://generativelanguage.googleapis.com/v1beta",
		providers.ProviderDefaults{MaxTokens: 512}, false)
	body, err := tp.buildInteractionsRequest(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: roleUser, Content: "hi"}},
	}, nil, "")
	require.NoError(t, err)
	assert.Len(t, body.Input, 1)
	assert.Nil(t, body.ResponseFormat, "no schema asked for, none sent")
}

// predictWithInteractions owns the request/response round trip, so its error
// paths matter as much as the happy one: a malformed body or an error envelope
// must surface as an error rather than an empty answer the caller trusts.

func interactionsTestProvider(t *testing.T, url string) *ToolProvider {
	t.Helper()
	t.Setenv("GEMINI_API_KEY", "test-key")
	return NewToolProvider("g-int", "gemini-3.7-flash", url,
		providers.ProviderDefaults{MaxTokens: 512}, false)
}

func TestPredictWithInteractions_ParsesAnswerToolCallsAndReasoning(t *testing.T) {
	body := `{"id":"i1","status":"completed","model":"gemini-3.7-flash","steps":[
	  {"type":"thought","signature":"SIG-1"},
	  {"type":"function_call","id":"c1","name":"probe","arguments":{"q":"x"}},
	  {"type":"model_output","content":[{"type":"text","text":"{\"ok\":true}"}]}],
	  "usage":{"total_input_tokens":10,"total_output_tokens":4}}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	tp := interactionsTestProvider(t, srv.URL)
	resp, calls, err := tp.predictWithInteractions(context.Background(),
		providers.PredictionRequest{
			Messages:       []types.Message{{Role: roleUser, Content: "hi"}},
			ResponseFormat: schemaFormat(),
		}, nil, "")
	require.NoError(t, err)

	assert.Equal(t, `{"ok":true}`, resp.Content)
	require.Len(t, calls, 1)
	assert.Equal(t, "probe", calls[0].Name)

	require.NotNil(t, resp.Reasoning)
	require.Len(t, resp.Reasoning.Opaque, 1)
	assert.Equal(t, "SIG-1", resp.Reasoning.Opaque[0].Data,
		"the signature must survive so the next round can replay it")

	require.NotNil(t, resp.CostInfo, "usage must produce cost info")
}

func TestPredictWithInteractions_SurfacesErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		expect string
	}{
		{
			name:   "http error",
			status: http.StatusBadRequest,
			body:   `{"error":{"message":"Unknown parameter 'call_id'","code":"invalid_request"}}`,
			expect: "call_id",
		},
		{
			name:   "unparsable body",
			status: http.StatusOK,
			body:   `{not json`,
			expect: "failed to parse",
		},
		{
			// A 200 carrying an error envelope must not read as an empty answer.
			name:   "error envelope in a 200",
			status: http.StatusOK,
			body:   `{"id":"i1","error":{"message":"quota exhausted"}}`,
			expect: "quota exhausted",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			tp := interactionsTestProvider(t, srv.URL)
			_, _, err := tp.predictWithInteractions(context.Background(),
				providers.PredictionRequest{
					Messages: []types.Message{{Role: roleUser, Content: "hi"}},
				}, nil, "")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.expect)
		})
	}
}

// TestPredictWithInteractions_RoutedFromPredictWithTools proves a configured
// Interactions API is reachable from the public entry point, not just from
// resolveAPIMode. A correct decision that nothing acts on is the
// inert-declaration failure.
func TestPredictWithInteractions_RoutedFromPredictWithTools(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"i1","steps":[{"type":"model_output","content":[{"type":"text","text":"{}"}]}]}`))
	}))
	defer srv.Close()

	tp := interactionsTestProvider(t, srv.URL)
	tp.apiMode = APIModeInteractions
	tools, err := tp.BuildTooling([]*providers.ToolDescriptor{{
		Name: "probe", Description: "p", InputSchema: json.RawMessage(`{"type":"object"}`)}})
	require.NoError(t, err)

	resp, _, err := tp.PredictWithTools(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: roleUser, Content: "hi"}},
	}, tools, "auto")
	require.NoError(t, err)

	assert.Equal(t, "{}", resp.Content)
	assert.Contains(t, gotPath, "/interactions",
		"api_mode interactions must actually reach the Interactions endpoint")
}
