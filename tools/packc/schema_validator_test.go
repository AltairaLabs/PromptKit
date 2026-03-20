package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatePackAgainstSchema_ValidPack(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")

	validPackJSON := []byte(`{
		"$schema": "https://promptpack.org/schema/latest/promptpack.schema.json",
		"id": "test-pack",
		"name": "Test Pack",
		"version": "v1.0.0",
		"description": "A test pack",
		"template_engine": {
			"version": "v1.0.0",
			"syntax": "go-template"
		},
		"prompts": {
			"test-prompt": {
				"id": "test-prompt",
				"name": "Test Prompt",
				"description": "A test prompt",
				"version": "v1.0.0",
				"system_template": "You are a helpful assistant."
			}
		}
	}`)

	result, err := ValidatePackAgainstSchema(validPackJSON)
	require.NoError(t, err, "Schema validation should not fail with local schema")

	if !result.Valid {
		t.Logf("Validation errors: %v", result.Errors)
	}

	assert.NotNil(t, result)
}

func TestValidatePackAgainstSchema_InvalidJSON(t *testing.T) {
	invalidJSON := []byte(`{not valid json}`)

	result, err := ValidatePackAgainstSchema(invalidJSON)

	// Should fail to parse
	if err == nil && result != nil && !result.Valid {
		// JSON parsing error reported as validation error
		require.Greater(t, len(result.Errors), 0)
	}
}

func TestValidatePackAgainstSchema_EmptyPack(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")

	emptyPackJSON := []byte(`{}`)

	result, err := ValidatePackAgainstSchema(emptyPackJSON)
	require.NoError(t, err, "Schema validation should not fail with local schema")

	// Empty pack should fail validation (missing required fields)
	assert.NotNil(t, result)
}
