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

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
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

// TestResolveAPIMode_ExplicitConfigWins pins the priority order.
func TestResolveAPIMode_ExplicitConfigWins(t *testing.T) {
	p := &Provider{model: "gemini-2.5-flash", apiMode: APIModeInteractions}
	assert.Equal(t, APIModeInteractions, p.resolveAPIMode(nil, false),
		"explicit config beats every automatic rule, including the model gate")

	p = &Provider{model: "gemini-3.7-flash", apiMode: APIModeGenerateContent}
	assert.Equal(t, APIModeGenerateContent, p.resolveAPIMode(schemaFormat(), true),
		"pinning generateContent must stop the automatic upgrade")
}

// TestResolveAPIMode_AutomaticIsNarrow keeps the automatic choice to exactly
// the case generateContent cannot serve, so no existing behavior moves.
func TestResolveAPIMode_AutomaticIsNarrow(t *testing.T) {
	p := &Provider{model: "gemini-3.7-flash"}

	assert.Equal(t, APIModeInteractions, p.resolveAPIMode(schemaFormat(), true),
		"schema + tools is the case generateContent cannot serve")

	assert.Equal(t, APIModeGenerateContent, p.resolveAPIMode(schemaFormat(), false),
		"without tools there is no conflict, so nothing needs to move")
	assert.Equal(t, APIModeGenerateContent, p.resolveAPIMode(nil, true),
		"tools without a schema is ordinary generateContent work")
	assert.Equal(t, APIModeGenerateContent, p.resolveAPIMode(
		&providers.ResponseFormat{Type: providers.ResponseFormatText}, true),
		"a text response format asks for no constraint")
}

// TestHonorsInteractionsSchema pins the model gate in BOTH directions. Gemini
// 2.5 accepts response_format on that API and ignores it, so routing it there
// would move the request and still return prose.
func TestHonorsInteractionsSchema(t *testing.T) {
	for _, m := range []string{"gemini-2.5-flash", "gemini-2.5-pro", "gemini-2.0-flash", "gemini-1.5-pro"} {
		assert.Falsef(t, honorsInteractionsSchema(m), "%s ignores response_format on interactions", m)
	}
	for _, m := range []string{"gemini-3.7-flash", "gemini-3.5-flash", "gemini-3-flash-preview", "gemini-4-future"} {
		assert.Truef(t, honorsInteractionsSchema(m),
			"%s: unrecognized models are assumed capable so new releases are not silently withheld", m)
	}
}

func TestResolveAPIMode_GateAppliesToOlderModels(t *testing.T) {
	p := &Provider{model: "gemini-2.5-flash"}
	assert.Equal(t, APIModeGenerateContent, p.resolveAPIMode(schemaFormat(), true),
		"routing 2.5 to interactions gains nothing and changes the API it uses")
}

// TestInteractionsURL covers both auth shapes: the key rides in the query for
// Google AI Studio, while Vertex authenticates by header.
func TestInteractionsURL(t *testing.T) {
	p := &Provider{baseURL: "https://generativelanguage.googleapis.com/v1beta", apiKey: "K"}
	url := p.interactionsURL()
	assert.True(t, strings.HasSuffix(url, "/interactions?key=K"), "got %s", url)
	assert.Contains(t, url, "/v1beta/interactions")
}

// TestBuildInteractionsRequest_SystemLeadsTranscript covers a shape difference:
// generateContent has a dedicated system field and this API does not, so the
// system prompt leads the transcript rather than being dropped.
func TestBuildInteractionsRequest_SystemLeadsTranscript(t *testing.T) {
	tp := NewToolProvider("g", "gemini-3.7-flash",
		"https://generativelanguage.googleapis.com/v1beta",
		providers.ProviderDefaults{MaxTokens: 512}, false)

	body := tp.buildInteractionsRequest(providers.PredictionRequest{
		System:         "be terse",
		Messages:       []types.Message{{Role: roleUser, Content: "hi"}},
		ResponseFormat: schemaFormat(),
	}, nil)

	assert.Equal(t, "gemini-3.7-flash", body.Model)
	require.NotNil(t, body.ResponseFormat)
	require.Len(t, body.Input, 2)

	first, ok := body.Input[0].(interactionsTextStep)
	require.True(t, ok, "system prompt must survive as the leading turn")
	assert.Equal(t, "be terse", first.Text)
}

func TestBuildInteractionsRequest_NoSystemNoExtraTurn(t *testing.T) {
	tp := NewToolProvider("g", "gemini-3.7-flash",
		"https://generativelanguage.googleapis.com/v1beta",
		providers.ProviderDefaults{MaxTokens: 512}, false)
	body := tp.buildInteractionsRequest(providers.PredictionRequest{
		Messages: []types.Message{{Role: roleUser, Content: "hi"}},
	}, nil)
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
		}, nil)
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
				}, nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.expect)
		})
	}
}

// TestPredictWithInteractions_RoutedFromPredictWithTools proves the routing is
// reachable from the public entry point, not just from resolveAPIMode. A
// correct decision that nothing acts on is the inert-declaration failure.
func TestPredictWithInteractions_RoutedFromPredictWithTools(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"i1","steps":[{"type":"model_output","content":[{"type":"text","text":"{}"}]}]}`))
	}))
	defer srv.Close()

	tp := interactionsTestProvider(t, srv.URL)
	tools, err := tp.BuildTooling([]*providers.ToolDescriptor{{
		Name: "probe", Description: "p", InputSchema: json.RawMessage(`{"type":"object"}`)}})
	require.NoError(t, err)

	_, _, err = tp.PredictWithTools(context.Background(), providers.PredictionRequest{
		Messages:       []types.Message{{Role: roleUser, Content: "hi"}},
		ResponseFormat: schemaFormat(),
	}, tools, "auto")
	require.NoError(t, err)

	assert.Contains(t, gotPath, "/interactions",
		"a schema alongside tools must actually reach the Interactions endpoint")
}
