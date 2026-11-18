package openai

import (
	"context"
	"encoding/json"
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

// Issue #2758. Strict mode accepts a subset of JSON Schema. A tool whose
// schema falls outside it either 400s the whole request (a keyword strict mode
// refuses) or is silently rewritten into a different contract (a map-typed
// object emptied to properties: {} + additionalProperties: false). Such a tool
// must reach the wire non-strict, with its schema exactly as written, while a
// strict-compatible tool beside it keeps strict mode.

// dreamProposeSchema is the issue's repro: a string→string map with
// minProperties/maxProperties, which strict mode refuses.
const dreamProposeSchema = `{"type":"object","additionalProperties":false,"required":["prompts"],
 "properties":{"prompts":{"type":"object","minProperties":1,"maxProperties":32,
   "additionalProperties":{"type":"string","minLength":1}}}}`

// strictFallbackReply answers either API with a plain text turn.
func strictFallbackReply(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if strings.HasSuffix(r.URL.Path, responsesAPIPath) {
		_, _ = w.Write([]byte(`{"id":"resp_1","status":"completed","output":[{"type":"message",` +
			`"id":"m1","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],` +
			`"usage":{"input_tokens":1,"output_tokens":1}}`))
		return
	}
	_, _ = w.Write([]byte(`{"id":"1","choices":[{"message":{"role":"assistant","content":"ok"},` +
		`"finish_reason":"stop"}]}`))
}

// sentTools builds tooling for descs on a provider made from spec, sends one
// tool turn, and returns the "tools" array of the request body the API got.
func sentTools(t *testing.T, spec providers.ProviderSpec, descs []*providers.ToolDescriptor) string {
	t.Helper()
	return string(sentRequest(t, spec, descs, "auto")["tools"])
}

// sentRequest sends one tool turn with toolChoice and returns the top-level
// fields of the request body the API got.
func sentRequest(
	t *testing.T, spec providers.ProviderSpec, descs []*providers.ToolDescriptor, toolChoice string,
) map[string]json.RawMessage {
	t.Helper()
	var (
		mu   sync.Mutex
		body []byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		body = b
		mu.Unlock()
		strictFallbackReply(w, r)
	}))
	t.Cleanup(srv.Close)

	spec.BaseURL = srv.URL
	if spec.PlatformConfig != nil {
		spec.PlatformConfig.Endpoint = srv.URL
	}
	p, err := providers.CreateProviderFromSpec(spec)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })
	tp, ok := p.(*ToolProvider)
	require.True(t, ok, "openai spec must build a ToolProvider, got %T", p)

	tools, err := tp.BuildTooling(descs)
	require.NoError(t, err)
	_, _, err = tp.PredictWithTools(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	}, tools, toolChoice)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	var got map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &got), "body=%s", body)
	return got
}

func openAISpec(mode string) providers.ProviderSpec {
	return providers.ProviderSpec{
		ID: "strict-fallback", Type: "openai", Model: "m",
		AdditionalConfig: map[string]any{"api_mode": mode},
	}
}

func azureSpec() providers.ProviderSpec {
	return providers.ProviderSpec{
		ID: "strict-fallback-azure", Type: "openai", Model: "deployment",
		Credential: &tokenCredential{}, Platform: "azure",
		PlatformConfig: &providers.PlatformConfig{Type: "azure"},
	}
}

func desc(name, schema string) *providers.ToolDescriptor {
	return &providers.ToolDescriptor{Name: name, Description: "d", InputSchema: json.RawMessage(schema)}
}

// The issue's repro, on every serializer the openai provider has: the
// Chat Completions body (OpenAI and Azure OpenAI) and the Responses body.
func TestBuildTooling_StrictIncompatibleToolIsSentNonStrictVerbatim(t *testing.T) {
	wantChat := `[{"type":"function","function":{"name":"dream_propose","description":"d",` +
		`"parameters":` + dreamProposeSchema + `}}]`
	// Responses says strict: false outright: with the key omitted, OpenAI's
	// function-calling guide says the API tries to normalize the schema into
	// strict mode, and to "set strict: false explicitly" to keep it non-strict.
	wantResponses := `[{"type":"function","name":"dream_propose","description":"d","strict":false,` +
		`"parameters":` + dreamProposeSchema + `}]`

	cases := []struct {
		name string
		spec providers.ProviderSpec
		want string
	}{
		{"chat completions", openAISpec("completions"), wantChat},
		{"responses", openAISpec("responses"), wantResponses},
		{"azure openai", azureSpec(), wantChat},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sentTools(t, tc.spec, []*providers.ToolDescriptor{desc("dream_propose", dreamProposeSchema)})
			assert.JSONEq(t, tc.want, got)
		})
	}
}

