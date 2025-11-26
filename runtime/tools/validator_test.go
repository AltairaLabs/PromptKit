package tools

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchemaValidator_ValidateArgs(t *testing.T) {
	validator := NewSchemaValidator()

	descriptor := &ToolDescriptor{
		Name: "test-tool",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"name": {"type": "string"},
				"age": {"type": "number"}
			},
			"required": ["name"]
		}`),
	}

	t.Run("valid args", func(t *testing.T) {
		args := json.RawMessage(`{"name": "Alice", "age": 30}`)
		err := validator.ValidateArgs(descriptor, args)
		assert.NoError(t, err)
	})

	t.Run("missing required field", func(t *testing.T) {
		args := json.RawMessage(`{"age": 30}`)
		err := validator.ValidateArgs(descriptor, args)
		require.Error(t, err)
		validationErr, ok := err.(*ValidationError)
		require.True(t, ok)
		assert.Equal(t, "args_invalid", validationErr.Type)
		assert.Equal(t, "test-tool", validationErr.Tool)
	})

	t.Run("invalid type", func(t *testing.T) {
		args := json.RawMessage(`{"name": "Alice", "age": "thirty"}`)
		err := validator.ValidateArgs(descriptor, args)
		require.Error(t, err)
		validationErr, ok := err.(*ValidationError)
		require.True(t, ok)
		assert.Equal(t, "args_invalid", validationErr.Type)
	})

	t.Run("invalid schema", func(t *testing.T) {
		badDescriptor := &ToolDescriptor{
			Name:        "bad-tool",
			InputSchema: json.RawMessage(`{invalid json`),
		}
		args := json.RawMessage(`{"name": "Alice"}`)
		err := validator.ValidateArgs(badDescriptor, args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid input schema")
	})

	t.Run("schema caching", func(t *testing.T) {
		args := json.RawMessage(`{"name": "Bob"}`)

		// First call - schema gets cached
		err := validator.ValidateArgs(descriptor, args)
		assert.NoError(t, err)

		// Second call - should use cached schema
		err = validator.ValidateArgs(descriptor, args)
		assert.NoError(t, err)

		// Verify cache contains the schema
		schemaKey := string(descriptor.InputSchema)
		_, exists := validator.cache[schemaKey]
		assert.True(t, exists)
	})
}

func TestSchemaValidator_ValidateResult(t *testing.T) {
	validator := NewSchemaValidator()

	descriptor := &ToolDescriptor{
		Name: "test-tool",
		OutputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"status": {"type": "string"},
				"count": {"type": "integer"}
			},
			"required": ["status"]
		}`),
	}

	t.Run("valid result", func(t *testing.T) {
		result := json.RawMessage(`{"status": "success", "count": 42}`)
		err := validator.ValidateResult(descriptor, result)
		assert.NoError(t, err)
	})

	t.Run("missing required field", func(t *testing.T) {
		result := json.RawMessage(`{"count": 42}`)
		err := validator.ValidateResult(descriptor, result)
		require.Error(t, err)
		validationErr, ok := err.(*ValidationError)
		require.True(t, ok)
		assert.Equal(t, "result_invalid", validationErr.Type)
		assert.Equal(t, "test-tool", validationErr.Tool)
	})

	t.Run("invalid type", func(t *testing.T) {
		result := json.RawMessage(`{"status": "success", "count": "forty-two"}`)
		err := validator.ValidateResult(descriptor, result)
		require.Error(t, err)
		validationErr, ok := err.(*ValidationError)
		require.True(t, ok)
		assert.Equal(t, "result_invalid", validationErr.Type)
	})

	t.Run("invalid schema", func(t *testing.T) {
		badDescriptor := &ToolDescriptor{
			Name:         "bad-tool",
			OutputSchema: json.RawMessage(`{invalid json`),
		}
		result := json.RawMessage(`{"status": "success"}`)
		err := validator.ValidateResult(badDescriptor, result)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid output schema")
	})
}

