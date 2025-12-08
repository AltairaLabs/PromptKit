// Package pack provides internal pack loading functionality.
package pack

import (
	"fmt"

	"github.com/xeipuuv/gojsonschema"
)

// PromptPackSchemaURL is the JSON Schema URL for validating PromptPack files.
const PromptPackSchemaURL = "https://promptpack.org/schema/latest/promptpack.schema.json"

// SchemaValidationError represents a schema validation error with details.
type SchemaValidationError struct {
	Errors []string
}

func (e *SchemaValidationError) Error() string {
	if len(e.Errors) == 1 {
		return fmt.Sprintf("pack schema validation failed: %s", e.Errors[0])
	}
	return fmt.Sprintf("pack schema validation failed with %d errors", len(e.Errors))
}

// ValidateAgainstSchema validates pack JSON data against the PromptPack schema.
// Returns nil if validation passes, or a SchemaValidationError with details.
func ValidateAgainstSchema(data []byte) error {
	schemaLoader := gojsonschema.NewReferenceLoader(PromptPackSchemaURL)
	documentLoader := gojsonschema.NewBytesLoader(data)

	result, err := gojsonschema.Validate(schemaLoader, documentLoader)
	if err != nil {
		return fmt.Errorf("schema validation error: %w", err)
	}

	if !result.Valid() {
		errors := make([]string, 0, len(result.Errors()))
		for _, desc := range result.Errors() {
			errors = append(errors, fmt.Sprintf("%s: %s", desc.Field(), desc.Description()))
		}
		return &SchemaValidationError{Errors: errors}
	}

	return nil
}
