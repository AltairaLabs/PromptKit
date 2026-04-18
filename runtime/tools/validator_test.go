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

		// Verify cache contains the schema (via CacheLen)
		assert.Greater(t, validator.CacheLen(), 0)
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
		// Test that CoerceResult handles valid nested maps via round-trip
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

func TestSchemaValidator_LRUEviction(t *testing.T) {
	// Create a validator with a tiny cache (size 2) to test eviction.
	validator := NewSchemaValidatorWithSize(2)

	schema1 := `{"type":"object","properties":{"a":{"type":"string"}}}`
	schema2 := `{"type":"object","properties":{"b":{"type":"string"}}}`
	schema3 := `{"type":"object","properties":{"c":{"type":"string"}}}`

	// Fill the cache with 2 entries.
	_, err := validator.getSchema(schema1)
	require.NoError(t, err)
	_, err = validator.getSchema(schema2)
	require.NoError(t, err)
	assert.Equal(t, 2, validator.CacheLen())

	// Adding a 3rd should evict the LRU (schema1).
	_, err = validator.getSchema(schema3)
	require.NoError(t, err)
	assert.Equal(t, 2, validator.CacheLen())

	// schema1 should have been evicted (re-add will not increase size beyond 2).
	// Access schema2 to make it MRU, then add schema1 — schema3 should be evicted.
	_, err = validator.getSchema(schema2)
	require.NoError(t, err)
	_, err = validator.getSchema(schema1)
	require.NoError(t, err)
	assert.Equal(t, 2, validator.CacheLen())

	// schema3 was LRU and should be evicted; schema2 and schema1 remain.
	// Re-access schema2 and schema1 to confirm they are still cached.
	_, err = validator.getSchema(schema2)
	assert.NoError(t, err)
	_, err = validator.getSchema(schema1)
	assert.NoError(t, err)
}

func TestSchemaValidator_DefaultCacheSize(t *testing.T) {
	v := NewSchemaValidator()
	assert.Equal(t, DefaultMaxSchemaCacheSize, v.maxSize)
}

func TestSchemaValidatorWithSize_ZeroDefaults(t *testing.T) {
	v := NewSchemaValidatorWithSize(0)
	assert.Equal(t, DefaultMaxSchemaCacheSize, v.maxSize)
}

