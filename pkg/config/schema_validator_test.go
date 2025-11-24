package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
  type: openai
  model: gpt-4
`)

	err := ValidateProvider(validProvider)
	assert.NoError(t, err)
}

func TestValidatePromptConfig_Valid(t *testing.T) {
	validPromptConfig := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: test-prompt
spec:
  task_type: test
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
  mode: mock
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
  role: user
  description: Test user persona
  behavior: friendly
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
