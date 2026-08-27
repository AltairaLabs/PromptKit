package gemini

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Gemini expresses a response schema as a responseMimeType / responseSchema
// pair, and the two request builders assemble generationConfig differently —
// one as a struct, one as a map. Both go through responseFormatFields so they
// cannot drift; the map builder previously had no mapping at all, which
// silently dropped a caller's schema on every tool round.

func TestResponseFormatFields(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"a":{"type":"string"}}}`)

	cases := []struct {
		name       string
		rf         *providers.ResponseFormat
		wantMime   string
		wantSchema bool
	}{
		{"nil is unset", nil, "", false},
		{"text sends nothing", &providers.ResponseFormat{Type: providers.ResponseFormatText}, "", false},
		{
			name:     "json_object sets mime only",
			rf:       &providers.ResponseFormat{Type: providers.ResponseFormatJSON},
			wantMime: "application/json",
		},
		{
			name:       "json_schema sets both",
			rf:         &providers.ResponseFormat{Type: providers.ResponseFormatJSONSchema, JSONSchema: schema},
			wantMime:   "application/json",
			wantSchema: true,
		},
		{
			name:     "json_schema with no schema degrades to json mode",
			rf:       &providers.ResponseFormat{Type: providers.ResponseFormatJSONSchema},
			wantMime: "application/json",
		},
		{
			// Malformed JSON must not send a broken schema; JSON mode alone is
			// the safe degradation, since the alternative is a 400.
			name:     "unparsable schema degrades to json mode",
			rf:       &providers.ResponseFormat{Type: providers.ResponseFormatJSONSchema, JSONSchema: json.RawMessage(`{`)},
			wantMime: "application/json",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mime, schema := responseFormatFields(tc.rf)
			assert.Equal(t, tc.wantMime, mime)
			if tc.wantSchema {
				assert.NotNil(t, schema)
			} else {
				assert.Nil(t, schema)
			}
		})
	}
}

// TestApplyResponseFormatToMap_MatchesStructPath pins that the map builder and
// the struct builder produce the same wire fields. A drift here is invisible:
// each looks correct alone, and only a tool round shows the difference.
func TestApplyResponseFormatToMap_MatchesStructPath(t *testing.T) {
	rf := &providers.ResponseFormat{
		Type:       providers.ResponseFormatJSONSchema,
		JSONSchema: json.RawMessage(`{"type":"object","properties":{"a":{"type":"string"}}}`),
	}
	p := &Provider{}

	var structReq geminiRequest
	p.applyResponseFormat(&structReq, rf)

	mapCfg := map[string]any{}
	p.applyResponseFormatToMap(mapCfg, rf)

	assert.Equal(t, structReq.GenerationConfig.ResponseMimeType, mapCfg["responseMimeType"],
		"map and struct builders disagree on responseMimeType")

	structSchema, err := json.Marshal(structReq.GenerationConfig.ResponseSchema)
	require.NoError(t, err)
	mapSchema, err := json.Marshal(mapCfg["responseSchema"])
	require.NoError(t, err)
	assert.JSONEq(t, string(structSchema), string(mapSchema),
		"map and struct builders disagree on responseSchema")
}

// TestApplyResponseFormatToMap_LeavesMapCleanWhenUnset keeps "caller asked for
// nothing" distinguishable from "we sent an empty constraint".
func TestApplyResponseFormatToMap_LeavesMapCleanWhenUnset(t *testing.T) {
	p := &Provider{}
	for _, rf := range []*providers.ResponseFormat{
		nil,
		{Type: providers.ResponseFormatText},
	} {
		cfg := map[string]any{"maxOutputTokens": 100}
		p.applyResponseFormatToMap(cfg, rf)
		assert.NotContains(t, cfg, "responseMimeType")
		assert.NotContains(t, cfg, "responseSchema")
	}
}

// TestWantsSchema covers the predicate that decides whether a dropped
// constraint is worth warning about — warning on every tool call would be
// noise, and warning on none would hide the drop.
func TestWantsSchema(t *testing.T) {
	assert.False(t, wantsSchema(nil))
	assert.False(t, wantsSchema(&providers.ResponseFormat{Type: providers.ResponseFormatText}))
	assert.True(t, wantsSchema(&providers.ResponseFormat{Type: providers.ResponseFormatJSON}))
	assert.True(t, wantsSchema(&providers.ResponseFormat{Type: providers.ResponseFormatJSONSchema}))
}

// buildToolRequest decides whether a caller's schema reaches the wire, and the
// decision differs by whether the round carries tools. Both branches matter:
// sending it with tools fails the turn on Gemini 2.5 and hangs the loop on
// Gemini 3, while dropping it on a tool-free round would discard the caller's
// constraint for no reason.

func toolRequestGenConfig(t *testing.T, model string, rf *providers.ResponseFormat, withTools bool) map[string]any {
	t.Helper()
	return toolRequestGenConfigChoice(t, model, rf, withTools, "auto")
}

func toolRequestGenConfigChoice(
	t *testing.T, model string, rf *providers.ResponseFormat, withTools bool, toolChoice string,
) map[string]any {
	t.Helper()
	tp := NewToolProvider("gemini-rf", model,
		"https://generativelanguage.googleapis.com/v1beta",
		providers.ProviderDefaults{MaxTokens: 512}, false)

	var tools providers.ProviderTools
	if withTools {
		built, err := tp.BuildTooling([]*providers.ToolDescriptor{{
			Name:        "probe",
			Description: "probe",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`),
		}})
		require.NoError(t, err)
		tools = built
	}

	req := providers.PredictionRequest{
		Messages:       []types.Message{{Role: "user", Content: "hello"}},
		MaxTokens:      512,
		ResponseFormat: rf,
	}
	built := tp.buildToolRequest(context.Background(), req, tools, toolChoice)

	raw, err := json.Marshal(built)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))

	gen, ok := decoded[keyGenerationConfig].(map[string]any)
	require.True(t, ok, "generationConfig missing from request: %s", raw)
	return gen
}

