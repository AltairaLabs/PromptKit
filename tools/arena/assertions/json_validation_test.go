package assertions

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsValidJSONValidator tests the is_valid_json assertion
func TestIsValidJSONValidator(t *testing.T) {
	tests := []struct {
		name       string
		params     map[string]interface{}
		content    string
		wantPassed bool
		wantError  string
	}{
		{
			name:       "Valid JSON object",
			params:     map[string]interface{}{},
			content:    `{"name": "John", "age": 30}`,
			wantPassed: true,
		},
		{
			name:       "Valid JSON array",
			params:     map[string]interface{}{},
			content:    `[1, 2, 3, "test"]`,
			wantPassed: true,
		},
		{
			name:       "Valid JSON string",
			params:     map[string]interface{}{},
			content:    `"simple string"`,
			wantPassed: true,
		},
		{
			name:       "Valid JSON number",
			params:     map[string]interface{}{},
			content:    `42`,
			wantPassed: true,
		},
		{
			name:       "Valid JSON boolean",
			params:     map[string]interface{}{},
			content:    `true`,
			wantPassed: true,
		},
		{
			name:       "Valid JSON null",
			params:     map[string]interface{}{},
			content:    `null`,
			wantPassed: true,
		},
		{
			name:       "Invalid JSON - malformed",
			params:     map[string]interface{}{},
			content:    `{"name": "John", "age": }`,
			wantPassed: false,
			wantError:  "invalid character",
		},
		{
			name:       "Invalid JSON - plain text",
			params:     map[string]interface{}{},
			content:    `This is just plain text`,
			wantPassed: false,
			wantError:  "invalid character",
		},
		{
			name:       "Invalid JSON - missing quote",
			params:     map[string]interface{}{},
			content:    `{"name: "John"}`,
			wantPassed: false,
		},
		{
			name:       "Empty string",
			params:     map[string]interface{}{},
			content:    ``,
			wantPassed: false,
			wantError:  "unexpected end of JSON input",
		},
		{
			name:       "JSON wrapped in markdown code block - allowed",
			params:     map[string]interface{}{"allow_wrapped": true},
			content:    "```json\n{\"name\": \"John\"}\n```",
			wantPassed: true,
		},
		{
			name:       "JSON wrapped in markdown code block - not allowed",
			params:     map[string]interface{}{"allow_wrapped": false},
			content:    "```json\n{\"name\": \"John\"}\n```",
			wantPassed: false,
		},
		{
			name:       "JSON in mixed text with extract enabled",
			params:     map[string]interface{}{"extract_json": true},
			content:    `Here is the data: {"name": "John", "age": 30} - that's the info`,
			wantPassed: true,
		},
		{
			name:       "JSON in mixed text without extract",
			params:     map[string]interface{}{"extract_json": false},
			content:    `Here is the data: {"name": "John", "age": 30}`,
			wantPassed: false,
		},
		{
			name:       "Nested JSON object",
			params:     map[string]interface{}{},
			content:    `{"user": {"name": "John", "address": {"city": "NYC"}}}`,
			wantPassed: true,
		},
		{
			name:       "Complex nested JSON",
			params:     map[string]interface{}{},
			content:    `{"users": [{"name": "John", "age": 30}, {"name": "Jane", "age": 25}]}`,
			wantPassed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := NewIsValidJSONValidator(tt.params)
			result := validator.Validate(tt.content, tt.params)

			assert.Equal(t, tt.wantPassed, result.Passed, "Passed status mismatch")

			if !tt.wantPassed && tt.wantError != "" {
				details, ok := result.Details.(map[string]interface{})
				assert.True(t, ok, "Expected details to be map[string]interface{}")
				if ok {
					error, hasError := details["error"]
					assert.True(t, hasError, "Expected error in details")
					if hasError {
						assert.Contains(t, error, tt.wantError, "Error message should contain expected text")
					}
				}
			}
		})
	}
}

