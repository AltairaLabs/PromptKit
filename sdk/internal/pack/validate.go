// Package pack provides internal pack loading functionality.
package pack

import (
	"fmt"

	"github.com/xeipuuv/gojsonschema"

	"github.com/AltairaLabs/PromptKit/runtime/prompt/schema"
)

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
// It uses the $schema URL from the pack if present, otherwise uses the embedded schema.
// The PROMPTKIT_SCHEMA_SOURCE environment variable can override this behavior:
//   - "local": Always use embedded schema (default, for offline support)
//   - "remote": Always fetch from URL
//   - file path: Load schema from local file
//
// Returns nil if validation passes, or a SchemaValidationError with details.
func ValidateAgainstSchema(data []byte) error {
	// Extract $schema from the pack to support versioned schemas
	packSchemaURL := schema.ExtractSchemaURL(data)

	schemaLoader, err := schema.GetSchemaLoader(packSchemaURL)
	if err != nil {
		return fmt.Errorf("failed to load schema: %w", err)
	}

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
