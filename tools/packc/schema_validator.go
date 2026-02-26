package main

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/prompt/schema"
)

// PackSchemaValidationResult contains the result of schema validation
type PackSchemaValidationResult struct {
	Valid  bool
	Errors []string
}

// ValidatePackAgainstSchema validates the compiled pack JSON against the PromptPack schema.
// It uses the $schema URL from the pack if present, otherwise uses the embedded schema.
// The PROMPTKIT_SCHEMA_SOURCE environment variable can override this behavior:
//   - "local": Always use embedded schema (default, for offline support)
//   - "remote": Always fetch from URL
//   - file path: Load schema from local file
func ValidatePackAgainstSchema(packJSON []byte) (*PackSchemaValidationResult, error) {
	// Extract $schema from the pack to support versioned schemas
	packSchemaURL := schema.ExtractSchemaURL(packJSON)

	schemaLoader, err := schema.GetSchemaLoader(packSchemaURL)
	if err != nil {
		return nil, fmt.Errorf("failed to load schema: %w", err)
	}

	result, err := schema.ValidateJSONAgainstLoader(packJSON, schemaLoader)
	if err != nil {
		return nil, err
	}

	validationResult := &PackSchemaValidationResult{
		Valid:  result.Valid,
		Errors: make([]string, 0),
	}

	if !result.Valid {
		for _, e := range result.Errors {
			validationResult.Errors = append(validationResult.Errors, e.Error())
		}
	}

	return validationResult, nil
}
