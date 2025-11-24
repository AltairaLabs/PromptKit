package config

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xeipuuv/gojsonschema"
	"gopkg.in/yaml.v3"
)

// SchemaBaseURL is the base URL for PromptKit JSON schemas
const SchemaBaseURL = "https://promptkit.altairalabs.ai/schemas/v1alpha1"

const errorFormat = "  - %s"

// ConfigType represents the type of configuration file
type ConfigType string

const (
	ConfigTypeArena        ConfigType = "arena"
	ConfigTypeScenario     ConfigType = "scenario"
	ConfigTypeProvider     ConfigType = "provider"
	ConfigTypePromptConfig ConfigType = "promptconfig"
	ConfigTypeTool         ConfigType = "tool"
	ConfigTypePersona      ConfigType = "persona"
)

// SchemaValidationError represents a validation error from JSON schema validation
type SchemaValidationError struct {
	Field       string
	Description string
	Value       interface{}
}

// Error implements the error interface
func (e SchemaValidationError) Error() string {
	if e.Value != nil {
		return fmt.Sprintf("%s: %s (value: %v)", e.Field, e.Description, e.Value)
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Description)
}

// SchemaValidationResult contains the results of schema validation
type SchemaValidationResult struct {
	Valid  bool
	Errors []SchemaValidationError
}

// ValidateWithSchema validates YAML data against a JSON schema
func ValidateWithSchema(yamlData []byte, configType ConfigType) (*SchemaValidationResult, error) {
	return validateWithSchemaSource(yamlData, configType, "")
}

// ValidateWithLocalSchema validates YAML data against a local JSON schema file
func ValidateWithLocalSchema(yamlData []byte, configType ConfigType, schemaDir string) (*SchemaValidationResult, error) {
	return validateWithSchemaSource(yamlData, configType, schemaDir)
}

func validateWithSchemaSource(yamlData []byte, configType ConfigType, schemaDir string) (*SchemaValidationResult, error) {
	// Convert YAML to JSON for schema validation
	var data interface{}
	if err := yaml.Unmarshal(yamlData, &data); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to JSON: %w", err)
	}

	// Determine schema loader (local file or remote URL)
	var schemaLoader gojsonschema.JSONLoader
	if schemaDir != "" {
		// Use local schema file
		schemaPath := fmt.Sprintf("file://%s/%s.json", schemaDir, configType)
		schemaLoader = gojsonschema.NewReferenceLoader(schemaPath)
	} else {
		// Use remote schema URL
		schemaURL := fmt.Sprintf("%s/%s.json", SchemaBaseURL, configType)
		schemaLoader = gojsonschema.NewReferenceLoader(schemaURL)
	}

	documentLoader := gojsonschema.NewBytesLoader(jsonData)

	result, err := gojsonschema.Validate(schemaLoader, documentLoader)
	if err != nil {
		return nil, fmt.Errorf("schema validation failed: %w", err)
	}

	// Convert results
	validationResult := &SchemaValidationResult{
		Valid:  result.Valid(),
		Errors: make([]SchemaValidationError, 0),
	}

	if !result.Valid() {
		for _, err := range result.Errors() {
			validationResult.Errors = append(validationResult.Errors, SchemaValidationError{
				Field:       err.Field(),
				Description: err.Description(),
				Value:       err.Value(),
			})
		}
	}

	return validationResult, nil
}

// ValidateArenaConfig validates an Arena configuration against its schema
func ValidateArenaConfig(yamlData []byte) error {
	result, err := ValidateWithSchema(yamlData, ConfigTypeArena)
	if err != nil {
		return err
	}

	if !result.Valid {
		var errorMessages []string
		for _, e := range result.Errors {
			errorMessages = append(errorMessages, fmt.Sprintf(errorFormat, e.Error()))
		}
		return fmt.Errorf("arena configuration does not match schema:\n%s", strings.Join(errorMessages, "\n"))
	}

	return nil
}

// ValidateScenario validates a Scenario configuration against its schema
func ValidateScenario(yamlData []byte) error {
	result, err := ValidateWithSchema(yamlData, ConfigTypeScenario)
	if err != nil {
		return err
	}

	if !result.Valid {
		var errorMessages []string
		for _, e := range result.Errors {
			errorMessages = append(errorMessages, fmt.Sprintf(errorFormat, e.Error()))
		}
		return fmt.Errorf("scenario configuration does not match schema:\n%s", strings.Join(errorMessages, "\n"))
	}

	return nil
}

// ValidateProvider validates a Provider configuration against its schema
func ValidateProvider(yamlData []byte) error {
	result, err := ValidateWithSchema(yamlData, ConfigTypeProvider)
	if err != nil {
		return err
	}

	if !result.Valid {
		var errorMessages []string
		for _, e := range result.Errors {
			errorMessages = append(errorMessages, fmt.Sprintf(errorFormat, e.Error()))
		}
		return fmt.Errorf("provider configuration does not match schema:\n%s", strings.Join(errorMessages, "\n"))
	}

	return nil
}

// ValidatePromptConfig validates a PromptConfig configuration against its schema
func ValidatePromptConfig(yamlData []byte) error {
	result, err := ValidateWithSchema(yamlData, ConfigTypePromptConfig)
	if err != nil {
		return err
	}

	if !result.Valid {
		var errorMessages []string
		for _, e := range result.Errors {
			errorMessages = append(errorMessages, fmt.Sprintf(errorFormat, e.Error()))
		}
		return fmt.Errorf("promptconfig configuration does not match schema:\n%s", strings.Join(errorMessages, "\n"))
	}

	return nil
}

// ValidateTool validates a Tool configuration against its schema
func ValidateTool(yamlData []byte) error {
	result, err := ValidateWithSchema(yamlData, ConfigTypeTool)
	if err != nil {
		return err
	}

	if !result.Valid {
		var errorMessages []string
		for _, e := range result.Errors {
			errorMessages = append(errorMessages, fmt.Sprintf(errorFormat, e.Error()))
		}
		return fmt.Errorf("tool configuration does not match schema:\n%s", strings.Join(errorMessages, "\n"))
	}

	return nil
}

// ValidatePersona validates a Persona configuration against its schema
func ValidatePersona(yamlData []byte) error {
	result, err := ValidateWithSchema(yamlData, ConfigTypePersona)
	if err != nil {
		return err
	}

	if !result.Valid {
		var errorMessages []string
		for _, e := range result.Errors {
			errorMessages = append(errorMessages, fmt.Sprintf(errorFormat, e.Error()))
		}
		return fmt.Errorf("persona configuration does not match schema:\n%s", strings.Join(errorMessages, "\n"))
	}

	return nil
}

// DetectConfigType attempts to detect the configuration type from YAML data
func DetectConfigType(yamlData []byte) (ConfigType, error) {
	var data map[string]interface{}
	if err := yaml.Unmarshal(yamlData, &data); err != nil {
		return "", fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Check for K8s-style manifest with kind field
	if kind, ok := data["kind"].(string); ok {
		switch kind {
		case "Arena":
			return ConfigTypeArena, nil
		case "Scenario":
			return ConfigTypeScenario, nil
		case "Provider":
			return ConfigTypeProvider, nil
		case "PromptConfig":
			return ConfigTypePromptConfig, nil
		case "Tool":
			return ConfigTypeTool, nil
		case "Persona":
			return ConfigTypePersona, nil
		}
	}

	return "", fmt.Errorf("unable to detect configuration type: missing or unknown 'kind' field")
}
