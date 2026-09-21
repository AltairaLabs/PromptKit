package claude

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decodeAdapted adapts a schema and decodes the result for inspection.
func decodeAdapted(t *testing.T, raw string) map[string]any {
	t.Helper()
	out := adaptSchemaForClaude(json.RawMessage(raw))
	var doc map[string]any
	require.NoError(t, json.Unmarshal(out, &doc), "adapted schema must stay valid JSON")
	return doc
}

// TestAdaptSchemaForClaude_NestedObjects is #2055 in miniature: the rule is
// recursive, and the reported failure was an object inside an array's items.
func TestAdaptSchemaForClaude_NestedObjects(t *testing.T) {
	doc := decodeAdapted(t, `{
	  "type": "object",
	  "properties": {
	    "items": {"type": "array", "items": {"type": "object", "properties": {"name": {"type": "string"}}}},
	    "nested": {"type": "object", "properties": {"deep": {"type": "object", "properties": {"x": {"type": "string"}}}}},
	    "choice": {"anyOf": [{"type": "object", "properties": {"a": {"type": "string"}}}, {"type": "null"}]}
	  },
	  "$defs": {"named": {"type": "object", "properties": {"n": {"type": "string"}}}}
	}`)

	assert.Equal(t, false, doc["additionalProperties"], "root")

	props := doc["properties"].(map[string]any)
	items := props["items"].(map[string]any)["items"].(map[string]any)
	assert.Equal(t, false, items["additionalProperties"], "object inside array items")

	nested := props["nested"].(map[string]any)
	assert.Equal(t, false, nested["additionalProperties"], "nested object")
	deep := nested["properties"].(map[string]any)["deep"].(map[string]any)
	assert.Equal(t, false, deep["additionalProperties"], "twice-nested object")

	branch := props["choice"].(map[string]any)["anyOf"].([]any)[0].(map[string]any)
	assert.Equal(t, false, branch["additionalProperties"], "object in an anyOf branch")

	named := doc["$defs"].(map[string]any)["named"].(map[string]any)
	assert.Equal(t, false, named["additionalProperties"], "object under $defs")
}

// TestAdaptSchemaForClaude_AdditionalPropertiesTrue covers the second
// rejection: the API refuses an explicit true as loudly as an absence
// ("'additionalProperties: true' is not supported").
func TestAdaptSchemaForClaude_AdditionalPropertiesTrue(t *testing.T) {
	doc := decodeAdapted(t,
		`{"type":"object","properties":{"a":{"type":"string"}},"additionalProperties":true}`)
	assert.Equal(t, false, doc["additionalProperties"])
}

// TestAdaptSchemaForClaude_StripsUnsupportedKeywords pins the measured
// denylist. Each of these returns a 400 naming the keyword; the live test
// re-probes them so the list cannot quietly rot.
func TestAdaptSchemaForClaude_StripsUnsupportedKeywords(t *testing.T) {
	doc := decodeAdapted(t, `{
	  "type": "object",
	  "properties": {
	    "n": {"type": "integer", "minimum": 1, "maximum": 5, "multipleOf": 2},
	    "tags": {"type": "array", "items": {"type": "string"}, "maxItems": 3, "uniqueItems": true},
	    "bag": {"type": "object", "properties": {}, "minProperties": 1, "patternProperties": {"^a": {"type": "string"}}}
	  },
	  "required": ["n"],
	  "minProperties": 1
	}`)

	props := doc["properties"].(map[string]any)
	for _, k := range []string{"minimum", "maximum", "multipleOf"} {
		assert.NotContains(t, props["n"], k)
	}
	for _, k := range []string{"maxItems", "uniqueItems"} {
		assert.NotContains(t, props["tags"], k)
	}
	for _, k := range []string{"minProperties", "patternProperties"} {
		assert.NotContains(t, props["bag"], k)
	}
	assert.NotContains(t, doc, "minProperties")

	// Supported neighbours survive — the point is a minimal rewrite, and
	// minItems/minLength/pattern/format/enum are all accepted (measured).
	assert.Equal(t, "integer", props["n"].(map[string]any)["type"])
	assert.Contains(t, props["tags"], "items")
}

