// Package pack — validator JSON load tests.
package pack

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidatorLoadsFromPromptPackJSON is the regression test for issue #933.
// A pack declaring a validator with params must load into pack.Validator
// with params populated — the SDK's struct tag must match the promptpack spec.
func TestValidatorLoadsFromPromptPackJSON(t *testing.T) {
	packJSON := []byte(`{
		"$schema": "https://promptpack.org/schema/latest/promptpack.schema.json",
		"id": "test-pack",
		"name": "Test Pack",
		"version": "1.0.0",
		"description": "test",
		"prompts": {
			"default": {
				"id": "default",
				"name": "Default",
				"description": "test prompt",
				"version": "1.0.0",
				"system_template": "you are helpful",
				"validators": [
					{
						"type": "max_length",
						"enabled": true,
						"fail_on_violation": false,
						"params": {"max_characters": 2000}
					}
				]
			}
		}
	}`)

	// Use Parse to bypass schema validation — we want to isolate the
	// struct-unmarshal behaviour from the schema validator in this test.
	pack, err := Parse(packJSON)
	require.NoError(t, err)

	prompt := pack.GetPrompt("default")
	require.NotNil(t, prompt)
	require.Len(t, prompt.Validators, 1, "pack should have one validator")

	v := prompt.Validators[0]
	assert.Equal(t, "max_length", v.Type)
	assert.True(t, v.Enabled, "enabled should be true")
	require.NotNil(t, v.FailOnViolation, "fail_on_violation should be present (pointer non-nil)")
	assert.False(t, *v.FailOnViolation, "fail_on_violation should be false")

	// THIS IS THE BUG: params arrives as nil today because the struct tag
	// is `json:"config"` instead of `json:"params"`.
	require.NotNil(t, v.Params, "params must survive JSON unmarshal (bug #933)")
	assert.Equal(t, float64(2000), v.Params["max_characters"],
		"max_characters must survive as float64 (JSON number default)")
}

// TestValidatorMarshalRoundTrip proves field-level fidelity: a Validator
// marshalled to JSON and unmarshalled back must compare equal.
func TestValidatorMarshalRoundTrip(t *testing.T) {
	failOn := true
	original := Validator{
		Type:            "max_length",
		Enabled:         true,
		FailOnViolation: &failOn,
		Params: map[string]any{
			"max_characters": float64(2000),
		},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var round Validator
	require.NoError(t, json.Unmarshal(data, &round))

	assert.Equal(t, original.Type, round.Type)
	assert.Equal(t, original.Enabled, round.Enabled)
	require.NotNil(t, round.FailOnViolation)
	assert.Equal(t, *original.FailOnViolation, *round.FailOnViolation)
	assert.Equal(t, original.Params, round.Params)
}