// TestJSONSchemaValidator tests the json_schema assertion
func TestJSONSchemaValidator(t *testing.T) {
	tests := []struct {
		name       string
		params     map[string]interface{}
		content    string
		wantPassed bool
	}{
		{
			name: "Valid against simple schema",
			params: map[string]interface{}{
				"schema": map[string]interface{}{
					"type":     "object",
					"required": []interface{}{"name", "age"},
					"properties": map[string]interface{}{
						"name": map[string]interface{}{"type": "string"},
						"age":  map[string]interface{}{"type": "integer"},
					},
				},
			},
			content:    `{"name": "John", "age": 30}`,
			wantPassed: true,
		},
		{
			name: "Missing required field",
			params: map[string]interface{}{
				"schema": map[string]interface{}{
					"type":     "object",
					"required": []interface{}{"name", "age"},
					"properties": map[string]interface{}{
						"name": map[string]interface{}{"type": "string"},
						"age":  map[string]interface{}{"type": "integer"},
					},
				},
			},
			content:    `{"name": "John"}`,
			wantPassed: false,
		},
		{
			name: "Wrong type for field",
			params: map[string]interface{}{
				"schema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"age": map[string]interface{}{"type": "integer"},
					},
				},
			},
			content:    `{"age": "thirty"}`,
			wantPassed: false,
		},
		{
			name: "String with minLength constraint - valid",
			params: map[string]interface{}{
				"schema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name": map[string]interface{}{
							"type":      "string",
							"minLength": 3,
						},
					},
				},
			},
			content:    `{"name": "John"}`,
			wantPassed: true,
		},
		{
			name: "String with minLength constraint - invalid",
			params: map[string]interface{}{
				"schema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name": map[string]interface{}{
							"type":      "string",
							"minLength": 10,
						},
					},
				},
			},
			content:    `{"name": "John"}`,
			wantPassed: false,
		},
		{
			name: "Number with minimum constraint - valid",
			params: map[string]interface{}{
				"schema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"age": map[string]interface{}{
							"type":    "integer",
							"minimum": 0,
							"maximum": 150,
						},
					},
				},
			},
			content:    `{"age": 30}`,
			wantPassed: true,
		},
		{
			name: "Number exceeds maximum",
			params: map[string]interface{}{
				"schema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"age": map[string]interface{}{
							"type":    "integer",
							"maximum": 150,
						},
					},
				},
			},
			content:    `{"age": 200}`,
			wantPassed: false,
		},
		{
			name: "Enum validation - valid",
			params: map[string]interface{}{
				"schema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"status": map[string]interface{}{
							"type": "string",
							"enum": []interface{}{"active", "inactive", "pending"},
						},
					},
				},
			},
			content:    `{"status": "active"}`,
			wantPassed: true,
		},
		{
			name: "Enum validation - invalid",
			params: map[string]interface{}{
				"schema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"status": map[string]interface{}{
							"type": "string",
							"enum": []interface{}{"active", "inactive", "pending"},
						},
					},
				},
			},
			content:    `{"status": "unknown"}`,
			wantPassed: false,
		},
		{
			name: "Array validation - valid",
			params: map[string]interface{}{
				"schema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"tags": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"type": "string",
							},
						},
					},
				},
			},
			content:    `{"tags": ["go", "testing", "json"]}`,
			wantPassed: true,
		},
		{
			name: "Array with wrong item type",
			params: map[string]interface{}{
				"schema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"tags": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"type": "string",
							},
						},
					},
				},
			},
			content:    `{"tags": ["go", 123, "json"]}`,
			wantPassed: false,
		},
		{
			name: "Nested object validation - valid",
			params: map[string]interface{}{
				"schema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"user": map[string]interface{}{
							"type":     "object",
							"required": []interface{}{"name"},
							"properties": map[string]interface{}{
								"name": map[string]interface{}{"type": "string"},
							},
						},
					},
				},
			},
			content:    `{"user": {"name": "John"}}`,
			wantPassed: true,
		},
		{
			name: "Invalid JSON",
			params: map[string]interface{}{
				"schema": map[string]interface{}{
					"type": "object",
				},
			},
			content:    `{invalid json}`,
			wantPassed: false,
		},
		{
			name: "Additional properties not allowed",
			params: map[string]interface{}{
				"schema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name": map[string]interface{}{"type": "string"},
					},
					"additionalProperties": false,
				},
			},
			content:    `{"name": "John", "age": 30}`,
			wantPassed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := NewJSONSchemaValidator(tt.params)
			result := validator.Validate(tt.content, tt.params)

			assert.Equal(t, tt.wantPassed, result.Passed, "Passed status mismatch")

			if !result.Passed {
				// Verify we have error details
				assert.NotNil(t, result.Details, "Expected details for failed validation")
			}
		})
	}
}

