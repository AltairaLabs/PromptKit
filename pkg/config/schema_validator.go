package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/xeipuuv/gojsonschema"
	"gopkg.in/yaml.v3"
)

// SchemaBaseURL is the base URL for PromptKit JSON schemas
const SchemaBaseURL = "https://promptkit.altairalabs.ai/schemas/v1alpha1"

const errorFormat = "  - %s"

// SchemaFallbackEnabled controls whether to fall back to local schemas when remote fetch fails
var SchemaFallbackEnabled = true

// SchemaValidationEnabled controls whether schema validation is performed
// Can be disabled for testing or when schemas are not yet published
var SchemaValidationEnabled = true

// SchemaLocalPath is the path to local schema files (relative to repo root)
const SchemaLocalPath = "schemas/v1alpha1"

// localSchemaDirIfRequested returns a local schema directory when
// PROMPTKIT_SCHEMA_SOURCE=local is set. Returns empty string otherwise.
func localSchemaDirIfRequested() string {
	if os.Getenv("PROMPTKIT_SCHEMA_SOURCE") != "local" {
		return ""
	}
	// Use discovery to find an existing local schema file, then use its directory
	if p := findLocalSchemaPath(string(ConfigTypeArena)); p != "" {
		return filepath.Dir(p)
	}
	if abs, err := filepath.Abs(SchemaLocalPath); err == nil {
		return abs
	}
	return SchemaLocalPath
}

type schemaCacheStore struct {
	mu      sync.RWMutex
	schemas map[string]*gojsonschema.Schema
}

// schemaCache caches compiled JSON schemas to avoid repeated HTTP requests
var schemaCache = &schemaCacheStore{
	schemas: make(map[string]*gojsonschema.Schema),
}

// get retrieves a schema from the cache
func (c *schemaCacheStore) get(key string) *gojsonschema.Schema {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.schemas[key]
}

// set stores a schema in the cache
func (c *schemaCacheStore) set(key string, schema *gojsonschema.Schema) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.schemas[key] = schema
}

// findLocalSchemaPath searches for a local schema file in common locations
func findLocalSchemaPath(configType string) string {
	// Try to find schema relative to current working directory
	// This works when running tests or when the module is in the current directory
	possiblePaths := []string{
		filepath.Join(SchemaLocalPath, configType+".json"),
		filepath.Join("../../", SchemaLocalPath, configType+".json"),    // From pkg/config
		filepath.Join("../../../", SchemaLocalPath, configType+".json"), // From deeper nested packages
	}

	for _, path := range possiblePaths {
		info, err := os.Stat(path)
		if err == nil && info != nil {
			if absPath, err := filepath.Abs(path); err == nil { // NOSONAR: Fallback to relative path if conversion fails
				return absPath
			}
			return path // Fallback to relative path
		}
	}

	return ""
}

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
	// Skip validation if disabled (for testing or when schemas not yet published)
	if !SchemaValidationEnabled {
		return &SchemaValidationResult{Valid: true, Errors: []SchemaValidationError{}}, nil
	}

	jsonData, err := convertYAMLToJSON(yamlData)
	if err != nil {
		return nil, err
	}

	schemaKey := buildSchemaKey(configType, schemaDir)
	schema, err := loadOrGetCachedSchema(schemaKey, configType, schemaDir)
	if err != nil {
		return nil, err
	}

	return validateJSONWithSchema(jsonData, schema)
}

func convertYAMLToJSON(yamlData []byte) ([]byte, error) {
	var data interface{}
	if err := yaml.Unmarshal(yamlData, &data); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to JSON: %w", err)
	}
	return jsonData, nil
}

func buildSchemaKey(configType ConfigType, schemaDir string) string {
	// Prefer provided schemaDir, else environment-driven local directory
	if schemaDir == "" {
		if d := localSchemaDirIfRequested(); d != "" {
			schemaDir = d
		}
	}
	if schemaDir != "" {
		return fmt.Sprintf("file://%s/%s.json", schemaDir, configType)
	}
	return fmt.Sprintf("%s/%s.json", SchemaBaseURL, configType)
}

func loadOrGetCachedSchema(schemaKey string, configType ConfigType, schemaDir string) (*gojsonschema.Schema, error) {
	// Check cache first
	schema := schemaCache.get(schemaKey)
	if schema != nil {
		return schema, nil
	}

	// Load and cache schema
	schema, err := loadSchema(schemaKey, configType, schemaDir)
	if err != nil {
		return nil, err
	}

	schemaCache.set(schemaKey, schema)
	return schema, nil
}

func loadSchema(schemaKey string, configType ConfigType, schemaDir string) (*gojsonschema.Schema, error) {
	// If a local schema directory is requested via env, prefer that
	if schemaDir == "" {
		if d := localSchemaDirIfRequested(); d != "" {
			schemaDir = d
		}
	}

	schemaLoader := gojsonschema.NewReferenceLoader(schemaKey)
	compiledSchema, err := gojsonschema.NewSchema(schemaLoader)

	if err == nil {
		return compiledSchema, nil
	}

	// Try fallback to local schema if remote fetch failed
	if SchemaFallbackEnabled && schemaDir == "" {
		localSchema, fallbackErr := tryLocalSchemaFallback(configType)
		if fallbackErr == nil {
			return localSchema, nil
		}
		// If fallback also fails, return the original error (not the fallback error)
		// This prevents confusing error messages when running in environments without local schemas
	}

	return nil, fmt.Errorf("failed to load schema: %w", err)
}

func tryLocalSchemaFallback(configType ConfigType) (*gojsonschema.Schema, error) {
	localSchemaPath := findLocalSchemaPath(string(configType))
	if localSchemaPath == "" {
		return nil, fmt.Errorf("no local schema found for fallback")
	}

	localLoader := gojsonschema.NewReferenceLoader("file://" + localSchemaPath)
	return gojsonschema.NewSchema(localLoader)
}

func validateJSONWithSchema(jsonData []byte, schema *gojsonschema.Schema) (*SchemaValidationResult, error) {
	documentLoader := gojsonschema.NewBytesLoader(jsonData)

	result, err := schema.Validate(documentLoader)
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
