package tools

// llm_tool_calling_test.go exercises the CoerceArgs → ValidateArgs pipeline
// with realistic LLM tool-calling patterns. These tests simulate how different
// LLMs (strong and weak) actually format tool arguments, and verify that the
// coercion + validation pipeline handles them gracefully.
//
// The test suite is organized by tool schema (matching real capability tools)
// and by LLM behaviour pattern (not by specific fix). If a test fails, it
// means our pipeline can't handle that LLM's output — which is a real bug.

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test helper: simulate the full CoerceArgs → ValidateArgs pipeline
// ---------------------------------------------------------------------------

// coerceAndValidate runs the same pipeline as Registry.Execute:
// CoerceArgs (normalize LLM output) → ValidateArgs (schema check).
// Returns the final args, any coercions applied, and the first error.
func coerceAndValidate(t *testing.T, sv *SchemaValidator, desc *ToolDescriptor, rawArgs string) (json.RawMessage, []Coercion, error) {
	t.Helper()
	args := json.RawMessage(rawArgs)

	coerced, coercions, coerceErr := sv.CoerceArgs(desc, args)
	if coerceErr != nil {
		return nil, nil, coerceErr
	}
	if coerced != nil {
		args = coerced
	}

	valErr := sv.ValidateArgs(desc, args)
	if valErr != nil {
		return args, coercions, valErr
	}

	return args, coercions, nil
}

// ---------------------------------------------------------------------------
// Realistic tool schemas matching capability-registered tools
// ---------------------------------------------------------------------------

// memoryRecallDescriptor matches runtime/memory/tools.go memory__recall
var memoryRecallDescriptor = &ToolDescriptor{
	Name: "memory__recall",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "What to search for in memory."},
			"types": {"type": "array", "items": {"type": "string"}, "description": "Filter by memory type."},
			"limit": {"type": "integer", "description": "Maximum number of results."},
			"min_confidence": {"type": "number", "description": "Minimum confidence threshold (0.0-1.0)."}
		},
		"required": ["query"]
	}`),
}

// memoryRememberDescriptor matches runtime/memory/tools.go memory__remember
var memoryRememberDescriptor = &ToolDescriptor{
	Name: "memory__remember",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"content": {"type": "string", "description": "What to remember."},
			"type": {"type": "string", "description": "Memory category."},
			"confidence": {"type": "number", "description": "How confident you are (0.0-1.0)."},
			"metadata": {"type": "object", "description": "Optional structured data."}
		},
		"required": ["content"]
	}`),
}

// memoryListDescriptor matches runtime/memory/tools.go memory__list
var memoryListDescriptor = &ToolDescriptor{
	Name: "memory__list",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"types": {"type": "array", "items": {"type": "string"}, "description": "Filter by memory type."},
			"limit": {"type": "integer", "description": "Maximum number of results."}
		}
	}`),
}

// memoryForgetDescriptor matches runtime/memory/tools.go memory__forget
var memoryForgetDescriptor = &ToolDescriptor{
	Name: "memory__forget",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"memory_id": {"type": "string", "description": "The ID of the memory to forget."}
		},
		"required": ["memory_id"]
	}`),
}

// workflowTransitionDescriptor matches runtime/workflow/transition_tool.go
var workflowTransitionDescriptor = &ToolDescriptor{
	Name: "workflow__transition",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"event": {"type": "string", "enum": ["Escalate", "Resolve", "Clarify"], "description": "The workflow event to trigger."},
			"context": {"type": "string", "description": "Summary of relevant context."}
		},
		"required": ["event", "context"]
	}`),
}

// workflowArtifactDescriptor matches runtime/workflow/artifact_tool.go
var workflowArtifactDescriptor = &ToolDescriptor{
	Name: "workflow__set_artifact",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "enum": ["customer_email", "order_id", "resolution_notes"], "description": "The artifact name to set."},
			"value": {"type": "string", "description": "The artifact value."}
		},
		"required": ["name", "value"]
	}`),
}

