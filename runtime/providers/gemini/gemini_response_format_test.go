package gemini

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
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
