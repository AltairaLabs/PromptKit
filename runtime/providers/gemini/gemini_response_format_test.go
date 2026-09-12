package gemini

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
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
			// An unrecognized type must send nothing at all rather than guess.
			// Guessing json here would silently constrain a caller who asked
			// for something else.
			name: "unknown type sends nothing",
			rf:   &providers.ResponseFormat{Type: providers.ResponseFormatType("xml")},
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

// buildToolRequest decides whether the caller's schema reaches the wire, and
// the two branches have very different consequences. Dropping it on a
// tool-free round would discard the constraint for no reason; sending it on a
// tool-carrying round fails the turn on Gemini 2.5 and stops Gemini 3 ever
// answering. Both are covered here because only the assembled request shows
// which branch ran.

func toolRequestGenConfig(t *testing.T, model string, rf *providers.ResponseFormat, withTools bool) map[string]any {
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
	built := tp.buildToolRequest(context.Background(), req, tools, "auto")

	raw, err := json.Marshal(built)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))

	gen, ok := decoded[keyGenerationConfig].(map[string]any)
	require.Truef(t, ok, "generationConfig missing from request: %s", raw)
	return gen
}

func TestBuildToolRequest_SchemaOnlyOnToolFreeRounds(t *testing.T) {
	rf := &providers.ResponseFormat{
		Type:       providers.ResponseFormatJSONSchema,
		JSONSchema: json.RawMessage(`{"type":"object","properties":{"a":{"type":"string"}}}`),
	}

	// Both generations behave the same, for different underlying reasons: 2.5
	// rejects the combination outright, 3.x accepts it and then never stops
	// calling tools.
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
