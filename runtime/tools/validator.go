package tools

import (
	"encoding/json"
	"fmt"

	"github.com/xeipuuv/gojsonschema"
)

// SchemaValidator handles JSON schema validation for tool inputs and outputs
type SchemaValidator struct {
	cache map[string]*gojsonschema.Schema
}

// NewSchemaValidator creates a new schema validator
func NewSchemaValidator() *SchemaValidator {
	return &SchemaValidator{
		cache: make(map[string]*gojsonschema.Schema),
	}
}

// ValidateArgs validates tool arguments against the input schema
func (sv *SchemaValidator) ValidateArgs(descriptor *ToolDescriptor, args json.RawMessage) error {
	schema, err := sv.getSchema(string(descriptor.InputSchema))
	if err != nil {
		return fmt.Errorf("invalid input schema for tool %s: %w", descriptor.Name, err)
	}

	argsLoader := gojsonschema.NewBytesLoader(args)
	result, err := schema.Validate(argsLoader)
	if err != nil {
		return fmt.Errorf("validation error for tool %s: %w", descriptor.Name, err)
	}

	if !result.Valid() {
		errors := make([]string, len(result.Errors()))
		for i, desc := range result.Errors() {
			errors[i] = desc.String()
		}
		return &ValidationError{
			Type:   "args_invalid",
			Tool:   descriptor.Name,
			Detail: fmt.Sprintf("argument validation failed: %v", errors),
		}
	}

	return nil
}

// ValidateResult validates tool result against the output schema
func (sv *SchemaValidator) ValidateResult(descriptor *ToolDescriptor, result json.RawMessage) error {
	schema, err := sv.getSchema(string(descriptor.OutputSchema))
	if err != nil {
		return fmt.Errorf("invalid output schema for tool %s: %w", descriptor.Name, err)
	}

	resultLoader := gojsonschema.NewBytesLoader(result)
	validationResult, err := schema.Validate(resultLoader)
	if err != nil {
		return fmt.Errorf("validation error for tool %s: %w", descriptor.Name, err)
	}

	if !validationResult.Valid() {
		errors := make([]string, len(validationResult.Errors()))
		for i, desc := range validationResult.Errors() {
			errors[i] = desc.String()
		}
		return &ValidationError{
			Type:   "result_invalid",
			Tool:   descriptor.Name,
			Detail: fmt.Sprintf("result validation failed: %v", errors),
		}
	}

	return nil
}

// getSchema retrieves or compiles a JSON schema
func (sv *SchemaValidator) getSchema(schemaJSON string) (*gojsonschema.Schema, error) {
	if schema, exists := sv.cache[schemaJSON]; exists {
		return schema, nil
	}

	schemaLoader := gojsonschema.NewStringLoader(schemaJSON)
	schema, err := gojsonschema.NewSchema(schemaLoader)
	if err != nil {
		return nil, err
	}

	sv.cache[schemaJSON] = schema
	return schema, nil
}

// CoerceResult attempts to coerce simple type mismatches in tool results
func (sv *SchemaValidator) CoerceResult(descriptor *ToolDescriptor, result json.RawMessage) (json.RawMessage, []Coercion, error) {
	// First try validation without coercion
	if err := sv.ValidateResult(descriptor, result); err == nil {
		return result, nil, nil
	}

	// Parse the result to perform coercion
	var data interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		return nil, nil, fmt.Errorf("cannot parse result for coercion: %w", err)
	}

	coercions := []Coercion{}
	coerced := sv.coerceValue(data, &coercions, "")

	// Re-marshal the coerced data
	coercedBytes, err := json.Marshal(coerced)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot marshal coerced result: %w", err)
	}

	// Validate the coerced result
	if err := sv.ValidateResult(descriptor, coercedBytes); err != nil {
		return nil, nil, fmt.Errorf("coercion failed: %w", err)
	}

	return coercedBytes, coercions, nil
}

// Coercion represents a type coercion that was performed
type Coercion struct {
	Path string      `json:"path"`
	From interface{} `json:"from"`
	To   interface{} `json:"to"`
}

// coerceValue performs simple type coercions (e.g., number to string, string to number)
func (sv *SchemaValidator) coerceValue(value interface{}, coercions *[]Coercion, path string) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{})
		for k, val := range v {
			childPath := path
			if childPath != "" {
				childPath += "."
			}
			childPath += k
			result[k] = sv.coerceValue(val, coercions, childPath)
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, val := range v {
			childPath := fmt.Sprintf("%s[%d]", path, i)
			result[i] = sv.coerceValue(val, coercions, childPath)
		}
		return result
	case float64:
		// Could potentially coerce to string if needed
		return v
	case string:
		// Could potentially coerce to number if needed
		return v
	default:
		return v
	}
}
