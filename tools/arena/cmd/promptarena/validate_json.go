package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

// validateJSONError is the machine-readable form of one schema error.
type validateJSONError struct {
	Field       string   `json:"field"`
	Description string   `json:"description"`
	Keyword     string   `json:"keyword,omitempty"`
	Suggestions []string `json:"suggestions,omitempty"`
}

// validateJSONReport is the structured result emitted by `validate --json`.
type validateJSONReport struct {
	File   string              `json:"file"`
	Type   string              `json:"type"`
	Valid  bool                `json:"valid"`
	Errors []validateJSONError `json:"errors"`
}

func buildValidateReport(file, configType string, result *config.SchemaValidationResult) validateJSONReport {
	report := validateJSONReport{
		File:   file,
		Type:   configType,
		Valid:  result.Valid,
		Errors: []validateJSONError{},
	}
	for _, e := range result.Errors {
		report.Errors = append(report.Errors, validateJSONError{
			Field:       e.Field,
			Description: e.Description,
			Keyword:     e.Keyword,
			Suggestions: e.Suggestions,
		})
	}
	return report
}

// writeValidateJSON runs schema validation and writes a JSON report. It returns a
// non-nil error when validation fails so the process exits non-zero, while still
// emitting the JSON report to w first.
func writeValidateJSON(w io.Writer, filePath, typeOption string) error {
	data, configType, err := prepareValidationWithType(filePath, typeOption)
	if err != nil {
		return err
	}

	result, err := validateWithSchema(data, config.ConfigType(configType))
	if err != nil {
		return err
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(buildValidateReport(filePath, configType, result)); err != nil {
		return err
	}

	if !result.Valid {
		return fmt.Errorf("schema validation failed with %d error(s)", len(result.Errors))
	}
	return nil
}
