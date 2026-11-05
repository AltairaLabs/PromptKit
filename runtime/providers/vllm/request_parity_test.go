package vllm

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

func wireMap(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

// TestRequestParity_EveryFieldReachesBothPaths pins every vllmRequest field on
// the plain request and on the tools request. buildToolRequest copies fields
// into a map by hand, so a field added to the struct (guided_grammar was one)
// is silently dropped from tool requests unless it is copied there too.
func TestRequestParity_EveryFieldReachesBothPaths(t *testing.T) {
	seed, topK := 7, 40
	penalty := float32(0.5)
	p := NewProvider("vllm", "m", "http://x", providers.ProviderDefaults{}, false, map[string]any{
		"use_beam_search":     true,
		"best_of":             3,
		"ignore_eos":          true,
		"skip_special_tokens": true,
		"guided_json":         map[string]any{"type": "object"},
		"guided_regex":        "^[A-Z]+$",
		"guided_grammar":      "root ::= [0-9]+",
		"guided_choice":       []any{"a", "b"},
	})
	req := &providers.PredictionRequest{
		Seed: &seed, TopK: &topK, FrequencyPenalty: &penalty, PresencePenalty: &penalty,
	}
	messages := []vllmMessage{{Role: "user", Content: "hi"}}

	plain := p.buildRequest(req, messages, 0.7, 0.9, 128, true)

	// The fixture must populate every struct field, or a new field would
	// escape this test the same way it escaped the tools path.
	rv := reflect.ValueOf(*plain)
	for i := 0; i < rv.NumField(); i++ {
		assert.Falsef(t, rv.Field(i).IsZero(),
			"fixture leaves vllmRequest.%s unset; set it so both paths are checked", rv.Type().Field(i).Name)
	}

	plainWire := wireMap(t, plain)
	toolWire := wireMap(t, p.buildToolRequest(req, messages, toolRequestParams{
		temperature: 0.7, topP: 0.9, maxTokens: 128, stream: true,
	}))

	require.NotEmpty(t, plainWire)
	for key, want := range plainWire {
		got, ok := toolWire[key]
		assert.Truef(t, ok, "tools request dropped %q", key)
		assert.Equalf(t, want, got, "tools request changed %q", key)
	}
}
