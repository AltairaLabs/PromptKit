package gemini

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// TestGeminiBuildTooling_SanitizesParameters is the tool half of #2055.
//
// responseSchema has been sanitized since it was added; function-declaration
// parameters never were, and they go through the same OpenAPI-subset parser:
//
//	Unknown name "additionalProperties" at
//	'tools[0].function_declarations[0].parameters': Cannot find field
//
// A pack tool schema carries additionalProperties because OpenAI and Anthropic
// strict mode require it, so the portable schema was the one Gemini refused.
func TestGeminiBuildTooling_SanitizesParameters(t *testing.T) {
	tp := NewToolProvider("gemini-tools", "gemini-2.5-flash",
		"https://generativelanguage.googleapis.com/v1beta",
		providers.ProviderDefaults{}, false)
	defer func() { _ = tp.Close() }()

	built, err := tp.BuildTooling([]*providers.ToolDescriptor{{
		Name:        "get_weather",
		Description: "weather",
		InputSchema: json.RawMessage(`{
		  "$schema": "http://json-schema.org/draft-07/schema#",
		  "type": "object",
		  "properties": {
		    "city": {"type": "string"},
		    "opts": {"type": "object", "properties": {"units": {"type": "string"}}, "additionalProperties": false}
		  },
		  "required": ["city"],
		  "additionalProperties": false
		}`),
	}})
	require.NoError(t, err)

	decl, ok := built.(geminiToolDeclaration)
	require.True(t, ok, "expected geminiToolDeclaration, got %T", built)
	require.Len(t, decl.FunctionDeclarations, 1)

	var params map[string]any
	require.NoError(t, json.Unmarshal(decl.FunctionDeclarations[0].Parameters, &params))

	assert.NotContains(t, params, "additionalProperties", "root")
	assert.NotContains(t, params, "$schema")

	opts := params["properties"].(map[string]any)["opts"].(map[string]any)
	assert.NotContains(t, opts, "additionalProperties",
		"the nested object is rejected just as loudly as the root")

	// The parts Gemini does accept must survive — this is a removal, not a
	// rewrite of the caller's data model.
	assert.Equal(t, "object", params["type"])
	assert.Equal(t, []any{"city"}, params["required"])
	assert.Contains(t, params["properties"].(map[string]any), "city")
	assert.Contains(t, opts["properties"], "units")
}

// TestSanitizeGeminiSchema_KeepsPropertiesNamedLikeKeywords guards the
// traversal change: these keys are also legal property names, and the previous
// blunt walk deleted a caller's field called "definitions".
func TestSanitizeGeminiSchema_KeepsPropertiesNamedLikeKeywords(t *testing.T) {
	out := sanitizeGeminiSchemaRaw(json.RawMessage(`{
	  "type": "object",
	  "properties": {
	    "definitions": {"type": "string"},
	    "additionalProperties": {"type": "string"}
	  },
	  "additionalProperties": false
	}`))

	var doc map[string]any
	require.NoError(t, json.Unmarshal(out, &doc))

	assert.NotContains(t, doc, "additionalProperties", "the keyword goes")
	props := doc["properties"].(map[string]any)
	assert.Contains(t, props, "definitions", "the caller's field stays")
	assert.Contains(t, props, "additionalProperties",
		"a property may legally be named after a keyword")
}