func TestCoerceArgs(t *testing.T) {
	validator := NewSchemaValidator()

	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {"type": "string"},
			"limit": {"type": "integer"},
			"min_confidence": {"type": "number"},
			"enabled": {"type": "boolean"},
			"tags": {"type": "array", "items": {"type": "string"}}
		}
	}`)

	descriptor := &ToolDescriptor{
		Name:        "test-tool",
		InputSchema: schema,
	}

	t.Run("coerces string to integer", func(t *testing.T) {
		args := json.RawMessage(`{"query": "hello", "limit": "10"}`)
		coerced, coercions, err := validator.CoerceArgs(descriptor, args)
		require.NoError(t, err)
		assert.NotEmpty(t, coercions)

		var result map[string]interface{}
		require.NoError(t, json.Unmarshal(coerced, &result))
		assert.Equal(t, float64(10), result["limit"]) // JSON numbers are float64
	})

	t.Run("coerces string to number", func(t *testing.T) {
		args := json.RawMessage(`{"query": "hello", "min_confidence": "0.85"}`)
		coerced, coercions, err := validator.CoerceArgs(descriptor, args)
		require.NoError(t, err)
		assert.NotEmpty(t, coercions)

		var result map[string]interface{}
		require.NoError(t, json.Unmarshal(coerced, &result))
		assert.Equal(t, 0.85, result["min_confidence"])
	})

	t.Run("coerces string to boolean", func(t *testing.T) {
		args := json.RawMessage(`{"query": "hello", "enabled": "true"}`)
		coerced, coercions, err := validator.CoerceArgs(descriptor, args)
		require.NoError(t, err)
		assert.NotEmpty(t, coercions)

		var result map[string]interface{}
		require.NoError(t, json.Unmarshal(coerced, &result))
		assert.Equal(t, true, result["enabled"])
	})

	t.Run("coerces false string to boolean", func(t *testing.T) {
		args := json.RawMessage(`{"query": "hello", "enabled": "false"}`)
		coerced, _, err := validator.CoerceArgs(descriptor, args)
		require.NoError(t, err)

		var result map[string]interface{}
		require.NoError(t, json.Unmarshal(coerced, &result))
		assert.Equal(t, false, result["enabled"])
	})

	t.Run("no coercion needed — passes through", func(t *testing.T) {
		args := json.RawMessage(`{"query": "hello", "limit": 10, "min_confidence": 0.5, "enabled": true}`)
		coerced, coercions, err := validator.CoerceArgs(descriptor, args)
		require.NoError(t, err)
		assert.Empty(t, coercions)
		assert.Equal(t, args, coerced)
	})

	t.Run("coerces multiple fields at once", func(t *testing.T) {
		args := json.RawMessage(`{"query": "hello", "limit": "5", "min_confidence": "0", "enabled": "true"}`)
		coerced, coercions, err := validator.CoerceArgs(descriptor, args)
		require.NoError(t, err)
		assert.Len(t, coercions, 3)

		var result map[string]interface{}
		require.NoError(t, json.Unmarshal(coerced, &result))
		assert.Equal(t, float64(5), result["limit"])
		assert.Equal(t, float64(0), result["min_confidence"])
		assert.Equal(t, true, result["enabled"])
	})

	t.Run("leaves strings alone", func(t *testing.T) {
		args := json.RawMessage(`{"query": "hello"}`)
		coerced, coercions, err := validator.CoerceArgs(descriptor, args)
		require.NoError(t, err)
		assert.Empty(t, coercions)

		var result map[string]interface{}
		require.NoError(t, json.Unmarshal(coerced, &result))
		assert.Equal(t, "hello", result["query"])
	})

	t.Run("no schema — passes through", func(t *testing.T) {
		noSchema := &ToolDescriptor{Name: "no-schema"}
		args := json.RawMessage(`{"limit": "10"}`)
		coerced, coercions, err := validator.CoerceArgs(noSchema, args)
		require.NoError(t, err)
		assert.Empty(t, coercions)
		assert.Equal(t, args, coerced)
	})

	t.Run("invalid string for integer — returns error", func(t *testing.T) {
		args := json.RawMessage(`{"limit": "not-a-number"}`)
		_, _, err := validator.CoerceArgs(descriptor, args)
		assert.Error(t, err)
	})

	// Extended schema with object and array types
	extSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"content": {"type": "string"},
			"metadata": {"type": "object"},
			"items": {"type": "array"}
		}
	}`)
	extDescriptor := &ToolDescriptor{Name: "ext-tool", InputSchema: extSchema}

	t.Run("coerces string to object", func(t *testing.T) {
		args := json.RawMessage(`{"content": "hello", "metadata": "{}"}`)
		coerced, coercions, err := validator.CoerceArgs(extDescriptor, args)
		require.NoError(t, err)
		assert.NotEmpty(t, coercions)

		var result map[string]interface{}
		require.NoError(t, json.Unmarshal(coerced, &result))
		meta, ok := result["metadata"].(map[string]interface{})
		assert.True(t, ok, "metadata should be an object, got %T", result["metadata"])
		assert.Empty(t, meta)
	})

	t.Run("coerces string to object with content", func(t *testing.T) {
		args := json.RawMessage(`{"content": "hello", "metadata": "{\"key\": \"value\"}"}`)
		coerced, coercions, err := validator.CoerceArgs(extDescriptor, args)
		require.NoError(t, err)
		assert.NotEmpty(t, coercions)

		var result map[string]interface{}
		require.NoError(t, json.Unmarshal(coerced, &result))
		meta, ok := result["metadata"].(map[string]interface{})
		assert.True(t, ok)
		assert.Equal(t, "value", meta["key"])
	})

	t.Run("coerces string to array", func(t *testing.T) {
		args := json.RawMessage(`{"content": "hello", "items": "[1, 2, 3]"}`)
		coerced, coercions, err := validator.CoerceArgs(extDescriptor, args)
		require.NoError(t, err)
		assert.NotEmpty(t, coercions)

		var result map[string]interface{}
		require.NoError(t, json.Unmarshal(coerced, &result))
		items, ok := result["items"].([]interface{})
		assert.True(t, ok, "items should be an array, got %T", result["items"])
		assert.Len(t, items, 3)
	})

	t.Run("invalid JSON string for object — returns error", func(t *testing.T) {
		args := json.RawMessage(`{"metadata": "not json"}`)
		_, _, err := validator.CoerceArgs(extDescriptor, args)
		assert.Error(t, err)
	})
}

