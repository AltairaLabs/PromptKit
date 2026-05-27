package config

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/xeipuuv/gojsonschema"
	"gopkg.in/yaml.v3"

	promptschema "github.com/AltairaLabs/PromptKit/runtime/prompt/schema"
)

// SchemaBaseURL is the base URL for PromptKit JSON schemas
const SchemaBaseURL = "https://promptkit.altairalabs.ai/schemas/v1alpha1"

const errorFormat = "  - %s"

// SchemaFallbackDisabled controls whether local schema fallback is suppressed.
// When true (non-default), the loader will not fall back to local schemas on remote fetch failure.
// Uses atomic.Bool for safe concurrent access across goroutines and test init functions.
var SchemaFallbackDisabled atomic.Bool

// SchemaValidationDisabled controls whether schema validation is skipped.
// When true (non-default), schema validation is bypassed (useful for testing or unpublished schemas).
// Uses atomic.Bool for safe concurrent access across goroutines and test init functions.
var SchemaValidationDisabled atomic.Bool

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

// maxSchemaCacheSize is the maximum number of schemas that can be cached.
// When this limit is reached, the least recently used entry is evicted.
const maxSchemaCacheSize = 64

type schemaCacheEntry struct {
	key    string
	schema *gojsonschema.Schema
}

type schemaCacheStore struct {
	mu      sync.Mutex
	schemas map[string]*schemaCacheEntry
	order   []string // LRU order: most recently used at the end
}

// schemaCache caches compiled JSON schemas to avoid repeated HTTP requests
var schemaCache = &schemaCacheStore{
	schemas: make(map[string]*schemaCacheEntry),
}

// get retrieves a schema from the cache and marks it as recently used.
func (c *schemaCacheStore) get(key string) *gojsonschema.Schema {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.schemas[key]
	if !ok {
		return nil
	}
	c.touchLocked(key)
	return entry.schema
}

// set stores a schema in the cache, evicting the LRU entry if at capacity.
func (c *schemaCacheStore) set(key string, schema *gojsonschema.Schema) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.schemas[key]; ok {
		c.schemas[key].schema = schema
		c.touchLocked(key)
		return
	}
	// Evict LRU if at capacity.
	if len(c.order) >= maxSchemaCacheSize {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.schemas, oldest)
	}
	c.schemas[key] = &schemaCacheEntry{key: key, schema: schema}
	c.order = append(c.order, key)
}

