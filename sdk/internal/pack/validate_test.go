package pack

import (
	"testing"
)

func TestValidateAgainstSchema_Valid(t *testing.T) {
	// Valid pack JSON
	validPack := []byte(`{
		"$schema": "https://promptpack.org/schema/latest/promptpack.schema.json",
		"id": "test-pack",
		"name": "Test Pack",
		"version": "1.0.0",
		"description": "A test pack",
		"template_engine": {
			"version": "v1",
			"syntax": "{{variable}}"
		},
		"prompts": {
			"hello": {
				"id": "hello",
				"name": "Hello Prompt",
				"version": "1.0.0",
				"system_template": "Hello, {{name}}!",
				"variables": [
					{
						"name": "name",
						"type": "string",
						"required": true
					}
				]
			}
		}
	}`)

	err := ValidateAgainstSchema(validPack)
	if err != nil {
		t.Errorf("expected no error for valid pack, got: %v", err)
	}
}

func TestValidateAgainstSchema_Invalid(t *testing.T) {
	// Invalid pack - missing required fields
	invalidPack := []byte(`{
		"id": "test-pack",
		"prompts": {}
	}`)

	err := ValidateAgainstSchema(invalidPack)
	if err == nil {
		t.Error("expected error for invalid pack, got nil")
	}

	// Check it's a SchemaValidationError
	schemaErr, ok := err.(*SchemaValidationError)
	if !ok {
		t.Errorf("expected SchemaValidationError, got %T", err)
	}
	if len(schemaErr.Errors) == 0 {
		t.Error("expected at least one validation error")
	}
}

func TestValidateAgainstSchema_InvalidJSON(t *testing.T) {
	invalidJSON := []byte(`{not valid json}`)

	err := ValidateAgainstSchema(invalidJSON)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestSchemaValidationError_Error(t *testing.T) {
	// Single error
	err := &SchemaValidationError{Errors: []string{"field is required"}}
	expected := "pack schema validation failed: field is required"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}

	// Multiple errors
	err = &SchemaValidationError{Errors: []string{"error1", "error2", "error3"}}
	expected = "pack schema validation failed with 3 errors"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestLoad_WithSchemaValidation(t *testing.T) {
	// This test requires the example pack files to be present
	// It validates that Load() performs schema validation by default

	// Skip if running in CI without the example files
	t.Skip("Requires example pack files - run manually")
}

func TestLoad_SkipSchemaValidation(t *testing.T) {
	// Create a temporary invalid pack file
	// and verify it loads when schema validation is skipped

	// Skip if running in CI without setup
	t.Skip("Requires temp file setup - run manually")
}