// TestCoerceStringValue locks in per-branch behavior of the package-private
// coerceStringValue helper ahead of an S3776 cognitive-complexity refactor.
func TestCoerceStringValue(t *testing.T) {
	t.Run("integer parses plain number", func(t *testing.T) {
		v, err := coerceStringValue("42", "integer")
		require.NoError(t, err)
		assert.Equal(t, int64(42), v)
	})

	t.Run("integer falls back to float when fractional part is zero", func(t *testing.T) {
		v, err := coerceStringValue("5.0", "integer")
		require.NoError(t, err)
		assert.Equal(t, int64(5), v)
	})

	t.Run("integer rejects non-integer float", func(t *testing.T) {
		_, err := coerceStringValue("3.14", "integer")
		assert.Error(t, err)
	})

	t.Run("integer rejects non-numeric string", func(t *testing.T) {
		_, err := coerceStringValue("abc", "integer")
		assert.Error(t, err)
	})

	t.Run("number parses float", func(t *testing.T) {
		v, err := coerceStringValue("0.85", "number")
		require.NoError(t, err)
		assert.Equal(t, 0.85, v)
	})

	t.Run("number rejects non-numeric", func(t *testing.T) {
		_, err := coerceStringValue("abc", "number")
		assert.Error(t, err)
	})

	t.Run("boolean parses strconv forms", func(t *testing.T) {
		for _, s := range []string{"true", "false", "1", "0", "t", "f"} {
			_, err := coerceStringValue(s, "boolean")
			assert.NoError(t, err, "input: %q", s)
		}
	})

	t.Run("boolean accepts yes and no case-insensitively with trim", func(t *testing.T) {
		v, err := coerceStringValue("  YES ", "boolean")
		require.NoError(t, err)
		assert.Equal(t, true, v)

		v, err = coerceStringValue("No", "boolean")
		require.NoError(t, err)
		assert.Equal(t, false, v)
	})

	t.Run("boolean rejects garbage", func(t *testing.T) {
		_, err := coerceStringValue("maybe", "boolean")
		assert.Error(t, err)
	})

	t.Run("object parses JSON map", func(t *testing.T) {
		v, err := coerceStringValue(`{"k":"v","n":1}`, "object")
		require.NoError(t, err)
		m, ok := v.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "v", m["k"])
	})

	t.Run("object rejects non-JSON string", func(t *testing.T) {
		_, err := coerceStringValue("not json", "object")
		assert.Error(t, err)
	})

	t.Run("array parses JSON array", func(t *testing.T) {
		v, err := coerceStringValue(`["a","b",3]`, "array")
		require.NoError(t, err)
		a, ok := v.([]any)
		require.True(t, ok)
		assert.Len(t, a, 3)
	})

	t.Run("array wraps bare string as single-element slice", func(t *testing.T) {
		v, err := coerceStringValue("bare", "array")
		require.NoError(t, err)
		assert.Equal(t, []any{"bare"}, v)
	})

	t.Run("unknown target type returns (nil, nil) for no-op", func(t *testing.T) {
		v, err := coerceStringValue("x", "mystery")
		require.NoError(t, err)
		assert.Nil(t, v)
	})
}
