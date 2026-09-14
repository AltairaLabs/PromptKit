package vllm

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// buildFromConfig runs the additional_config extraction the way a declaratively
// configured provider reaches it.
func buildFromConfig(t *testing.T, cfg map[string]any) *vllmRequest {
	t.Helper()
	p := NewProvider("vllm", "m", "http://x", providers.ProviderDefaults{}, false, cfg)
	return p.buildRequest(&providers.PredictionRequest{}, nil, 0.7, 1, 128, false)
}

// decodeYAML mirrors what pkg/config hands a provider from a *.provider.yaml:
// a map[string]any decoded by the YAML decoder, NOT a hand-written Go literal.
// Hand-written literals are why these paths looked covered — a test that writes
// []string{"a"} asserts a shape the config loader never produces.
func decodeYAML(t *testing.T, src string) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(src), &out))
	return out
}

func decodeJSON(t *testing.T, src string) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(src), &out))
	return out
}

// TestAdditionalConfig_GuidedChoiceFromYAML pins the shape a config file
// actually produces. A YAML sequence decodes to []any, never []string, so a
// []string type assertion drops the field silently — guided_choice could not
// be set from a provider file at all.
func TestAdditionalConfig_GuidedChoiceFromYAML(t *testing.T) {
	req := buildFromConfig(t, decodeYAML(t, "guided_choice: [refund, exchange, escalate]"))

	assert.Equal(t, []string{"refund", "exchange", "escalate"}, req.GuidedChoice)
}

// TestAdditionalConfig_BestOfFromJSON — JSON numbers decode to float64, so an
// int assertion drops best_of on the JSON path while it worked from YAML.
func TestAdditionalConfig_BestOfFromJSON(t *testing.T) {
	req := buildFromConfig(t, decodeJSON(t, `{"best_of": 4}`))

	assert.Equal(t, 4, req.BestOf)
}

func TestAdditionalConfig_BestOfFromYAML(t *testing.T) {
	req := buildFromConfig(t, decodeYAML(t, "best_of: 4"))

	assert.Equal(t, 4, req.BestOf)
}

// TestAdditionalConfig_RemainingKeysFromYAML is the control: the keys whose
// declared type already matched what a decoder produces must keep working.
func TestAdditionalConfig_RemainingKeysFromYAML(t *testing.T) {
	req := buildFromConfig(t, decodeYAML(t, `
use_beam_search: true
ignore_eos: true
skip_special_tokens: true
guided_regex: "^[A-Z]+$"
guided_grammar: "root ::= [0-9]+"
guided_json:
  type: object
`))

	assert.True(t, req.UseBeamSearch)
	assert.True(t, req.IgnoreEOS)
	assert.True(t, req.SkipSpecialTokens)
	assert.Equal(t, "^[A-Z]+$", req.GuidedRegex)
	assert.Equal(t, "root ::= [0-9]+", req.GuidedGrammar)
	assert.Equal(t, map[string]any{"type": "object"}, req.GuidedJSON)
}

// TestAdditionalConfig_WrongTypesAreIgnored — a genuinely wrong type is still
// ignored rather than coerced. Widening the accepted shapes must not turn
// "guided_choice: 3" into a value.
func TestAdditionalConfig_WrongTypesAreIgnored(t *testing.T) {
	req := buildFromConfig(t, decodeYAML(t, `
guided_choice: 3
best_of: "many"
use_beam_search: "yes"
`))

	assert.Nil(t, req.GuidedChoice)
	assert.Zero(t, req.BestOf)
	assert.False(t, req.UseBeamSearch)
}

// TestAdditionalConfig_GuidedChoiceRejectsNonStrings — a sequence carrying a
// non-string element is not a choice list. Take none of it rather than
// silently dropping the element the author wrote.
func TestAdditionalConfig_GuidedChoiceRejectsNonStrings(t *testing.T) {
	req := buildFromConfig(t, decodeYAML(t, "guided_choice: [refund, 7]"))

	assert.Nil(t, req.GuidedChoice)
}