// One incompatible tool does not cost the others strict mode: the compliant
// tool beside it is still rewritten and sent strict.
func TestBuildTooling_StrictFallbackIsPerTool(t *testing.T) {
	got := sentTools(t, openAISpec("completions"), []*providers.ToolDescriptor{
		desc("dream_propose", dreamProposeSchema),
		desc("get_weather", `{"type":"object","properties":{"city":{"type":"string"}}}`),
	})
	assert.JSONEq(t, `[
	  {"type":"function","function":{"name":"dream_propose","description":"d","parameters":`+dreamProposeSchema+`}},
	  {"type":"function","function":{"name":"get_weather","description":"d","strict":true,
	    "parameters":{"type":"object","properties":{"city":{"type":"string"}},
	      "additionalProperties":false,"required":["city"]}}}
	]`, got)
}

// Each shape strict mode cannot express sends its tool non-strict and
// verbatim; each shape it can express keeps strict mode.
func TestBuildTooling_StrictCompatibilityIsDecidedBySchema(t *testing.T) {
	nonStrict := map[string]string{
		// A map-typed object: rewriting it to properties:{} destroys it.
		"map object": `{"type":"object","properties":{"labels":{"type":"object",` +
			`"additionalProperties":{"type":"string"}}}}`,
		"additionalProperties true": `{"type":"object","properties":{"a":{"type":"string"}},` +
			`"additionalProperties":true}`,
		"unsupported format": `{"type":"object","properties":{"u":{"type":"string","format":"uri"}}}`,
		"oneOf":              `{"type":"object","properties":{"v":{"oneOf":[{"type":"string"},{"type":"number"}]}}}`,
		"allOf under $defs": `{"type":"object","properties":{"v":{"$ref":"#/$defs/v"}},` +
			`"$defs":{"v":{"allOf":[{"type":"string"}]}}}`,
		"uniqueItems in anyOf branch": `{"type":"object","properties":{"v":{"anyOf":[` +
			`{"type":"array","items":{"type":"string"},"uniqueItems":true},{"type":"null"}]}}}`,
		// Tuple-form items: strict mode requires items to be one schema.
		"tuple items": `{"type":"object","properties":{"pair":{"type":"array",` +
			`"items":[{"type":"string"},{"type":"integer"}]}}}`,
		// draft-07 spelling of dependentRequired / dependentSchemas.
		"dependencies": `{"type":"object","properties":{"a":{"type":"string"},"b":{"type":"string"}},` +
			`"dependencies":{"a":["b"]}}`,
		// A nested free-form object: closing it would accept no keys at all.
		"nested free-form object": `{"type":"object","properties":{"meta":{"type":"object"}}}`,
		"nested object with empty properties": `{"type":"object","properties":{"meta":{"type":"object",` +
			`"properties":{}}}}`,
		"free-form object in array items": `{"type":"object","properties":{"rows":{"type":"array",` +
			`"items":{"type":"object"}}}}`,
	}
	for name, schema := range nonStrict {
		t.Run("non-strict: "+name, func(t *testing.T) {
			got := sentTools(t, openAISpec("completions"), []*providers.ToolDescriptor{desc("t", schema)})
			assert.JSONEq(t, `[{"type":"function","function":{"name":"t","description":"d","parameters":`+
				schema+`}}]`, got)
		})
	}

	strict := map[string]struct{ schema, want string }{
		// A forbidden keyword's NAME used as a property name is data, not a keyword.
		"keyword-named property": {
			`{"type":"object","properties":{"minProperties":{"type":"integer"}}}`,
			`{"type":"object","properties":{"minProperties":{"type":"integer"}},` +
				`"additionalProperties":false,"required":["minProperties"]}`,
		},
		"supported keywords": {
			`{"type":"object","properties":{"e":{"type":"string","format":"email","minLength":1},` +
				`"n":{"type":"integer","minimum":0},"l":{"type":"array","items":{"type":"string"},"minItems":1}}}`,
			`{"type":"object","properties":{"e":{"type":"string","format":"email","minLength":1},` +
				`"n":{"type":"integer","minimum":0},"l":{"type":"array","items":{"type":"string"},"minItems":1}},` +
				`"additionalProperties":false,"required":["e","l","n"]}`,
		},
		// A root object with no properties is a no-argument tool, not a map.
		"no-argument tool, bare root": {
			`{"type":"object"}`,
			`{"type":"object","properties":{},"additionalProperties":false,"required":[]}`,
		},
		"no-argument tool, empty root properties": {
			`{"type":"object","properties":{}}`,
			`{"type":"object","properties":{},"additionalProperties":false,"required":[]}`,
		},
		// A nested object that declares its properties is closed, not free-form.
		"nested object with properties": {
			`{"type":"object","properties":{"o":{"type":"object","properties":{"k":{"type":"string"}}}}}`,
			`{"type":"object","properties":{"o":{"type":"object","properties":{"k":{"type":"string"}},` +
				`"additionalProperties":false,"required":["k"]}},"additionalProperties":false,"required":["o"]}`,
		},
	}
	for name, tc := range strict {
		t.Run("strict: "+name, func(t *testing.T) {
			got := sentTools(t, openAISpec("completions"), []*providers.ToolDescriptor{desc("t", tc.schema)})
			assert.JSONEq(t, `[{"type":"function","function":{"name":"t","description":"d","strict":true,`+
				`"parameters":`+tc.want+`}}]`, got)
		})
	}
}
