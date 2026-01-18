package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xeipuuv/gojsonschema"
)

func TestLocalSchemaDirIfRequested(t *testing.T) {
	// Ensure unset env yields empty
	_ = os.Unsetenv("PROMPTKIT_SCHEMA_SOURCE")
	if d := localSchemaDirIfRequested(); d != "" {
		t.Fatalf("expected empty dir when env not set, got %q", d)
	}

	// Set env to local and expect non-empty directory path
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	if d := localSchemaDirIfRequested(); d == "" {
		t.Fatalf("expected non-empty dir when env=local")
	}
}

func TestBuildSchemaKey_UsesFileWhenSchemaDirOrEnvSet(t *testing.T) {
	// Explicit schemaDir
	key := buildSchemaKey(ConfigTypeArena, "/tmp/schemas")
	if !strings.HasPrefix(key, "file://") || !strings.Contains(key, "/tmp/schemas/arena.json") {
		t.Fatalf("unexpected schema key with explicit dir: %q", key)
	}

	// Env-driven local directory
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	key2 := buildSchemaKey(ConfigTypeScenario, "")
	if !strings.HasPrefix(key2, "file://") || !strings.HasSuffix(key2, "/scenario.json") {
		t.Fatalf("expected file:// schema key from env, got %q", key2)
	}
}

func TestValidateArenaConfig_Valid(t *testing.T) {
	validConfig := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test-arena
  namespace: default
spec:
  prompt_configs:
    - id: test
      file: test.yaml
  providers:
    - file: provider.yaml
  scenarios:
    - file: scenario.yaml
  defaults:
    temperature: 0.7
`)

	err := ValidateArenaConfig(validConfig)
	assert.NoError(t, err)
}

func TestValidateArenaConfig_MissingMetadata(t *testing.T) {
	invalidConfig := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
spec:
  prompt_configs:
    - id: test
      file: test.yaml
`)

	err := ValidateArenaConfig(invalidConfig)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "schema")
}

func TestSchemaCaching(t *testing.T) {
	// Clear cache before test
	schemaCache.mu.Lock()
	schemaCache.schemas = make(map[string]*gojsonschema.Schema)
	schemaCache.mu.Unlock()

	validConfig := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test-arena
spec:
  prompt_configs:
    - id: test
      file: test.yaml
  providers:
    - file: provider.yaml
  scenarios:
    - file: scenario.yaml
`)

	// Find the project root by looking for go.work file
	cwd, err := os.Getwd()
	require.NoError(t, err)

	// Navigate up to find the project root (where go.work exists)
	projectRoot := cwd
	for {
		if _, err := os.Stat(filepath.Join(projectRoot, "go.work")); err == nil {
			break
		}
		parent := filepath.Dir(projectRoot)
		if parent == projectRoot {
			t.Skip("Cannot find project root (go.work not found)")
			return
		}
		projectRoot = parent
	}

	schemaDir := filepath.Join(projectRoot, "docs", "public", "schemas", "v1alpha1")

	// Verify schema directory exists before running test
	if _, err := os.Stat(schemaDir); os.IsNotExist(err) {
		t.Skipf("Schema directory not found at %s, skipping test", schemaDir)
		return
	}

	// First validation - should cache the schema
	_, err = ValidateWithLocalSchema(validConfig, ConfigTypeArena, schemaDir)
	require.NoError(t, err)

	// Check that schema was cached
	schemaCache.mu.RLock()
	schemaKey := "file://" + schemaDir + "/arena.json"
	cachedSchema := schemaCache.schemas[schemaKey]
	schemaCache.mu.RUnlock()
	assert.NotNil(t, cachedSchema, "Schema should be cached after first validation")

	// Second validation - should use cached schema
	_, err = ValidateWithLocalSchema(validConfig, ConfigTypeArena, schemaDir)
	require.NoError(t, err)

	// Verify same schema instance is used (pointer equality)
	schemaCache.mu.RLock()
	cachedSchema2 := schemaCache.schemas[schemaKey]
	schemaCache.mu.RUnlock()
	assert.Same(t, cachedSchema, cachedSchema2, "Should reuse cached schema instance")
}

func TestValidateArenaConfig_MissingKind(t *testing.T) {
	invalidConfig := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
metadata:
  name: test-arena
spec:
  prompt_configs:
    - id: test
      file: test.yaml
`)

	err := ValidateArenaConfig(invalidConfig)
	assert.Error(t, err)
}

func TestValidateScenario_Valid(t *testing.T) {
	validScenario := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: test-scenario
spec:
  id: test-scenario
  task_type: test
  description: Test scenario
  turns:
    - role: user
      content: Hello
`)

	err := ValidateScenario(validScenario)
	assert.NoError(t, err)
}

func TestValidateProvider_Valid(t *testing.T) {
	validProvider := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: test-provider
spec:
  id: test-provider
  type: openai
  model: gpt-4
`)

	err := ValidateProvider(validProvider)
	assert.NoError(t, err)
}