func TestSchemaValidator_CoerceResult(t *testing.T) {
	validator := NewSchemaValidator()

	descriptor := &ToolDescriptor{
		Name: "test-tool",
		OutputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"status": {"type": "string"},
				"count": {"type": "integer"}
			},
			"required": ["status"]
		}`),
	}

	t.Run("already valid - no coercion needed", func(t *testing.T) {
		result := json.RawMessage(`{"status": "success", "count": 42}`)
		coerced, coercions, err := validator.CoerceResult(descriptor, result)
		assert.NoError(t, err)
		assert.Equal(t, result, coerced)
		assert.Nil(t, coercions)
	})

	t.Run("coerces nested maps", func(t *testing.T) {
		// Even though this might not match the schema perfectly,
		// we're testing that coerceValue handles nested maps
		descriptor := &ToolDescriptor{
			Name: "nested-tool",
			OutputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"status": {"type": "string"},
					"metadata": {
						"type": "object",
						"properties": {
							"timestamp": {"type": "number"}
						}
					}
				},
				"required": ["status"]
			}`),
		}

		result := json.RawMessage(`{"status": "success", "metadata": {"timestamp": 123.45}}`)
		coerced, coercions, err := validator.CoerceResult(descriptor, result)
		assert.NoError(t, err)
		assert.NotNil(t, coerced)
		assert.Empty(t, coercions)
	})

	t.Run("coerces arrays", func(t *testing.T) {
		descriptor := &ToolDescriptor{
			Name: "array-tool",
			OutputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"items": {
						"type": "array",
						"items": {"type": "number"}
					}
				}
			}`),
		}

		result := json.RawMessage(`{"items": [1, 2, 3]}`)
		coerced, coercions, err := validator.CoerceResult(descriptor, result)
		assert.NoError(t, err)
		assert.NotNil(t, coerced)
		assert.Empty(t, coercions)
	})

	t.Run("handles strings and numbers", func(t *testing.T) {
		descriptor := &ToolDescriptor{
			Name: "mixed-tool",
			OutputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"text": {"type": "string"},
					"value": {"type": "number"}
				}
			}`),
		}

		result := json.RawMessage(`{"text": "hello", "value": 42.5}`)
		coerced, coercions, err := validator.CoerceResult(descriptor, result)
		assert.NoError(t, err)
		assert.NotNil(t, coerced)
		assert.Empty(t, coercions)
	})

	t.Run("invalid json", func(t *testing.T) {
		result := json.RawMessage(`{invalid json`)
		coerced, coercions, err := validator.CoerceResult(descriptor, result)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot parse result for coercion")
		assert.Nil(t, coerced)
		assert.Nil(t, coercions)
	})

	t.Run("coercion still fails validation", func(t *testing.T) {
		// Missing required field - coercion can't fix this
		result := json.RawMessage(`{"count": 42}`)
		coerced, coercions, err := validator.CoerceResult(descriptor, result)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "coercion failed")
		assert.Nil(t, coerced)
		assert.Nil(t, coercions)
	})
}

func TestSchemaValidator_coerceValue(t *testing.T) {
	validator := NewSchemaValidator()

	t.Run("handles map recursively", func(t *testing.T) {
		input := map[string]interface{}{
			"a": "text",
			"b": 123.45,
			"c": map[string]interface{}{
				"nested": "value",
			},
		}

		result := validator.coerceValue(input, "root")
		assert.NotNil(t, result)

		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "text", resultMap["a"])
		assert.Equal(t, 123.45, resultMap["b"])

		nestedMap, ok := resultMap["c"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "value", nestedMap["nested"])
	})

	t.Run("handles array recursively", func(t *testing.T) {
		input := []interface{}{
			"string",
			42.0,
			map[string]interface{}{"key": "value"},
			[]interface{}{1.0, 2.0},
		}

		result := validator.coerceValue(input, "root")
		assert.NotNil(t, result)

		resultArray, ok := result.([]interface{})
		require.True(t, ok)
		assert.Len(t, resultArray, 4)
		assert.Equal(t, "string", resultArray[0])
		assert.Equal(t, 42.0, resultArray[1])

		nestedMap, ok := resultArray[2].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "value", nestedMap["key"])

		nestedArray, ok := resultArray[3].([]interface{})
		require.True(t, ok)
		assert.Len(t, nestedArray, 2)
	})

	t.Run("handles primitives", func(t *testing.T) {
		assert.Equal(t, 42.0, validator.coerceValue(42.0, "num"))
		assert.Equal(t, "text", validator.coerceValue("text", "str"))
		assert.Equal(t, true, validator.coerceValue(true, "bool"))
		assert.Nil(t, validator.coerceValue(nil, "null"))
	})
}