// TestJSONPathValidator tests the json_path assertion
func TestJSONPathValidator(t *testing.T) {
	tests := []struct {
		name       string
		params     map[string]interface{}
		content    string
		wantPassed bool
	}{
		{
			name: "Simple field extraction - match",
			params: map[string]interface{}{
				"expression": "name",
				"expected":   "John",
			},
			content:    `{"name": "John", "age": 30}`,
			wantPassed: true,
		},
		{
			name: "Simple field extraction - no match",
			params: map[string]interface{}{
				"expression": "name",
				"expected":   "Jane",
			},
			content:    `{"name": "John", "age": 30}`,
			wantPassed: false,
		},
		{
			name: "Nested field extraction",
			params: map[string]interface{}{
				"expression": "user.address.city",
				"expected":   "NYC",
			},
			content:    `{"user": {"address": {"city": "NYC"}}}`,
			wantPassed: true,
		},
		{
			name: "Array element extraction",
			params: map[string]interface{}{
				"expression": "items[0]",
				"expected":   "apple",
			},
			content:    `{"items": ["apple", "banana", "orange"]}`,
			wantPassed: true,
		},
		{
			name: "Array slice",
			params: map[string]interface{}{
				"expression": "items[0:2]",
				"expected":   []interface{}{"apple", "banana"},
			},
			content:    `{"items": ["apple", "banana", "orange"]}`,
			wantPassed: true,
		},
		{
			name: "Wildcard projection",
			params: map[string]interface{}{
				"expression": "users[*].name",
				"expected":   []interface{}{"John", "Jane"},
			},
			content:    `{"users": [{"name": "John", "age": 30}, {"name": "Jane", "age": 25}]}`,
			wantPassed: true,
		},
		{
			name: "Filter expression - matching",
			params: map[string]interface{}{
				"expression":  "products[?price > `100`]",
				"min_results": 1,
			},
			content:    `{"products": [{"name": "laptop", "price": 1200}, {"name": "mouse", "price": 25}]}`,
			wantPassed: true,
		},
		{
			name: "Filter expression - no matches",
			params: map[string]interface{}{
				"expression":  "products[?price > `2000`]",
				"min_results": 1,
			},
			content:    `{"products": [{"name": "laptop", "price": 1200}, {"name": "mouse", "price": 25}]}`,
			wantPassed: false,
		},
		{
			name: "Length function",
			params: map[string]interface{}{
				"expression": "length(items)",
				"expected":   float64(3),
			},
			content:    `{"items": ["a", "b", "c"]}`,
			wantPassed: true,
		},
		{
			name: "Numeric comparison - min constraint",
			params: map[string]interface{}{
				"expression": "price",
				"min":        100.0,
			},
			content:    `{"price": 150}`,
			wantPassed: true,
		},
		{
			name: "Numeric comparison - max constraint",
			params: map[string]interface{}{
				"expression": "price",
				"max":        1000.0,
			},
			content:    `{"price": 1500}`,
			wantPassed: false,
		},
		{
			name: "Numeric comparison - within range",
			params: map[string]interface{}{
				"expression": "price",
				"min":        100.0,
				"max":        2000.0,
			},
			content:    `{"price": 1500}`,
			wantPassed: true,
		},
		{
			name: "Contains check - array",
			params: map[string]interface{}{
				"expression": "tags",
				"contains":   []interface{}{"golang", "testing"},
			},
			content:    `{"tags": ["golang", "testing", "json", "validation"]}`,
			wantPassed: true,
		},
		{
			name: "Contains check - missing element",
			params: map[string]interface{}{
				"expression": "tags",
				"contains":   []interface{}{"python", "rust"},
			},
			content:    `{"tags": ["golang", "testing"]}`,
			wantPassed: false,
		},
		{
			name: "Array result count - min",
			params: map[string]interface{}{
				"expression":  "items",
				"min_results": 3,
			},
			content:    `{"items": ["a", "b", "c", "d"]}`,
			wantPassed: true,
		},
		{
			name: "Array result count - max exceeded",
			params: map[string]interface{}{
				"expression":  "items",
				"max_results": 2,
			},
			content:    `{"items": ["a", "b", "c", "d"]}`,
			wantPassed: false,
		},
		{
			name: "Pipe expression",
			params: map[string]interface{}{
				"expression": "users[*].age | max(@)",
				"expected":   float64(30),
			},
			content:    `{"users": [{"age": 30}, {"age": 25}, {"age": 28}]}`,
			wantPassed: true,
		},
		{
			name: "Invalid JSON",
			params: map[string]interface{}{
				"expression": "name",
				"expected":   "John",
			},
			content:    `{invalid}`,
			wantPassed: false,
		},
		{
			name: "Non-existent path",
			params: map[string]interface{}{
				"expression": "missing.field",
				"expected":   "value",
			},
			content:    `{"name": "John"}`,
			wantPassed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := NewJSONPathValidator(tt.params)
			result := validator.Validate(tt.content, tt.params)

			assert.Equal(t, tt.wantPassed, result.Passed, "Passed status mismatch")

			if !result.Passed {
				// Verify we have error details
				assert.NotNil(t, result.Details, "Expected details for failed validation")
			}
		})
	}
}

