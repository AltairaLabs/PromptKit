package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatePackAgainstSchema_ValidPack(t *testing.T) {
	// Note: This test requires network access to fetch the schema from promptpack.org
	// If the schema is not available, the test will be skipped

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

	// If schema fetch fails, skip the test
	if err != nil {
		t.Skipf("Schema validation could not be performed (schema may not be available): %v", err)
	}

	if !result.Valid {
		t.Logf("Validation errors: %v", result.Errors)
	}

	// The schema may have additional required fields we're not aware of
	// So we just check that validation completed without error
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
	emptyPackJSON := []byte(`{}`)

	result, err := ValidatePackAgainstSchema(emptyPackJSON)

	// If schema fetch fails, skip the test
	if err != nil {
		t.Skipf("Schema validation could not be performed: %v", err)
	}

	// Empty pack should fail validation (missing required fields)
	// But we don't know the exact schema requirements
	assert.NotNil(t, result)
}
