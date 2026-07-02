package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

const (
	// kindEval is the K8s-style kind value for Eval configurations
	kindEval = "Eval"

	// errSchemaValidationFailed is the shared error-wrap format used by the
	// schema-validation failure path in LoadSimpleK8sManifest.
	errSchemaValidationFailed = "schema validation failed for %s: %w"
)

// K8sManifest is an interface for K8s-style manifest types.
type K8sManifest interface {
	GetAPIVersion() string
	GetKind() string
	GetName() string
	SetID(id string)
}

// LoadSimpleK8sManifest is a generic loader for K8s-style manifest files.
// This is used for simple config types (Scenario, Provider, Eval, Tool,
// Persona) that don't support legacy formats.
func LoadSimpleK8sManifest[T K8sManifest](filename, expectedKind string) (T, error) {
	var zero T

	data, err := os.ReadFile(filename)
	if err != nil {
		return zero, fmt.Errorf("failed to read %s file: %w", expectedKind, err)
	}

	// Schema validation based on kind
	var validationErr error
	switch expectedKind {
	case "Scenario":
		validationErr = ValidateScenario(data)
	case kindEval:
		validationErr = ValidateEval(data)
	case "Provider":
		validationErr = ValidateProvider(data)
	case "Tool":
		validationErr = ValidateTool(data)
	case "Persona":
		validationErr = ValidatePersona(data)
	}
	if validationErr != nil {
		return zero, fmt.Errorf(errSchemaValidationFailed, expectedKind, validationErr)
	}

	var config T
	if err := yaml.Unmarshal(data, &config); err != nil {
		return zero, fmt.Errorf("failed to parse %s file: %w", expectedKind, err)
	}

	// Schema validation already confirmed required fields and kind value are correct

	// Use metadata.name as the ID
	config.SetID(config.GetName())
	return config, nil
}

// LoadProvider loads and parses a provider configuration from a YAML file in K8s-style manifest format
func LoadProvider(filename string) (*Provider, error) {
	// Use K8s version for unmarshaling
	config, err := LoadSimpleK8sManifest[*ProviderConfigK8s](filename, "Provider")
	if err != nil {
		return nil, err
	}
	return &config.Spec, nil
}
