package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xeipuuv/gojsonschema"
)

func TestValidateJSONAgainstLoader_Valid(t *testing.T) {
	schemaJSON := `{
		"type": "object",
		"required": ["name"],
		"properties": {
			"name": {"type": "string"}
		}
	}`
	loader := gojsonschema.NewStringLoader(schemaJSON)
	data := []byte(`{"name": "test"}`)

	result, err := ValidateJSONAgainstLoader(data, loader)
	require.NoError(t, err)
	assert.True(t, result.Valid)
	assert.Empty(t, result.Errors)
}

func TestValidateJSONAgainstLoader_Invalid(t *testing.T) {
	schemaJSON := `{
		"type": "object",
		"required": ["name", "version"],
		"properties": {
			"name": {"type": "string"},
			"version": {"type": "string"}
		}
	}`
	loader := gojsonschema.NewStringLoader(schemaJSON)
	data := []byte(`{"name": "test"}`)

	result, err := ValidateJSONAgainstLoader(data, loader)
	require.NoError(t, err)
	assert.False(t, result.Valid)
	assert.NotEmpty(t, result.Errors)

	// Check that error has field information
	found := false
	for _, e := range result.Errors {
		if e.Field == "(root)" && e.Description != "" {
			found = true
		}
	}
	assert.True(t, found, "expected validation error with field info, got: %v", result.Errors)
}

func TestValidateJSONAgainstLoader_InvalidJSON(t *testing.T) {
	schemaJSON := `{"type": "object"}`
	loader := gojsonschema.NewStringLoader(schemaJSON)
	data := []byte(`{not valid json}`)

	_, err := ValidateJSONAgainstLoader(data, loader)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema validation failed")
}

func TestValidateJSONAgainstLoader_InvalidSchema(t *testing.T) {
	loader := gojsonschema.NewReferenceLoader("file:///nonexistent/schema.json")
	data := []byte(`{"name": "test"}`)

	_, err := ValidateJSONAgainstLoader(data, loader)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema validation failed")
}

func TestValidationError_Error(t *testing.T) {
	t.Run("with value", func(t *testing.T) {
		e := ValidationError{
			Field:       "spec.name",
			Description: "is required",
			Value:       "bad",
		}
		assert.Equal(t, "spec.name: is required (value: bad)", e.Error())
	})

	t.Run("without value", func(t *testing.T) {
		e := ValidationError{
			Field:       "spec.name",
			Description: "is required",
			Value:       nil,
		}
		assert.Equal(t, "spec.name: is required", e.Error())
	})
}

func TestConvertResult_Valid(t *testing.T) {
	schemaJSON := `{"type": "object"}`
	schemaLoader := gojsonschema.NewStringLoader(schemaJSON)
	documentLoader := gojsonschema.NewStringLoader(`{"name": "test"}`)

	result, err := gojsonschema.Validate(schemaLoader, documentLoader)
	require.NoError(t, err)

	vr := ConvertResult(result)
	assert.True(t, vr.Valid)
	assert.Empty(t, vr.Errors)
}

func TestConvertResult_Invalid(t *testing.T) {
	schemaJSON := `{
		"type": "object",
		"required": ["name"],
		"properties": {
			"name": {"type": "string"}
		}
	}`
	schemaLoader := gojsonschema.NewStringLoader(schemaJSON)
	documentLoader := gojsonschema.NewStringLoader(`{}`)

	result, err := gojsonschema.Validate(schemaLoader, documentLoader)
	require.NoError(t, err)

	vr := ConvertResult(result)
	assert.False(t, vr.Valid)
	require.NotEmpty(t, vr.Errors)
	assert.Equal(t, "(root)", vr.Errors[0].Field)
	assert.Contains(t, vr.Errors[0].Description, "name is required")
}
