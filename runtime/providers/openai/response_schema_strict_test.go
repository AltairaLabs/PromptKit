package openai

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// nestedResponseSchema omits additionalProperties on the object inside the
// array — the #2055 shape. Strict mode rejects it.
const nestedResponseSchema = `{
  "type": "object",
  "properties": {
    "items": {"type": "array", "items": {"type": "object", "properties": {"name": {"type": "string"}}}}
  },
  "required": ["items"]
}`

func strictSchemaOf(t *testing.T, raw string, strict bool) map[string]any {
	t.Helper()
	out := strictResponseSchema(&providers.ResponseFormat{
		Type:       providers.ResponseFormatJSONSchema,
		JSONSchema: json.RawMessage(raw),
		Strict:     strict,
	})
	var doc map[string]any
	require.NoError(t, json.Unmarshal(out, &doc))
	return doc
}

// TestStrictResponseSchema_NormalizesNestedObjects is the response-format half
// of #2055: BuildTooling has run ensureStrictSchema over tool schemas since
// strict mode arrived, and both response-format builders posted the caller's
// schema verbatim.
func TestStrictResponseSchema_NormalizesNestedObjects(t *testing.T) {
	doc := strictSchemaOf(t, nestedResponseSchema, true)

	assert.Equal(t, false, doc["additionalProperties"], "root")

	items := doc["properties"].(map[string]any)["items"].(map[string]any)["items"].(map[string]any)
	assert.Equal(t, false, items["additionalProperties"],
		"the object inside the array is what the API rejects")
	assert.Equal(t, []any{"name"}, items["required"])
}

// TestStrictResponseSchema_NonStrictIsVerbatim: without strict: true OpenAI
// enforces neither rule and treats the schema as guidance, so rewriting it
// would change the caller's contract for no gain.
func TestStrictResponseSchema_NonStrictIsVerbatim(t *testing.T) {
	raw := json.RawMessage(nestedResponseSchema)
	out := strictResponseSchema(&providers.ResponseFormat{
		Type:       providers.ResponseFormatJSONSchema,
		JSONSchema: raw,
	})
	assert.Equal(t, string(raw), string(out))
}

func TestStrictResponseSchema_Degenerate(t *testing.T) {
	// No response format at all — the early return exists so reading
	// rf.JSONSchema does not dereference nil.
	assert.Empty(t, string(strictResponseSchema(nil)))

	// Strict with no schema: nothing to rewrite, and the builder must not
	// invent one (an empty object would constrain the model to `{}`).
	assert.Equal(t, "", string(strictResponseSchema(&providers.ResponseFormat{
		Type:   providers.ResponseFormatJSONSchema,
		Strict: true,
	})))
}

// TestEnsureStrictSchema_ReachesEveryNestedObject pins the traversal fix. The
// previous recursion descended through "properties" and a map-valued "items"
// only, so these three shapes reached the API non-compliant.
func TestEnsureStrictSchema_ReachesEveryNestedObject(t *testing.T) {
	out := ensureStrictSchema(json.RawMessage(`{
	  "type": "object",
	  "properties": {
	    "ref": {"$ref": "#/$defs/named"},
	    "choice": {"anyOf": [{"type": "object", "properties": {"a": {"type": "string"}}}]},
	    "tuple": {"type": "array", "prefixItems": [{"type": "object", "properties": {"t": {"type": "string"}}}]}
	  },
	  "$defs": {"named": {"type": "object", "properties": {"n": {"type": "string"}}}}
	}`))

	var doc map[string]any
	require.NoError(t, json.Unmarshal(out, &doc))
	props := doc["properties"].(map[string]any)

	named := doc["$defs"].(map[string]any)["named"].(map[string]any)
	assert.Equal(t, false, named["additionalProperties"], "$defs")

	branch := props["choice"].(map[string]any)["anyOf"].([]any)[0].(map[string]any)
	assert.Equal(t, false, branch["additionalProperties"], "anyOf branch")

	tuple := props["tuple"].(map[string]any)["prefixItems"].([]any)[0].(map[string]any)
	assert.Equal(t, false, tuple["additionalProperties"], "prefixItems entry")
}

// TestEnsureStrictSchema_RequiredIsDeterministic: required used to be built
// from map iteration, so the same schema produced different request bytes on
// different calls and never hit the prompt cache.
func TestEnsureStrictSchema_RequiredIsDeterministic(t *testing.T) {
	raw := json.RawMessage(
		`{"type":"object","properties":{"z":{"type":"string"},"a":{"type":"string"},"m":{"type":"string"}}}`)

	first := string(ensureStrictSchema(raw))
	for i := 0; i < 20; i++ {
		assert.Equal(t, first, string(ensureStrictSchema(raw)))
	}

	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(first), &doc))
	assert.Equal(t, []any{"a", "m", "z"}, doc["required"])
}

// TestConvertResponseFormat_AppliesStrictRules checks the wire builder, not
// just the helper — config-reached behavior is what broke.
func TestConvertResponseFormat_AppliesStrictRules(t *testing.T) {
	p := NewProvider("test", "gpt-4o-mini", "https://api.openai.com/v1",
		providers.ProviderDefaults{}, false)

	got := p.convertResponseFormat(&providers.ResponseFormat{
		Type:       providers.ResponseFormatJSONSchema,
		JSONSchema: json.RawMessage(nestedResponseSchema),
		Strict:     true,
	})
	require.NotNil(t, got)
	require.NotNil(t, got.JSONSchema)

	encoded, err := json.Marshal(got.JSONSchema.Schema)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"additionalProperties":false`)
}

// TestBuildResponsesFormat_AppliesStrictRules covers the Responses API
// serializer. OpenAI has two message serializers and two response-format
// builders; fixing one and not the other is how #1735 happened.
func TestBuildResponsesFormat_AppliesStrictRules(t *testing.T) {
	p := NewProvider("test", "gpt-5", "https://api.openai.com/v1",
		providers.ProviderDefaults{}, false)

	got := p.convertResponseFormatToResponses(&providers.ResponseFormat{
		Type:       providers.ResponseFormatJSONSchema,
		JSONSchema: json.RawMessage(nestedResponseSchema),
		Strict:     true,
	})
	require.NotNil(t, got)

	encoded, err := json.Marshal(got)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"additionalProperties":false`)
}