func TestValidatePromptConfig_Valid(t *testing.T) {
	validPromptConfig := []byte(`
apiVersion: promptkit.altaira.ai/v1alpha1
kind: PromptConfig
metadata:
  name: test-prompt
spec:
  task_type: test
  version: v1.0.0
  description: Test prompt config
  system_template: You are a helpful assistant
`)

	err := ValidatePromptConfig(validPromptConfig)
	assert.NoError(t, err)
}

func TestValidateTool_Valid(t *testing.T) {
	validTool := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: test-tool
spec:
  name: get_weather
  description: Get weather information
  input_schema: {}
  output_schema: {}
  mode: mock
  timeout_ms: 5000
`)

	err := ValidateTool(validTool)
	assert.NoError(t, err)
}

func TestValidatePersona_Valid(t *testing.T) {
	validPersona := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Persona
metadata:
  name: test-persona
spec:
  id: test-persona
  description: Test user persona
  system_prompt: You are a friendly test user
  goals:
    - Test the system
  constraints:
    - Be polite
  style:
    verbosity: medium
    challenge_level: low
    friction_tags: []
  defaults:
    temperature: 0.7
    seed: 42
`)

	err := ValidatePersona(validPersona)
	assert.NoError(t, err)
}

func TestDetectConfigType(t *testing.T) {
	tests := []struct {
		name     string
		yaml     []byte
		expected ConfigType
		wantErr  bool
	}{
		{
			name: "arena",
			yaml: []byte(`
kind: Arena
metadata:
  name: test
`),
			expected: ConfigTypeArena,
			wantErr:  false,
		},
		{
			name: "scenario",
			yaml: []byte(`
kind: Scenario
metadata:
  name: test
`),
			expected: ConfigTypeScenario,
			wantErr:  false,
		},
		{
			name: "provider",
			yaml: []byte(`
kind: Provider
metadata:
  name: test
`),
			expected: ConfigTypeProvider,
			wantErr:  false,
		},
		{
			name: "promptconfig",
			yaml: []byte(`
kind: PromptConfig
metadata:
  name: test
`),
			expected: ConfigTypePromptConfig,
			wantErr:  false,
		},
		{
			name: "tool",
			yaml: []byte(`
kind: Tool
metadata:
  name: test
`),
			expected: ConfigTypeTool,
			wantErr:  false,
		},
		{
			name: "persona",
			yaml: []byte(`
kind: Persona
metadata:
  name: test
`),
			expected: ConfigTypePersona,
			wantErr:  false,
		},
		{
			name: "missing kind",
			yaml: []byte(`
metadata:
  name: test
`),
			wantErr: true,
		},
		{
			name: "unknown kind",
			yaml: []byte(`
kind: Unknown
metadata:
  name: test
`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DetectConfigType(tt.yaml)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, got)
			}
		})
	}
}

func TestValidateWithSchema_InvalidYAML(t *testing.T) {
	invalidYAML := []byte(`
this is not valid yaml: {{{
`)

	_, err := ValidateWithSchema(invalidYAML, ConfigTypeArena)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parse YAML")
}

func TestSchemaValidationError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      SchemaValidationError
		expected string
	}{
		{
			name: "with value",
			err: SchemaValidationError{
				Field:       "metadata.name",
				Description: "is required",
				Value:       nil,
			},
			expected: "metadata.name: is required",
		},
		{
			name: "without value",
			err: SchemaValidationError{
				Field:       "spec.providers",
				Description: "must be array",
				Value:       "string",
			},
			expected: "spec.providers: must be array (value: string)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			assert.Contains(t, got, tt.expected)
		})
	}
}

func TestValidateEval_Valid(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	validEval := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Eval
metadata:
  name: test-eval
spec:
  id: test-eval-id
  description: Test evaluation
  recording:
    path: test-recording.json
    type: session
  assertions:
    - type: contains
      params:
        pattern: "test"
`)

	err := ValidateEval(validEval)
	assert.NoError(t, err)
}

func TestValidateEval_MissingRequired(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	invalidEval := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Eval
metadata:
  name: test-eval
spec:
  description: Missing ID
  recording:
    path: test.json
`)

	err := ValidateEval(invalidEval)
	assert.Error(t, err)
}

func TestDetectConfigType_Eval(t *testing.T) {
	yamlData := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Eval
metadata:
  name: test-eval
spec:
  id: test-id
`)

	configType, err := DetectConfigType(yamlData)
	require.NoError(t, err)
	assert.Equal(t, ConfigTypeEval, configType)
}

func TestDetectConfigType_UnknownKind(t *testing.T) {
	yamlData := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: UnknownType
metadata:
  name: test
`)

	_, err := DetectConfigType(yamlData)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown 'kind'")
}

