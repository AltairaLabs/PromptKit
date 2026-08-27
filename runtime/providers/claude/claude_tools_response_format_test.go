package claude

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// The tool paths used to drop ResponseFormat entirely: buildToolRequest never
// read the field, so a caller that set a JSON schema got free-form prose and no
// signal that the constraint had been discarded. Issue #1848.
//
// The omission was guarded on a conflict between structured outputs and tool
// use. Verified live against claude-sonnet-4-6 that no such conflict exists:
// sending tools and output_config together returns HTTP 200, the model calls
// the tool normally on the first round, and the final round returns
// schema-conforming JSON with stop_reason=end_turn.

const toolSchema = `{"type":"object","properties":{"city":{"type":"string"},` +
	`"celsius":{"type":"number"}},"required":["city","celsius"],"additionalProperties":false}`

func toolRequestJSON(t *testing.T, rf *providers.ResponseFormat, tools providers.ProviderTools) []byte {
	t.Helper()
	tp := NewToolProvider("claude-test", "claude-sonnet-4-6", "https://api.anthropic.com/v1",
		providers.ProviderDefaults{MaxTokens: 1024}, false)
	req := providers.PredictionRequest{
		Messages:       []types.Message{{Role: "user", Content: "temperature in Bristol?"}},
		MaxTokens:      1024,
		ResponseFormat: rf,
	}
	raw, err := json.Marshal(tp.buildToolRequest(context.Background(), req, tools, ""))
	require.NoError(t, err)
	return raw
}

func schemaTools(t *testing.T) providers.ProviderTools {
	t.Helper()
	tp := NewToolProvider("claude-test", "claude-sonnet-4-6", "https://api.anthropic.com/v1",
		providers.ProviderDefaults{MaxTokens: 1024}, false)
	tools, err := tp.BuildTooling([]*providers.ToolDescriptor{{
		Name:        "get_temperature",
		Description: "Get temperature for a city",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`),
	}})
	require.NoError(t, err)
	return tools
}

// TestBuildToolRequest_CarriesResponseFormat is the core fix: a schema set by
// the caller must reach the wire on the tool path, as it already does on
// Predict and PredictStream.
func TestBuildToolRequest_CarriesResponseFormat(t *testing.T) {
	raw := toolRequestJSON(t, &providers.ResponseFormat{
		Type:       providers.ResponseFormatJSONSchema,
		JSONSchema: json.RawMessage(toolSchema),
	}, schemaTools(t))

	assert.Containsf(t, string(raw), `"output_config"`,
		"tool request dropped the caller's ResponseFormat; request=%s", raw)
	assert.Contains(t, string(raw), `"json_schema"`)
	assert.Contains(t, string(raw), `"celsius"`, "the schema itself must reach the wire")

	// The tools must still be sent — honoring the schema must not displace them.
	assert.Contains(t, string(raw), `"get_temperature"`)
}

// TestBuildToolRequest_ResponseFormatOmittedWhenUnset keeps the absent case
// distinguishable from the set case, so "no output_config" means the caller
// asked for none rather than the field being dropped again.
func TestBuildToolRequest_ResponseFormatOmittedWhenUnset(t *testing.T) {
	cases := []struct {
		name string
		rf   *providers.ResponseFormat
	}{
		{"nil response format", nil},
		{"text format", &providers.ResponseFormat{Type: providers.ResponseFormatText}},
		{"json_schema with no schema", &providers.ResponseFormat{Type: providers.ResponseFormatJSONSchema}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := toolRequestJSON(t, tc.rf, schemaTools(t))
			assert.NotContainsf(t, string(raw), `"output_config"`,
				"emitted output_config for %s; request=%s", tc.name, raw)
		})
	}
}

// TestBuildToolRequest_ResponseFormatAppliesWithoutTools covers the final round
// of a loop that offers no tools — the case the issue suggested as a fallback.
// It must work through the same path rather than only when tools are present.
func TestBuildToolRequest_ResponseFormatAppliesWithoutTools(t *testing.T) {
	raw := toolRequestJSON(t, &providers.ResponseFormat{
		Type:       providers.ResponseFormatJSONSchema,
		JSONSchema: json.RawMessage(toolSchema),
	}, nil)

	assert.Contains(t, string(raw), `"output_config"`)
	assert.NotContains(t, string(raw), `"tools"`, "no tools were supplied for this round")
}
