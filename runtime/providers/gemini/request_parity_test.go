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

// The plain request is a typed geminiRequest; the tools request is a
// hand-built map. They drifted once (the tools request dropped safetySettings,
// #2319), so this pins every key the plain request sends to the tools request.
// Tools-only keys (tools, tool_config) are allowed.
func TestRequestParity_ToolsRequestCarriesEveryPlainKey(t *testing.T) {
	topK := 40
	req := providers.PredictionRequest{
		System:      "be brief",
		Messages:    []types.Message{{Role: "user", Content: "hello"}},
		Temperature: 0.3,
		TopP:        0.8,
		TopK:        &topK,
		MaxTokens:   256,
		ResponseFormat: &providers.ResponseFormat{
			Type:       providers.ResponseFormatJSONSchema,
			JSONSchema: json.RawMessage(`{"type":"object","properties":{"a":{"type":"string"}}}`),
		},
	}

	tp := NewToolProvider("gemini-parity", "gemini-2.5-flash",
		"https://generativelanguage.googleapis.com/v1beta",
		providers.ProviderDefaults{MaxTokens: 512}, false)
	ctx := context.Background()

	prep, err := tp.prepareGeminiRequest(ctx, req)
	require.NoError(t, err)
	plain := prep.build(tp.Provider)
	plain.GenerationConfig.applyOptionalSampling(tp.Provider, &req)
	tp.applyResponseFormat(&plain, req.ResponseFormat)

	// No tools, so the response schema is sent on both paths and the
	// generationConfig can be compared whole.
	tooled := mustBuildToolRequest(t, tp, req, nil, "")

	plainMap := toJSONMap(t, plain)
	toolsMap := toJSONMap(t, tooled)

	require.Contains(t, plainMap, "safetySettings", "fixture must exercise safetySettings")
	for key, want := range plainMap {
		got, ok := toolsMap[key]
		if !assert.Truef(t, ok, "tools request is missing %q that the plain request sends", key) {
			continue
		}
		assert.Equalf(t, want, got, "tools request value differs for %q", key)
	}
}

func toJSONMap(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	return m
}
