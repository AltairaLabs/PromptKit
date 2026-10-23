package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

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

// testManifestSchema stands in for a tool's schema: a manifest whose spec only
// accepts an id.
const testManifestSchema = `{"type":"object","required":["apiVersion","kind","spec"],
"properties":{"spec":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"}}}}}`

// Each Validate* wrapper validates against the schema its tool registered for
// that type, and names the type when the file does not match.
func TestValidateWrappers_UseTheRegisteredSchemaForTheirType(t *testing.T) {
	fsys := fstest.MapFS{}
	for _, ct := range []ConfigType{ConfigTypeArena, ConfigTypeScenario, ConfigTypeEval, ConfigTypeProvider,
		ConfigTypePromptConfig, ConfigTypeTool, ConfigTypePersona} {
		fsys[string(ct)+".json"] = &fstest.MapFile{Data: []byte(testManifestSchema)}
	}
	useSchemaFSForTest(t, fsys)

	wrappers := map[ConfigType]func([]byte) error{
		ConfigTypeArena: ValidateArenaConfig, ConfigTypeScenario: ValidateScenario, ConfigTypeEval: ValidateEval,
		ConfigTypeProvider: ValidateProvider, ConfigTypePromptConfig: ValidatePromptConfig,
		ConfigTypeTool: ValidateTool, ConfigTypePersona: ValidatePersona,
	}
	for ct, validate := range wrappers {
		t.Run(string(ct), func(t *testing.T) {
			assert.NoError(t, validate([]byte("apiVersion: v1\nkind: X\nspec:\n  id: a\n")))

			err := validate([]byte("apiVersion: v1\nkind: X\nspec:\n  stray: true\n"))
			require.Error(t, err)
			assert.Contains(t, err.Error(), string(ct)+" configuration does not match schema")
			assert.Contains(t, err.Error(), "stray")
		})
	}
}

// A compiled schema is cached per source, so repeat validations reuse it.
func TestSchemaCaching(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "arena.json"), []byte(testManifestSchema), 0o600))
	doc := []byte("apiVersion: v1\nkind: Arena\nspec: {}\n")

	_, err := ValidateWithLocalSchema(doc, ConfigTypeArena, dir)
	require.NoError(t, err)
	key := "file://" + dir + "/arena.json"
	first := schemaCache.get(key)
	require.NotNil(t, first, "schema should be cached after the first validation")

	_, err = ValidateWithLocalSchema(doc, ConfigTypeArena, dir)
	require.NoError(t, err)
	assert.Same(t, first, schemaCache.get(key))
}

// --- M32: Schema cache LRU eviction ---

func TestSchemaCacheStore_LRUEviction(t *testing.T) {
	cache := &schemaCacheStore{
		schemas: make(map[string]*schemaCacheEntry),
	}

	// Create a dummy schema for testing.
	loader := gojsonschema.NewStringLoader(`{"type": "object"}`)
	dummySchema, err := gojsonschema.NewSchema(loader)
	require.NoError(t, err)

	// Fill cache to capacity.
	for i := range maxSchemaCacheSize {
		cache.set(fmt.Sprintf("key-%d", i), dummySchema)
	}
	assert.Equal(t, maxSchemaCacheSize, len(cache.schemas))

	// Adding one more should evict the oldest (key-0).
	cache.set("key-new", dummySchema)
	assert.Equal(t, maxSchemaCacheSize, len(cache.schemas))
	assert.Nil(t, cache.get("key-0"), "oldest entry should be evicted")
	assert.NotNil(t, cache.get("key-new"), "new entry should be present")
}

func TestSchemaCacheStore_LRUTouchPromotes(t *testing.T) {
	cache := &schemaCacheStore{
		schemas: make(map[string]*schemaCacheEntry),
	}

	loader := gojsonschema.NewStringLoader(`{"type": "object"}`)
	dummySchema, err := gojsonschema.NewSchema(loader)
	require.NoError(t, err)

	// Fill cache to capacity.
	for i := range maxSchemaCacheSize {
		cache.set(fmt.Sprintf("key-%d", i), dummySchema)
	}

	// Touch key-0 to promote it.
	cache.get("key-0")

	// Adding a new entry should evict key-1 (the new oldest) instead of key-0.
	cache.set("key-new", dummySchema)
	assert.NotNil(t, cache.get("key-0"), "touched entry should not be evicted")
	assert.Nil(t, cache.get("key-1"), "second oldest should be evicted")
}

func TestSchemaCacheStore_SetExistingKey(t *testing.T) {
	cache := &schemaCacheStore{
		schemas: make(map[string]*schemaCacheEntry),
	}

	loader := gojsonschema.NewStringLoader(`{"type": "object"}`)
	dummySchema, err := gojsonschema.NewSchema(loader)
	require.NoError(t, err)

	cache.set("key-1", dummySchema)
	cache.set("key-1", dummySchema) // update existing
	assert.Equal(t, 1, len(cache.schemas))
	assert.NotNil(t, cache.get("key-1"))
}

func TestSchemaCacheStore_GetMiss(t *testing.T) {
	cache := &schemaCacheStore{
		schemas: make(map[string]*schemaCacheEntry),
	}
	assert.Nil(t, cache.get("nonexistent"))
}