// simpleToolDescriptor — a basic tool for testing general patterns
var simpleToolDescriptor = &ToolDescriptor{
	Name: "get_weather",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"city": {"type": "string", "description": "City name."},
			"units": {"type": "string", "enum": ["celsius", "fahrenheit"], "description": "Temperature units."},
			"include_forecast": {"type": "boolean", "description": "Include 5-day forecast."},
			"days": {"type": "integer", "description": "Number of forecast days."}
		},
		"required": ["city"]
	}`),
}

// ---------------------------------------------------------------------------
// Tests: Strong LLM behaviour (should all pass — baseline)
// ---------------------------------------------------------------------------

func TestLLMToolCalling_StrongLLM_CorrectCalls(t *testing.T) {
	sv := NewSchemaValidator()

	t.Run("memory__recall with all fields", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, memoryRecallDescriptor,
			`{"query": "user preferences", "types": ["preference", "fact"], "limit": 5, "min_confidence": 0.7}`)
		assert.NoError(t, err)
	})

	t.Run("memory__recall with required only", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, memoryRecallDescriptor,
			`{"query": "user preferences"}`)
		assert.NoError(t, err)
	})

	t.Run("memory__remember with all fields", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, memoryRememberDescriptor,
			`{"content": "User prefers dark mode", "type": "preference", "confidence": 0.9, "metadata": {"source": "chat"}}`)
		assert.NoError(t, err)
	})

	t.Run("memory__remember with required only", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, memoryRememberDescriptor,
			`{"content": "User prefers dark mode"}`)
		assert.NoError(t, err)
	})

	t.Run("memory__list no args", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, memoryListDescriptor, `{}`)
		assert.NoError(t, err)
	})

	t.Run("memory__forget with required", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, memoryForgetDescriptor,
			`{"memory_id": "abc-123"}`)
		assert.NoError(t, err)
	})

	t.Run("workflow__transition correct enum", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, workflowTransitionDescriptor,
			`{"event": "Escalate", "context": "Customer is frustrated"}`)
		assert.NoError(t, err)
	})

	t.Run("workflow__set_artifact correct enum", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, workflowArtifactDescriptor,
			`{"name": "customer_email", "value": "test@example.com"}`)
		assert.NoError(t, err)
	})

	t.Run("simple tool correct call", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, simpleToolDescriptor,
			`{"city": "London", "units": "celsius", "include_forecast": true, "days": 5}`)
		assert.NoError(t, err)
	})
}

// ---------------------------------------------------------------------------
// Tests: Null values for optional fields (Ollama, Llama, Mistral)
// ---------------------------------------------------------------------------

func TestLLMToolCalling_NullOptionalFields(t *testing.T) {
	sv := NewSchemaValidator()

	t.Run("memory__remember all optional fields null", func(t *testing.T) {
		// Ollama consistently sends null for all optional fields
		_, _, err := coerceAndValidate(t, sv, memoryRememberDescriptor,
			`{"content": "test value", "confidence": null, "metadata": null, "type": null}`)
		assert.NoError(t, err, "null optional fields should be stripped before validation")
	})

	t.Run("memory__recall optional fields null", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, memoryRecallDescriptor,
			`{"query": "preferences", "types": null, "limit": null, "min_confidence": null}`)
		assert.NoError(t, err, "null optional fields should be stripped before validation")
	})

	t.Run("memory__list all fields null", func(t *testing.T) {
		// All fields are optional in memory__list
		_, _, err := coerceAndValidate(t, sv, memoryListDescriptor,
			`{"types": null, "limit": null}`)
		assert.NoError(t, err, "null optional fields should be stripped before validation")
	})

	t.Run("simple tool optional fields null", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, simpleToolDescriptor,
			`{"city": "London", "units": null, "include_forecast": null, "days": null}`)
		assert.NoError(t, err, "null optional fields should be stripped before validation")
	})

	t.Run("null required field should still fail", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, memoryRecallDescriptor,
			`{"query": null, "types": null}`)
		assert.Error(t, err, "null required field should fail validation")
	})

	t.Run("null required field in workflow should fail", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, workflowTransitionDescriptor,
			`{"event": null, "context": "some context"}`)
		assert.Error(t, err, "null required field should fail validation")
	})
}

// ---------------------------------------------------------------------------
// Tests: Empty strings for optional fields (GPT-3.5, smaller models)
// ---------------------------------------------------------------------------

func TestLLMToolCalling_EmptyStringOptionalFields(t *testing.T) {
	sv := NewSchemaValidator()

	t.Run("empty string for optional number field", func(t *testing.T) {
		// LLM sends "" instead of omitting confidence
		_, _, err := coerceAndValidate(t, sv, memoryRememberDescriptor,
			`{"content": "test", "confidence": ""}`)
		assert.NoError(t, err, "empty string for optional number should be stripped")
	})

	t.Run("empty string for optional integer field", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, memoryRecallDescriptor,
			`{"query": "test", "limit": ""}`)
		assert.NoError(t, err, "empty string for optional integer should be stripped")
	})

	t.Run("empty string for optional object field", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, memoryRememberDescriptor,
			`{"content": "test", "metadata": ""}`)
		assert.NoError(t, err, "empty string for optional object should be stripped")
	})

	t.Run("empty string for optional array field", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, memoryRecallDescriptor,
			`{"query": "test", "types": ""}`)
		assert.NoError(t, err, "empty string for optional array should be stripped")
	})

	t.Run("empty string for optional boolean field", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, simpleToolDescriptor,
			`{"city": "London", "include_forecast": ""}`)
		assert.NoError(t, err, "empty string for optional boolean should be stripped")
	})

	t.Run("empty string for optional string field is valid", func(t *testing.T) {
		// Empty string IS a valid string — don't strip it
		_, _, err := coerceAndValidate(t, sv, memoryRememberDescriptor,
			`{"content": "test", "type": ""}`)
		assert.NoError(t, err, "empty string for optional string field should be valid")
	})

	t.Run("empty string for required string field is valid", func(t *testing.T) {
		// Empty string is still a string — schema doesn't enforce minLength
		_, _, err := coerceAndValidate(t, sv, memoryRecallDescriptor,
			`{"query": ""}`)
		assert.NoError(t, err, "empty string for required string should be valid (no minLength)")
	})
}

// ---------------------------------------------------------------------------
// Tests: String-encoded values (existing coercion, regression check)
// ---------------------------------------------------------------------------

func TestLLMToolCalling_StringEncodedValues(t *testing.T) {
	sv := NewSchemaValidator()

	t.Run("string-encoded integer", func(t *testing.T) {
		// LLM sends "5" instead of 5
		args, coercions, err := coerceAndValidate(t, sv, memoryRecallDescriptor,
			`{"query": "test", "limit": "5"}`)
		assert.NoError(t, err)
		assert.NotEmpty(t, coercions, "should have coerced string to integer")
		// Verify the coerced value
		var data map[string]any
		require.NoError(t, json.Unmarshal(args, &data))
		assert.Equal(t, float64(5), data["limit"]) // JSON numbers are float64
	})

	t.Run("string-encoded number", func(t *testing.T) {
		// LLM sends "0.8" instead of 0.8
		args, coercions, err := coerceAndValidate(t, sv, memoryRememberDescriptor,
			`{"content": "test", "confidence": "0.8"}`)
		assert.NoError(t, err)
		assert.NotEmpty(t, coercions)
		var data map[string]any
		require.NoError(t, json.Unmarshal(args, &data))
		assert.Equal(t, 0.8, data["confidence"])
	})

	t.Run("string-encoded boolean", func(t *testing.T) {
		// LLM sends "true" instead of true
		args, coercions, err := coerceAndValidate(t, sv, simpleToolDescriptor,
			`{"city": "London", "include_forecast": "true"}`)
		assert.NoError(t, err)
		assert.NotEmpty(t, coercions)
		var data map[string]any
		require.NoError(t, json.Unmarshal(args, &data))
		assert.Equal(t, true, data["include_forecast"])
	})

	t.Run("string-encoded object", func(t *testing.T) {
		// LLM sends metadata as string-encoded JSON
		_, _, err := coerceAndValidate(t, sv, memoryRememberDescriptor,
			`{"content": "test", "metadata": "{\"source\": \"chat\"}"}`)
		assert.NoError(t, err)
	})

	t.Run("string-encoded array", func(t *testing.T) {
		// LLM sends types as string-encoded JSON array
		_, _, err := coerceAndValidate(t, sv, memoryRecallDescriptor,
			`{"query": "test", "types": "[\"preference\", \"fact\"]"}`)
		assert.NoError(t, err)
	})
}

// ---------------------------------------------------------------------------
// Tests: Bare string instead of array (weak models)
// ---------------------------------------------------------------------------

func TestLLMToolCalling_BareStringForArray(t *testing.T) {
	sv := NewSchemaValidator()

	t.Run("bare string for array field", func(t *testing.T) {
		// LLM sends "preference" instead of ["preference"]
		args, _, err := coerceAndValidate(t, sv, memoryRecallDescriptor,
			`{"query": "test", "types": "preference"}`)
		assert.NoError(t, err, "bare string should be wrapped into single-element array")
		if err == nil {
			var data map[string]any
			require.NoError(t, json.Unmarshal(args, &data))
			types, ok := data["types"].([]any)
			require.True(t, ok, "types should be an array after coercion")
			assert.Equal(t, []any{"preference"}, types)
		}
	})

	t.Run("bare string for array in memory__list", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, memoryListDescriptor,
			`{"types": "episodic"}`)
		assert.NoError(t, err, "bare string should be wrapped into single-element array")
	})
}

// ---------------------------------------------------------------------------
// Tests: Enum case mismatch (weak models)
// ---------------------------------------------------------------------------

func TestLLMToolCalling_EnumCaseMismatch(t *testing.T) {
	sv := NewSchemaValidator()

	t.Run("lowercase enum value", func(t *testing.T) {
		// LLM sends "escalate" instead of "Escalate"
		args, _, err := coerceAndValidate(t, sv, workflowTransitionDescriptor,
			`{"event": "escalate", "context": "Customer needs help"}`)
		assert.NoError(t, err, "enum case should be normalized")
		if err == nil {
			var data map[string]any
			require.NoError(t, json.Unmarshal(args, &data))
			assert.Equal(t, "Escalate", data["event"])
		}
	})

	t.Run("uppercase enum value", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, workflowTransitionDescriptor,
			`{"event": "RESOLVE", "context": "Issue fixed"}`)
		assert.NoError(t, err, "enum case should be normalized")
	})

	t.Run("mixed case enum value", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, workflowTransitionDescriptor,
			`{"event": "cLaRiFy", "context": "Need more info"}`)
		assert.NoError(t, err, "enum case should be normalized")
	})

	t.Run("artifact name lowercase", func(t *testing.T) {
		// Artifact names are typically snake_case, so this tests that exact match still works
		_, _, err := coerceAndValidate(t, sv, workflowArtifactDescriptor,
			`{"name": "customer_email", "value": "test@example.com"}`)
		assert.NoError(t, err)
	})

	t.Run("artifact name wrong case", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, workflowArtifactDescriptor,
			`{"name": "Customer_Email", "value": "test@example.com"}`)
		assert.NoError(t, err, "enum case should be normalized for artifacts too")
	})

	t.Run("enum value not in list at all should fail", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, workflowTransitionDescriptor,
			`{"event": "Cancel", "context": "Cancelling"}`)
		assert.Error(t, err, "completely wrong enum value should fail validation")
	})

	t.Run("simple tool enum case mismatch", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, simpleToolDescriptor,
			`{"city": "London", "units": "Celsius"}`)
		assert.NoError(t, err, "enum case should be normalized")
	})
}

// ---------------------------------------------------------------------------
// Tests: Whitespace issues (various LLMs)
// ---------------------------------------------------------------------------

func TestLLMToolCalling_WhitespaceIssues(t *testing.T) {
	sv := NewSchemaValidator()

	t.Run("trailing whitespace in enum value", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, workflowTransitionDescriptor,
			`{"event": "Escalate ", "context": "Customer needs help"}`)
		assert.NoError(t, err, "trailing whitespace in enum should be trimmed")
	})

	t.Run("leading whitespace in enum value", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, workflowTransitionDescriptor,
			`{"event": " Escalate", "context": "Customer needs help"}`)
		assert.NoError(t, err, "leading whitespace in enum should be trimmed")
	})

	t.Run("whitespace in regular string is preserved", func(t *testing.T) {
		// Don't trim whitespace from non-enum string fields
		args, _, err := coerceAndValidate(t, sv, memoryRecallDescriptor,
			`{"query": " user preferences "}`)
		assert.NoError(t, err)
		var data map[string]any
		require.NoError(t, json.Unmarshal(args, &data))
		assert.Equal(t, " user preferences ", data["query"], "non-enum string whitespace should be preserved")
	})
}

// ---------------------------------------------------------------------------
// Tests: Boolean variants (weak models)
// ---------------------------------------------------------------------------

func TestLLMToolCalling_BooleanVariants(t *testing.T) {
	sv := NewSchemaValidator()

	t.Run("yes/no for boolean", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, simpleToolDescriptor,
			`{"city": "London", "include_forecast": "yes"}`)
		assert.NoError(t, err, "\"yes\" should coerce to true for boolean fields")
	})

	t.Run("no for boolean", func(t *testing.T) {
		args, _, err := coerceAndValidate(t, sv, simpleToolDescriptor,
			`{"city": "London", "include_forecast": "no"}`)
		assert.NoError(t, err, "\"no\" should coerce to false for boolean fields")
		if err == nil {
			var data map[string]any
			require.NoError(t, json.Unmarshal(args, &data))
			assert.Equal(t, false, data["include_forecast"])
		}
	})

	t.Run("Yes capitalized for boolean", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, simpleToolDescriptor,
			`{"city": "London", "include_forecast": "Yes"}`)
		assert.NoError(t, err, "\"Yes\" should coerce to true for boolean fields")
	})

	t.Run("1/0 for boolean", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, simpleToolDescriptor,
			`{"city": "London", "include_forecast": "1"}`)
		assert.NoError(t, err, "\"1\" should coerce to true for boolean fields")
	})

	t.Run("integer 1 for boolean", func(t *testing.T) {
		// Some LLMs send integer 1 instead of true
		_, _, err := coerceAndValidate(t, sv, simpleToolDescriptor,
			`{"city": "London", "include_forecast": 1}`)
		assert.NoError(t, err, "integer 1 should be coerced to true for boolean fields")
	})

	t.Run("integer 0 for boolean", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, simpleToolDescriptor,
			`{"city": "London", "include_forecast": 0}`)
		assert.NoError(t, err, "integer 0 should be coerced to false for boolean fields")
	})
}

// ---------------------------------------------------------------------------
// Tests: Number type confusion (weak models)
// ---------------------------------------------------------------------------

func TestLLMToolCalling_NumberTypeConfusion(t *testing.T) {
	sv := NewSchemaValidator()

	t.Run("float for integer field", func(t *testing.T) {
		// LLM sends 5.0 instead of 5 for integer field
		_, _, err := coerceAndValidate(t, sv, memoryRecallDescriptor,
			`{"query": "test", "limit": 5.0}`)
		assert.NoError(t, err, "float with .0 should be accepted for integer fields")
	})

	t.Run("float with fractional for integer field should fail", func(t *testing.T) {
		// 5.5 is not a valid integer
		_, _, err := coerceAndValidate(t, sv, memoryRecallDescriptor,
			`{"query": "test", "limit": 5.5}`)
		assert.Error(t, err, "float with fractional part should fail for integer fields")
	})

	t.Run("negative number for confidence", func(t *testing.T) {
		// Schema doesn't enforce min/max, so this should pass validation
		_, _, err := coerceAndValidate(t, sv, memoryRememberDescriptor,
			`{"content": "test", "confidence": -0.5}`)
		assert.NoError(t, err, "negative number should be valid (no min constraint in schema)")
	})

	t.Run("string-encoded float for integer field", func(t *testing.T) {
		// LLM sends "5.0" for integer field
		_, _, err := coerceAndValidate(t, sv, memoryRecallDescriptor,
			`{"query": "test", "limit": "5.0"}`)
		assert.NoError(t, err, "string \"5.0\" should coerce to integer 5")
	})
}

// ---------------------------------------------------------------------------
// Tests: Extra/unknown fields (various LLMs)
// ---------------------------------------------------------------------------

func TestLLMToolCalling_ExtraFields(t *testing.T) {
	sv := NewSchemaValidator()

	t.Run("extra fields are ignored by default", func(t *testing.T) {
		// LLMs sometimes add extra fields not in the schema
		_, _, err := coerceAndValidate(t, sv, memoryRecallDescriptor,
			`{"query": "test", "extra_field": "something", "another": 42}`)
		assert.NoError(t, err, "extra fields should be ignored (no additionalProperties: false)")
	})

	t.Run("extra fields with workflow tool", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, workflowTransitionDescriptor,
			`{"event": "Escalate", "context": "Help needed", "reason": "customer angry"}`)
		assert.NoError(t, err, "extra fields should be ignored")
	})
}

// ---------------------------------------------------------------------------
// Tests: Combined weak-LLM patterns (realistic multi-issue calls)
// ---------------------------------------------------------------------------

func TestLLMToolCalling_CombinedWeakPatterns(t *testing.T) {
	sv := NewSchemaValidator()

	t.Run("Ollama-style: nulls + string-encoded values", func(t *testing.T) {
		// Real Ollama output: required fields present, optionals null or string-encoded
		_, _, err := coerceAndValidate(t, sv, memoryRememberDescriptor,
			`{"content": "user prefers dark mode", "confidence": "0.8", "metadata": null, "type": null}`)
		assert.NoError(t, err, "combined null + string coercion should work")
	})

	t.Run("Llama-style: bare strings + nulls", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, memoryRecallDescriptor,
			`{"query": "food preferences", "types": "preference", "limit": null, "min_confidence": null}`)
		assert.NoError(t, err, "combined bare string array + null stripping should work")
	})

	t.Run("Mistral-style: empty strings everywhere", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, memoryRememberDescriptor,
			`{"content": "test", "confidence": "", "metadata": "", "type": ""}`)
		assert.NoError(t, err, "empty strings for non-string optional fields should be stripped")
	})

	t.Run("weak model workflow: wrong case + trailing space", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, workflowTransitionDescriptor,
			`{"event": "escalate ", "context": "needs help"}`)
		assert.NoError(t, err, "combined case normalization + whitespace trimming should work")
	})

	t.Run("weak model: string numbers + null + bare array", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, memoryRecallDescriptor,
			`{"query": "test", "types": "fact", "limit": "10", "min_confidence": null}`)
		assert.NoError(t, err, "string int + bare array + null should all coerce")
	})

	t.Run("weak model: yes/no boolean + null optional", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, simpleToolDescriptor,
			`{"city": "London", "include_forecast": "yes", "days": null, "units": null}`)
		assert.NoError(t, err, "yes→true + null stripping should work together")
	})

	t.Run("weak model: integer boolean + empty string enum", func(t *testing.T) {
		// LLM sends 1 for boolean and empty string for optional enum
		_, _, err := coerceAndValidate(t, sv, simpleToolDescriptor,
			`{"city": "London", "include_forecast": 1, "units": ""}`)
		assert.NoError(t, err, "integer boolean + empty optional enum should both be handled")
	})
}

// ---------------------------------------------------------------------------
// Tests: Edge cases that should still fail (guard rails)
// ---------------------------------------------------------------------------

func TestLLMToolCalling_ShouldStillFail(t *testing.T) {
	sv := NewSchemaValidator()

	t.Run("completely missing required fields", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, memoryRecallDescriptor, `{}`)
		assert.Error(t, err)
	})

	t.Run("wrong type for required field", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, memoryRecallDescriptor,
			`{"query": 42}`)
		assert.Error(t, err, "number for required string should fail")
	})

	t.Run("completely invalid enum value", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, workflowTransitionDescriptor,
			`{"event": "Destroy", "context": "bye"}`)
		assert.Error(t, err, "enum value not matching any entry (even case-insensitive) should fail")
	})

	t.Run("null for all required fields", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, workflowTransitionDescriptor,
			`{"event": null, "context": null}`)
		assert.Error(t, err, "null for required fields should fail")
	})

	t.Run("empty object when fields required", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, workflowTransitionDescriptor, `{}`)
		assert.Error(t, err)
	})

	t.Run("array for string field", func(t *testing.T) {
		_, _, err := coerceAndValidate(t, sv, memoryRecallDescriptor,
			`{"query": ["a", "b"]}`)
		assert.Error(t, err, "array for string field should fail")
	})
}