// touchLocked moves key to the end of the LRU order. Must be called with mu held.
func (c *schemaCacheStore) touchLocked(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
	c.order = append(c.order, key)
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
	ConfigTypeEval         ConfigType = "eval"
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

	// Keyword mirrors runtime/prompt/schema.ValidationError.Keyword
	// (e.g. "enum", "additional_property_not_allowed").
	Keyword string
	// ValidValues lists valid alternatives when computable — enum allowed
	// values, or sibling property names for additionalProperties.
	ValidValues []string
	// Suggestions are nearest-match candidates from ValidValues.
	Suggestions []string
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
	if SchemaValidationDisabled.Load() {
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

	return validateJSONWithSchema(jsonData, schema, schemaKey)
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

// TODO: Use golang.org/x/sync/singleflight to deduplicate concurrent schema loads
// for the same key. Currently, multiple goroutines requesting the same uncached
// schema will each perform an independent load (thundering herd).
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
	if !SchemaFallbackDisabled.Load() && schemaDir == "" {
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

func validateJSONWithSchema(
	jsonData []byte,
	compiledSchema *gojsonschema.Schema,
	schemaKey string,
) (*SchemaValidationResult, error) {
	// Use the compiled schema as a loader via its internal reference.
	// For pre-compiled schemas we still need to call Validate on the schema directly.
	documentLoader := gojsonschema.NewBytesLoader(jsonData)

	result, err := compiledSchema.Validate(documentLoader)
	if err != nil {
		return nil, fmt.Errorf("schema validation failed: %w", err)
	}

	return convertSharedResult(result, schemaKey), nil
}

// convertSharedResult converts a promptschema.ValidationResult to a config
// SchemaValidationResult and enriches additionalProperty/enum errors with
// valid alternatives from the raw schema at schemaKey.
func convertSharedResult(result *gojsonschema.Result, schemaKey string) *SchemaValidationResult {
	shared := promptschema.ConvertResult(result)
	out := &SchemaValidationResult{
		Valid:  shared.Valid,
		Errors: make([]SchemaValidationError, 0, len(shared.Errors)),
	}

	var rawSchema map[string]any
	loaded := false

	for _, e := range shared.Errors {
		converted := SchemaValidationError{
			Field:       e.Field,
			Description: e.Description,
			Value:       e.Value,
			Keyword:     e.Keyword,
			ValidValues: e.ValidValues,
			Suggestions: e.Suggestions,
		}
		switch e.Keyword {
		case keywordAdditionalProperty, keywordEnum:
			if !loaded {
				rawSchema = loadRawSchemaForKey(schemaKey)
				loaded = true
			}
			enrichFromSchema(&converted, rawSchema)
		}
		out.Errors = append(out.Errors, converted)
	}
	return out
}

// enrichFromSchema populates ValidValues and Suggestions on an error using
// the parsed raw schema. No-op if rawSchema is nil.
func enrichFromSchema(e *SchemaValidationError, rawSchema map[string]any) {
	if rawSchema == nil {
		return
	}
	switch e.Keyword {
	case keywordAdditionalProperty:
		enrichAdditionalProperty(e, rawSchema)
	case keywordEnum:
		enrichEnum(e, rawSchema)
	}
}

func enrichAdditionalProperty(e *SchemaValidationError, rawSchema map[string]any) {
	const prefix = "Additional property "
	const suffix = " is not allowed"
	offending := ""
	if strings.HasPrefix(e.Description, prefix) && strings.HasSuffix(e.Description, suffix) {
		offending = strings.TrimSuffix(strings.TrimPrefix(e.Description, prefix), suffix)
	}
	valid := promptschema.LookupProperties(rawSchema, e.Field)
	if len(valid) == 0 {
		return
	}
	if offending != "" {
		e.Suggestions = promptschema.NearestMatches(offending, valid)
	}
	e.ValidValues = truncateValidValues(valid, maxValidValuesShown)
}

func enrichEnum(e *SchemaValidationError, rawSchema map[string]any) {
	allowed := promptschema.LookupEnumValues(rawSchema, e.Field)
	if len(allowed) == 0 {
		return
	}
	e.ValidValues = allowed
	if s, ok := e.Value.(string); ok {
		e.Suggestions = promptschema.NearestMatches(s, allowed)
	}
}

const (
	maxValidValuesShown       = 8
	keywordAdditionalProperty = "additional_property_not_allowed"
	keywordEnum               = "enum"
)

// truncateValidValues caps the displayed valid set and appends a "+N more"
// sentinel. Sorts lexicographically for determinism.
func truncateValidValues(values []string, limit int) []string {
	sorted := make([]string, len(values))
	copy(sorted, values)
	sort.Strings(sorted)
	if len(sorted) <= limit {
		return sorted
	}
	out := make([]string, 0, limit+1)
	out = append(out, sorted[:limit]...)
	out = append(out, fmt.Sprintf("+%d more", len(sorted)-limit))
	return out
}

// rawSchemaCacheStore holds parsed-JSON schema documents keyed by schemaKey.
// Separate from schemaCache (which holds compiled gojsonschema schemas)
// because the raw doc is only needed for error enrichment on the invalid
// path, not on every Validate call.
type rawSchemaCacheStore struct {
	mu   sync.Mutex
	docs map[string]map[string]any
}

func (c *rawSchemaCacheStore) get(key string) map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.docs[key]
}

func (c *rawSchemaCacheStore) set(key string, doc map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.docs[key] = doc
}

var rawSchemaCache = &rawSchemaCacheStore{docs: make(map[string]map[string]any)}

// loadRawSchemaForKey fetches and parses the raw schema document for the
// given schemaKey (file:// or https:// URL). Returns nil on any error so
// callers degrade gracefully.
func loadRawSchemaForKey(schemaKey string) map[string]any {
	if cached := rawSchemaCache.get(schemaKey); cached != nil {
		return cached
	}
	bytes, err := fetchSchemaBytes(schemaKey)
	if err != nil {
		return nil
	}
	var doc map[string]any
	if err := json.Unmarshal(bytes, &doc); err != nil {
		return nil
	}
	rawSchemaCache.set(schemaKey, doc)
	return doc
}

func fetchSchemaBytes(schemaKey string) ([]byte, error) {
	switch {
	case strings.HasPrefix(schemaKey, "file://"):
		path := strings.TrimPrefix(schemaKey, "file://")
		return os.ReadFile(path) //nolint:gosec // path comes from configured schemaDir
	case strings.HasPrefix(schemaKey, "http://"), strings.HasPrefix(schemaKey, "https://"):
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, schemaKey, http.NoBody)
		if err != nil {
			return nil, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("status %d", resp.StatusCode)
		}
		return io.ReadAll(resp.Body)
	default:
		return nil, fmt.Errorf("unsupported schema source: %s", schemaKey)
	}
}

// ValidateConfig validates YAML data against the JSON schema for the given ConfigType.
// It returns a formatted error listing all schema violations, or nil if the data is valid.
func ValidateConfig(configType ConfigType, yamlData []byte) error {
	result, err := ValidateWithSchema(yamlData, configType)
	if err != nil {
		return err
	}

	if !result.Valid {
		var errorMessages []string
		for _, e := range result.Errors {
			errorMessages = append(errorMessages, fmt.Sprintf(errorFormat, e.Error()))
		}
		return fmt.Errorf("%s configuration does not match schema:\n%s",
			string(configType), strings.Join(errorMessages, "\n"))
	}

	return nil
}

// ValidateArenaConfig validates an Arena configuration against its schema.
func ValidateArenaConfig(yamlData []byte) error { return ValidateConfig(ConfigTypeArena, yamlData) }

// ValidateScenario validates a Scenario configuration against its schema.
func ValidateScenario(yamlData []byte) error { return ValidateConfig(ConfigTypeScenario, yamlData) }

// ValidateEval validates an Eval configuration against its schema.
func ValidateEval(yamlData []byte) error { return ValidateConfig(ConfigTypeEval, yamlData) }

// ValidateProvider validates a Provider configuration against its schema.
func ValidateProvider(yamlData []byte) error { return ValidateConfig(ConfigTypeProvider, yamlData) }

// ValidatePromptConfig validates a PromptConfig configuration against its schema.
func ValidatePromptConfig(yamlData []byte) error {
	return ValidateConfig(ConfigTypePromptConfig, yamlData)
}

// ValidateTool validates a Tool configuration against its schema.
func ValidateTool(yamlData []byte) error { return ValidateConfig(ConfigTypeTool, yamlData) }

// ValidatePersona validates a Persona configuration against its schema.
func ValidatePersona(yamlData []byte) error { return ValidateConfig(ConfigTypePersona, yamlData) }

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
		case kindEval:
			return ConfigTypeEval, nil
		}
	}

	return "", fmt.Errorf("unable to detect configuration type: missing or unknown 'kind' field")
}
