package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeSchemaFixture(t *testing.T, schemaJSON string) string {
	t.Helper()
	tmpDir := t.TempDir()
	schemaDir := filepath.Join(tmpDir, "schemas", "v1alpha1")
	require.NoError(t, os.MkdirAll(schemaDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(schemaDir, "arena.json"),
		[]byte(schemaJSON), 0o600,
	))
	return schemaDir
}

func TestSchemaValidationError_AdditionalPropertyEnriched(t *testing.T) {
	schemaDir := writeSchemaFixture(t, `{
		"$schema":"http://json-schema.org/draft-07/schema#",
		"type":"object",
		"properties":{
			"spec":{
				"type":"object",
				"additionalProperties":false,
				"properties":{
					"prompt":{"type":"string"},
					"model":{"type":"string"}
				}
			}
		}
	}`)

	yamlData := []byte("spec:\n  judge: foo\n")

	result, err := ValidateWithLocalSchema(yamlData, ConfigTypeArena, schemaDir)
	require.NoError(t, err)
	require.False(t, result.Valid)
	require.NotEmpty(t, result.Errors)

	var addPropErr *SchemaValidationError
	for i, e := range result.Errors {
		if e.Keyword == "additional_property_not_allowed" {
			addPropErr = &result.Errors[i]
			break
		}
	}
	require.NotNil(t, addPropErr, "expected additional_property_not_allowed error, got %+v", result.Errors)
	assert.ElementsMatch(t, []string{"model", "prompt"}, addPropErr.ValidValues)
	// "judge" vs "prompt" distance 5; "judge" vs "model" distance 5; no close match.
	assert.Nil(t, addPropErr.Suggestions)
}

func TestSchemaValidationError_AdditionalPropertyWithCloseMatch(t *testing.T) {
	schemaDir := writeSchemaFixture(t, `{
		"$schema":"http://json-schema.org/draft-07/schema#",
		"type":"object",
		"additionalProperties":false,
		"properties":{
			"prompt":{"type":"string"},
			"prompts":{"type":"array"}
		}
	}`)

	yamlData := []byte("promp: x\n")

	result, err := ValidateWithLocalSchema(yamlData, ConfigTypeArena, schemaDir)
	require.NoError(t, err)
	require.False(t, result.Valid)

	var addPropErr *SchemaValidationError
	for i, e := range result.Errors {
		if e.Keyword == "additional_property_not_allowed" {
			addPropErr = &result.Errors[i]
			break
		}
	}
	require.NotNil(t, addPropErr)
	assert.Equal(t, []string{"prompt", "prompts"}, addPropErr.Suggestions)
}

func TestSchemaValidationError_EnumEnriched(t *testing.T) {
	schemaDir := writeSchemaFixture(t, `{
		"$schema":"http://json-schema.org/draft-07/schema#",
		"type":"object",
		"properties":{
			"provider":{"type":"string","enum":["openai","anthropic","mock"]}
		}
	}`)

	yamlData := []byte("provider: anthrop\n")

	result, err := ValidateWithLocalSchema(yamlData, ConfigTypeArena, schemaDir)
	require.NoError(t, err)
	require.False(t, result.Valid)
	require.NotEmpty(t, result.Errors)

	var enumErr *SchemaValidationError
	for i, e := range result.Errors {
		if e.Keyword == "enum" {
			enumErr = &result.Errors[i]
			break
		}
	}
	require.NotNil(t, enumErr)
	assert.Equal(t, []string{"openai", "anthropic", "mock"}, enumErr.ValidValues)
	assert.Equal(t, []string{"anthropic"}, enumErr.Suggestions)
}

func TestTruncateValidValues(t *testing.T) {
	t.Run("under limit", func(t *testing.T) {
		got := truncateValidValues([]string{"b", "a", "c"}, 8)
		assert.Equal(t, []string{"a", "b", "c"}, got)
	})
	t.Run("over limit", func(t *testing.T) {
		got := truncateValidValues([]string{"a", "b", "c", "d", "e"}, 3)
		assert.Equal(t, []string{"a", "b", "c", "+2 more"}, got)
	})
}