func TestBuildToolRequest_SchemaOnlyWithoutTools(t *testing.T) {
	rf := &providers.ResponseFormat{
		Type:       providers.ResponseFormatJSONSchema,
		JSONSchema: json.RawMessage(`{"type":"object","properties":{"a":{"type":"string"}}}`),
	}

	// Every generation behaves the same here, for different underlying
	// reasons — 2.5 rejects the combination outright, 3.x accepts it and then
	// never stops calling tools.
	for _, model := range []string{"gemini-2.5-flash", "gemini-3.7-flash"} {
		t.Run(model+"/with_tools_drops_schema", func(t *testing.T) {
			gen := toolRequestGenConfig(t, model, rf, true)
			assert.NotContains(t, gen, "responseSchema",
				"a schema sent alongside tools fails the turn (2.5) or prevents the model "+
					"from ever answering (3.x)")
			assert.NotContains(t, gen, "responseMimeType")
		})

		t.Run(model+"/without_tools_keeps_schema", func(t *testing.T) {
			gen := toolRequestGenConfig(t, model, rf, false)
			assert.Contains(t, gen, "responseSchema",
				"a tool-free round has no conflict and must honor the caller's schema")
			assert.Equal(t, "application/json", gen["responseMimeType"])
		})
	}
}

// TestBuildToolRequest_NoSchemaConfigured keeps the drop path distinguishable
// from the never-asked path: neither sends the fields, but only one of them
// represents a constraint the caller will not get.
func TestBuildToolRequest_NoSchemaConfigured(t *testing.T) {
	for _, withTools := range []bool{true, false} {
		gen := toolRequestGenConfig(t, "gemini-2.5-flash", nil, withTools)
		assert.NotContains(t, gen, "responseSchema")
		assert.NotContains(t, gen, "responseMimeType")
	}
}

// TestBuildToolRequest_SchemaSentWhenCallingDisabled is the case that makes a
// constrained answer possible at all.
//
// Gemini rejects a schema while function calling is ENABLED, not while tools
// are declared. With tool_choice=none the tools stay in the request — so the
// tool history remains in context — and both generations return conforming
// JSON. Verified live on gemini-2.5-flash and gemini-3.7-flash.
func TestBuildToolRequest_SchemaSentWhenCallingDisabled(t *testing.T) {
	rf := &providers.ResponseFormat{
		Type:       providers.ResponseFormatJSONSchema,
		JSONSchema: json.RawMessage(`{"type":"object","properties":{"a":{"type":"string"}}}`),
	}

	for _, model := range []string{"gemini-2.5-flash", "gemini-3.7-flash"} {
		t.Run(model, func(t *testing.T) {
			gen := toolRequestGenConfigChoice(t, model, rf, true, "none")
			assert.Contains(t, gen, "responseSchema",
				"tool_choice=none cannot call a tool, so the schema is safe and must be sent")
			assert.Equal(t, "application/json", gen["responseMimeType"])
		})
	}
}

// TestToolCallingDisabled pins the predicate against the modes addToolConfig
// produces: only NONE stops the model calling, so only NONE makes a schema safe.
func TestToolCallingDisabled(t *testing.T) {
	assert.True(t, toolCallingDisabled("none"))
	assert.True(t, toolCallingDisabled("NONE"), "tool choice is matched case-insensitively")

	for _, tc := range []string{"", "auto", "required", "any", "get_weather"} {
		assert.Falsef(t, toolCallingDisabled(tc),
			"%q still permits a call, so a schema would fail the round", tc)
	}
}
