// Package pack — validator JSON load tests.
package pack

import (
	"encoding/json"
	"strings"
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

// TestValidatorJSONRejectsForbiddenFields proves that the embedded
// promptpack schema (loaded by ValidateAgainstSchema) rejects validator
// JSON containing fields the spec forbids via additionalProperties:false.
// This is the outer guarantee that the SDK never reaches struct unmarshal
// for a non-spec pack.
func TestValidatorJSONRejectsForbiddenFields(t *testing.T) {
	cases := []struct {
		name    string
		extra   string
		wantErr string
	}{
		{
			name:    "monitor field forbidden",
			extra:   `"monitor": true`,
			wantErr: "monitor",
		},
		{
			name:    "config field forbidden",
			extra:   `"config": {"foo": "bar"}`,
			wantErr: "config",
		},
		{
			name:    "message field forbidden",
			extra:   `"message": "custom"`,
			wantErr: "message",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			packJSON := []byte(`{
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
						"system_template": "hi",
						"validators": [
							{"type": "max_length", "enabled": true, ` + tc.extra + `}
						]
					}
				}
			}`)

			err := ValidateAgainstSchema(packJSON)
			require.Error(t, err, "schema must reject validator with forbidden field %q", tc.extra)

			var schemaErr *SchemaValidationError
			require.ErrorAs(t, err, &schemaErr)
			// At least one error must mention the forbidden field or
			// flag it as an unknown/additional property.
			found := false
			for _, e := range schemaErr.Errors {
				if strings.Contains(e, tc.wantErr) || strings.Contains(e, "additional property") {
					found = true
					break
				}
			}
			assert.True(t, found,
				"expected schema error to reference %q or additionalProperties; got %v",
				tc.wantErr, schemaErr.Errors)
		})
	}
}