// TestAdaptSchemaForClaude_KeepsStrippedConstraintsAsProse: dropping the
// keyword must not drop the instruction. The model reads descriptions, so the
// constraint survives where the grammar cannot carry it.
func TestAdaptSchemaForClaude_KeepsStrippedConstraintsAsProse(t *testing.T) {
	doc := decodeAdapted(t, `{
	  "type": "object",
	  "properties": {
	    "n": {"type": "integer", "minimum": 1, "maximum": 5, "description": "How many."},
	    "m": {"type": "integer", "minimum": 2}
	  }
	}`)

	props := doc["properties"].(map[string]any)
	assert.Equal(t, "How many. Constraints: minimum 1, maximum 5.",
		props["n"].(map[string]any)["description"])
	assert.Equal(t, "Constraints: minimum 2.",
		props["m"].(map[string]any)["description"],
		"a node with no description gets one rather than losing the constraint")
}

// TestAdaptSchemaForClaude_IsIdempotent matters because a retry re-adapts the
// same schema: a second pass must not append a second constraint sentence or
// otherwise drift the request bytes, which are a prompt-cache key.
func TestAdaptSchemaForClaude_IsIdempotent(t *testing.T) {
	raw := json.RawMessage(`{
	  "type": "object",
	  "properties": {"n": {"type": "integer", "minimum": 1, "description": "How many."}},
	  "required": ["n"]
	}`)

	once := adaptSchemaForClaude(raw)
	twice := adaptSchemaForClaude(once)
	assert.Equal(t, string(once), string(twice))
}

// TestAdaptSchemaForClaude_OneOfBecomesAnyOf: oneOf is rejected as a keyword,
// but stripping it would leave the node with no constraint at all. anyOf is
// accepted and means the same thing for generation.
func TestAdaptSchemaForClaude_OneOfBecomesAnyOf(t *testing.T) {
	doc := decodeAdapted(t,
		`{"type":"object","properties":{"v":{"oneOf":[{"type":"string"},{"type":"number"}]}}}`)

	v := doc["properties"].(map[string]any)["v"].(map[string]any)
	assert.NotContains(t, v, "oneOf")
	require.Contains(t, v, "anyOf")
	assert.Len(t, v["anyOf"], 2, "the branches must survive the rename")
}

// TestAdaptSchemaForClaude_LeavesRequiredAlone. Anthropic, unlike OpenAI
// strict mode, accepts a partial required list (verified live), so there is no
// reason to make a caller's optional field mandatory.
func TestAdaptSchemaForClaude_LeavesRequiredAlone(t *testing.T) {
	doc := decodeAdapted(t,
		`{"type":"object","properties":{"a":{"type":"string"},"b":{"type":"string"}},"required":["a"]}`)

	assert.Equal(t, []any{"a"}, doc["required"])
}

// TestAdaptSchemaForClaude_CompliantSchemaIsUntouched keeps the common case
// byte-identical: re-encoding reorders keys for no reason, and the request
// body is a prompt-cache key.
func TestAdaptSchemaForClaude_CompliantSchemaIsUntouched(t *testing.T) {
	raw := json.RawMessage(
		`{"type":"object","properties":{"a":{"type":"string"}},"required":["a"],"additionalProperties":false}`)

	assert.Equal(t, string(raw), string(adaptSchemaForClaude(raw)))
}

func TestAdaptSchemaForClaude_Degenerate(t *testing.T) {
	assert.Empty(t, adaptSchemaForClaude(nil))

	broken := json.RawMessage(`{"type":`)
	assert.Equal(t, string(broken), string(adaptSchemaForClaude(broken)),
		"an unparseable schema is the API's error to report")
}

// TestAdaptSchemaForClaude_KeepsNotWhenStrippingWouldEmptyTheNode.
//
// "not" is rejected as a keyword, but "anything except X" has no equivalent in
// the accepted subset, and a node stripped down to {} is rejected too —
// "Empty schema ({}) that accepts any JSON value is not supported". Removing
// it would swap an error that names the cause for one that does not.
func TestAdaptSchemaForClaude_KeepsNotWhenStrippingWouldEmptyTheNode(t *testing.T) {
	doc := decodeAdapted(t,
		`{"type":"object","properties":{"v":{"not":{"type":"number"}}}}`)

	v := doc["properties"].(map[string]any)["v"].(map[string]any)
	assert.Contains(t, v, "not",
		"a node with nothing left to say must keep the keyword the API can name")
}

// TestAdaptSchemaForClaude_StripsNotWhenATypeSurvives — the carve-out is about
// the node ending up typeless, not about "not" being special.
func TestAdaptSchemaForClaude_StripsNotWhenATypeSurvives(t *testing.T) {
	doc := decodeAdapted(t,
		`{"type":"object","properties":{"v":{"type":"string","not":{"const":"x"}}}}`)

	v := doc["properties"].(map[string]any)["v"].(map[string]any)
	assert.NotContains(t, v, "not")
	assert.Equal(t, "string", v["type"])
}
