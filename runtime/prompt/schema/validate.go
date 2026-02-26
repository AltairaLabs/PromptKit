// Package schema provides embedded PromptPack schema and shared schema validation utilities.
package schema

import (
	"fmt"

	"github.com/xeipuuv/gojsonschema"
)

// ValidationError represents a single schema validation error with field-level detail.
type ValidationError struct {
	Field       string
	Description string
	Value       interface{}
}

// Error implements the error interface.
func (e ValidationError) Error() string {
	if e.Value != nil {
		return fmt.Sprintf("%s: %s (value: %v)", e.Field, e.Description, e.Value)
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Description)
}

// ValidationResult contains the results of JSON schema validation.
type ValidationResult struct {
	Valid  bool
	Errors []ValidationError
}

// ValidateJSONAgainstLoader validates raw JSON bytes against a schema provided as a gojsonschema.JSONLoader.
// This is the shared, low-level validation entry point used by Arena config validation (pkg/config),
// PromptPack validation (sdk/internal/pack), and the pack compiler (tools/packc).
func ValidateJSONAgainstLoader(jsonData []byte, schemaLoader gojsonschema.JSONLoader) (*ValidationResult, error) {
	documentLoader := gojsonschema.NewBytesLoader(jsonData)

	result, err := gojsonschema.Validate(schemaLoader, documentLoader)
	if err != nil {
		return nil, fmt.Errorf("schema validation failed: %w", err)
	}

	return ConvertResult(result), nil
}

// ConvertResult converts a gojsonschema result into a ValidationResult.
func ConvertResult(result *gojsonschema.Result) *ValidationResult {
	vr := &ValidationResult{
		Valid:  result.Valid(),
		Errors: make([]ValidationError, 0),
	}

	if !result.Valid() {
		for _, e := range result.Errors() {
			vr.Errors = append(vr.Errors, ValidationError{
				Field:       e.Field(),
				Description: e.Description(),
				Value:       e.Value(),
			})
		}
	}

	return vr
}
