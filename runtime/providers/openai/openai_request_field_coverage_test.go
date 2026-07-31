package openai

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// newFieldCoverageProvider builds a provider with defaults permissive enough
// that no field is dropped for a reason unrelated to the builder under test.
func newFieldCoverageProvider() *ToolProvider {
	return NewToolProvider(
		"test", "gpt-4o-mini", "https://api.openai.com/v1",
		providers.ProviderDefaults{MaxTokens: 64}, false, nil, nil,
	)
}

// jsonSchemaFormat is a minimal structured-output request.
func jsonSchemaFormat() *providers.ResponseFormat {
	return &providers.ResponseFormat{
		Type:       providers.ResponseFormatJSONSchema,
		JSONSchema: json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}}}`),
	}
}

// fullyPopulatedRequest sets every field of PredictionRequest that a builder
// could express, so a builder that silently drops one is visible.
func fullyPopulatedRequest() providers.PredictionRequest {
	seed := 42
	return providers.PredictionRequest{
		System:         "you are a helpful assistant",
		Messages:       []types.Message{{Role: "user", Content: "hello"}},
		Temperature:    0.5,
		TopP:           0.9,
		MaxTokens:      128,
		Seed:           &seed,
		ResponseFormat: jsonSchemaFormat(),
	}
}

// TestBuildToolRequest_SendsResponseFormat pins the first divergence in #1742.
//
// enrichRequest is the only place response_format was ever set, and
// buildToolRequest neither called it nor set the field itself — so a pack
// combining structured output with tools silently lost its response format. No
// error, no warning; the request simply was not what the caller asked for.
//
// Tools are declared here because that is the configuration that reaches this
// builder in practice, and the combination (structured output + tools) is
// exactly the one that was broken.
func TestBuildToolRequest_SendsResponseFormat(t *testing.T) {
	p := newFieldCoverageProvider()
	p.apiMode = APIModeCompletions

	req := providers.PredictionRequest{ResponseFormat: jsonSchemaFormat()}
	got := p.buildToolRequest(context.Background(), req, sampleTools(), "")

	rf, ok := got["response_format"]
	require.True(t, ok,
		"structured output must survive the chat-completions tool path; "+
			"dropping it silently returns unstructured text to a caller expecting a schema")
	assert.NotNil(t, rf)
}

// TestBuildResponsesRequest_SendsSeed pins the second divergence in #1742.
//
// buildResponsesRequest never read req.Seed, so reproducibility was silently
// lost for every Responses-mode model — the one field whose whole purpose is
// determinism.
func TestBuildResponsesRequest_SendsSeed(t *testing.T) {
	p := newFieldCoverageProvider()
	seed := 42
	req := providers.PredictionRequest{Seed: &seed}

	got := p.buildResponsesRequest(req, nil, "")

	v, ok := got["seed"]
	require.True(t, ok,
		"seed must reach the Responses API; without it a run configured for "+
			"reproducibility silently is not reproducible")
	assert.Equal(t, 42, v)
}

// chatCompletionsFieldKeys names the wire key(s) each PredictionRequest field
// maps to on the chat-completions builders. A field lists more than one key when
// the name is legitimately model-dependent — max tokens is sent as
// max_completion_tokens except on models that only accept the older max_tokens
// (addMaxTokensToRequest). Any one of the listed keys satisfies the field.
var chatCompletionsFieldKeys = map[string][]string{
	"System":         {"messages"}, // folded into the messages array as a system turn
	"Messages":       {"messages"},
	"Temperature":    {"temperature"},
	"TopP":           {"top_p"},
	"MaxTokens":      {"max_completion_tokens", "max_tokens"},
	"Seed":           {"seed"},
	"ResponseFormat": {"response_format"},
}

// responsesFieldKeys is the same mapping for the Responses API, which renames
// several fields rather than dropping them.
var responsesFieldKeys = map[string][]string{
	"System":         {"instructions"},
	"Messages":       {"input"},
	"Temperature":    {"temperature"},
	"TopP":           {"top_p"},
	"MaxTokens":      {"max_output_tokens"},
	"Seed":           {"seed"},
	"ResponseFormat": {"text"},
}

// firstPresentKey returns the first of keys present in body, and whether any was.
func firstPresentKey(body map[string]any, keys []string) (any, bool) {
	for _, k := range keys {
		if v, ok := body[k]; ok {
			return v, true
		}
	}
	return nil, false
}

// TestEveryPredictionRequestFieldReachesEveryBuilder is the permanent net #1742
// asks for, and the reason this file outlives the two fixes above.
//
// The invariant is: every field of PredictionRequest is honored by every builder
// that can express it. That is what actually matters, and — unlike a golden
// fixture — it pins nothing about the body's shape, survives provider-side API
// changes, and keys off our own type rather than OpenAI's.
//
// Four bugs of this exact shape have been patched individually (#1735, #1736,
// #1738, and the two above); each time the shape survived because nothing
// asserted the invariant itself. Adding a field to PredictionRequest now fails
// here until every builder declares it handled or explicitly not-applicable,
// which is the drift mechanism that produced all of them.
func TestEveryPredictionRequestFieldReachesEveryBuilder(t *testing.T) {
	req := fullyPopulatedRequest()

	builders := []struct {
		name string
		keys map[string][]string
		body map[string]any
	}{
		{
			name: "chat-completions non-tool (enrichRequest)",
			keys: chatCompletionsFieldKeys,
			body: func() map[string]any {
				p := newFieldCoverageProvider()
				p.apiMode = APIModeCompletions
				body := map[string]any{
					"model":    p.model,
					"messages": p.convertRequestMessagesToOpenAI(context.Background(), req),
				}
				p.enrichRequest(body, &req, "wav")
				return body
			}(),
		},
		{
			name: "chat-completions with tools (buildToolRequest)",
			keys: chatCompletionsFieldKeys,
			body: func() map[string]any {
				p := newFieldCoverageProvider()
				p.apiMode = APIModeCompletions
				return p.buildToolRequest(context.Background(), req, sampleTools(), "")
			}(),
		},
		{
			name: "responses (buildResponsesRequest)",
			keys: responsesFieldKeys,
			body: func() map[string]any {
				p := newFieldCoverageProvider()
				return p.buildResponsesRequest(req, sampleTools(), "")
			}(),
		},
	}

	for _, b := range builders {
		t.Run(b.name, func(t *testing.T) {
			for field, wireKeys := range b.keys {
				v, ok := firstPresentKey(b.body, wireKeys)
				assert.Truef(t, ok,
					"PredictionRequest.%s is silently dropped: none of %v present in the "+
						"request body. Either honor the field in this builder, or map it to "+
						"the key it legitimately uses here.", field, wireKeys)
				assert.NotNilf(t, v, "PredictionRequest.%s produced a nil %v", field, wireKeys)
			}
		})
	}
}

// TestPredictionRequestFieldCoverageIsExhaustive fails when a field is added to
// PredictionRequest without a decision being recorded above.
//
// Without it the table is a snapshot rather than a net: a new field would simply
// go unlisted, every builder check would still pass, and the next silent drop
// would ship exactly as the previous four did.
func TestPredictionRequestFieldCoverageIsExhaustive(t *testing.T) {
	// Fields that legitimately have no place in a request body.
	notApplicable := map[string]string{
		"Metadata": "provider-side routing context; never serialized to the API",
	}

	rt := reflect.TypeOf(providers.PredictionRequest{})
	for i := 0; i < rt.NumField(); i++ {
		name := rt.Field(i).Name
		_, mapped := chatCompletionsFieldKeys[name]
		_, exempt := notApplicable[name]
		assert.Truef(t, mapped || exempt,
			"PredictionRequest.%s is new and undeclared. Add it to "+
				"chatCompletionsFieldKeys and responsesFieldKeys, or to notApplicable "+
				"with the reason it is never sent.", name)

		if mapped {
			_, inResponses := responsesFieldKeys[name]
			assert.Truef(t, inResponses,
				"PredictionRequest.%s is mapped for chat-completions but not for the "+
					"Responses API; declare its key there too.", name)
		}
	}
}