// TestJSONValidation_Integration tests all JSON validators together
func TestJSONValidation_Integration(t *testing.T) {
	content := `{
		"user": {
			"name": "John Doe",
			"email": "john@example.com",
			"age": 30
		},
		"products": [
			{"name": "laptop", "price": 1200},
			{"name": "mouse", "price": 25}
		],
		"total": 1225
	}`

	// Test 1: Valid JSON check
	t.Run("Valid JSON", func(t *testing.T) {
		validator := NewIsValidJSONValidator(map[string]interface{}{})
		result := validator.Validate(content, map[string]interface{}{})
		assert.True(t, result.Passed, "Content should be valid JSON")
	})

	// Test 2: Schema validation
	t.Run("Schema validation", func(t *testing.T) {
		params := map[string]interface{}{
			"schema": map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"user", "products", "total"},
				"properties": map[string]interface{}{
					"user": map[string]interface{}{
						"type":     "object",
						"required": []interface{}{"name", "email"},
						"properties": map[string]interface{}{
							"name": map[string]interface{}{"type": "string"},
							"email": map[string]interface{}{
								"type":   "string",
								"format": "email",
							},
							"age": map[string]interface{}{
								"type":    "integer",
								"minimum": 0,
							},
						},
					},
					"products": map[string]interface{}{
						"type": "array",
					},
					"total": map[string]interface{}{
						"type": "number",
					},
				},
			},
		}
		validator := NewJSONSchemaValidator(params)
		result := validator.Validate(content, params)
		assert.True(t, result.Passed, "Content should match schema")
	})

	// Test 3: JMESPath queries
	t.Run("JMESPath queries", func(t *testing.T) {
		tests := []struct {
			name   string
			params map[string]interface{}
		}{
			{
				name: "Extract user name",
				params: map[string]interface{}{
					"expression": "user.name",
					"expected":   "John Doe",
				},
			},
			{
				name: "Check total",
				params: map[string]interface{}{
					"expression": "total",
					"expected":   float64(1225),
				},
			},
			{
				name: "Filter expensive products",
				params: map[string]interface{}{
					"expression":  "products[?price > `100`]",
					"min_results": 1,
				},
			},
			{
				name: "Count products",
				params: map[string]interface{}{
					"expression": "length(products)",
					"expected":   float64(2),
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				validator := NewJSONPathValidator(tt.params)
				result := validator.Validate(content, tt.params)
				assert.True(t, result.Passed, "JMESPath query should pass")
			})
		}
	})
}
