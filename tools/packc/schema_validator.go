package main

import (
	"fmt"

	"github.com/xeipuuv/gojsonschema"

	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

// PackSchemaValidationResult contains the result of schema validation
type PackSchemaValidationResult struct {
	Valid  bool
	Errors []string
}

// ValidatePackAgainstSchema validates the compiled pack JSON against the PromptPack schema
func ValidatePackAgainstSchema(packJSON []byte) (*PackSchemaValidationResult, error) {
	schemaURL := prompt.PromptPackSchemaURL

	schemaLoader := gojsonschema.NewReferenceLoader(schemaURL)
	documentLoader := gojsonschema.NewBytesLoader(packJSON)

	result, err := gojsonschema.Validate(schemaLoader, documentLoader)
	if err != nil {
		return nil, fmt.Errorf("schema validation failed: %w", err)
	}

	validationResult := &PackSchemaValidationResult{
		Valid:  result.Valid(),
		Errors: make([]string, 0),
	}

	if !result.Valid() {
		for _, desc := range result.Errors() {
			validationResult.Errors = append(validationResult.Errors, desc.String())
		}
	}

	return validationResult, nil
}