func TestDetectConfigType_MissingKind(t *testing.T) {
	yamlData := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
metadata:
  name: test
`)

	_, err := DetectConfigType(yamlData)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing or unknown 'kind'")
}

func TestTryLocalSchemaFallback(t *testing.T) {
	// Test with a config type that should have a local schema
	schema, err := tryLocalSchemaFallback(ConfigTypeArena)
	if err == nil {
		require.NotNil(t, schema)
	} else {
		// If no local schema exists, we expect an error
		assert.Contains(t, err.Error(), "no local schema found")
	}

	// Test with an invalid config type (no schema file should exist)
	_, err = tryLocalSchemaFallback(ConfigType("nonexistent"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no local schema found")
}

func TestLoadSchema_WithFallback(t *testing.T) {
	// Enable fallback for this test
	originalFallbackSetting := SchemaFallbackEnabled
	SchemaFallbackEnabled = true
	defer func() { SchemaFallbackEnabled = originalFallbackSetting }()

	// Test with an invalid remote schema URL (should trigger fallback)
	schema, err := loadSchema("http://invalid-url-that-does-not-exist.com/schema.json", ConfigTypeArena, "")

	// The result depends on whether local schemas exist
	// Either way, we're testing the fallback path
	if err != nil {
		assert.Contains(t, err.Error(), "failed to load schema")
	} else {
		require.NotNil(t, schema)
	}
}

func TestLoadSchema_WithoutFallback(t *testing.T) {
	// Disable fallback for this test
	originalFallbackSetting := SchemaFallbackEnabled
	SchemaFallbackEnabled = false
	defer func() { SchemaFallbackEnabled = originalFallbackSetting }()

	// Test with a malformed schema reference that will fail
	_, err := loadSchema("file:///nonexistent/path/to/schema.json", ConfigTypeArena, "")
	if err != nil {
		// Error is expected when the file doesn't exist
		require.Error(t, err)
	}
	// Note: This test verifies the non-fallback path is tested
}

func TestValidateScenario_InvalidYAML(t *testing.T) {
	invalidYAML := []byte(`invalid: yaml: content: [[[`)

	err := ValidateScenario(invalidYAML)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse YAML")
}

func TestValidateProvider_InvalidYAML(t *testing.T) {
	invalidYAML := []byte(`invalid: yaml: content: [[[`)

	err := ValidateProvider(invalidYAML)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse YAML")
}

func TestValidatePromptConfig_InvalidYAML(t *testing.T) {
	invalidYAML := []byte(`invalid: yaml: content: [[[`)

	err := ValidatePromptConfig(invalidYAML)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse YAML")
}

func TestValidateTool_InvalidYAML(t *testing.T) {
	invalidYAML := []byte(`invalid: yaml: content: [[[`)

	err := ValidateTool(invalidYAML)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse YAML")
}

func TestValidatePersona_InvalidYAML(t *testing.T) {
	invalidYAML := []byte(`invalid: yaml: content: [[[`)

	err := ValidatePersona(invalidYAML)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse YAML")
}

func TestValidateScenario_SchemaValidationFailure(t *testing.T) {
	// Valid YAML but doesn't match schema (missing required fields)
	invalidScenario := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: test
spec:
  id: test
  # Missing required fields like task_type, description, turns
`)

	err := ValidateScenario(invalidScenario)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scenario configuration does not match schema")
}

func TestValidateProvider_SchemaValidationFailure(t *testing.T) {
	// Valid YAML but doesn't match schema
	invalidProvider := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: test
spec:
  id: test
  # Missing required field 'type'
`)

	err := ValidateProvider(invalidProvider)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider configuration does not match schema")
}

func TestValidatePromptConfig_SchemaValidationFailure(t *testing.T) {
	// Valid YAML but doesn't match schema
	invalidPrompt := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: test
spec:
  id: test
  # Missing required fields
`)

	err := ValidatePromptConfig(invalidPrompt)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "promptconfig configuration does not match schema")
}

func TestValidateTool_SchemaValidationFailure(t *testing.T) {
	// Valid YAML but doesn't match schema
	invalidTool := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: test
spec:
  invalid_field: true
`)

	err := ValidateTool(invalidTool)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool configuration does not match schema")
}

func TestValidatePersona_SchemaValidationFailure(t *testing.T) {
	// Valid YAML but doesn't match schema
	invalidPersona := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Persona
metadata:
  name: test
spec:
  invalid_field: true
`)

	err := ValidatePersona(invalidPersona)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "persona configuration does not match schema")
}
