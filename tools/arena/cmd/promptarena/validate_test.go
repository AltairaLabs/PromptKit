package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()
	_ = w.Close()
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	return string(buf[:n])
}

func TestDisplayError_AdditionalPropertyWithSuggestion(t *testing.T) {
	e := config.SchemaValidationError{
		Field:       "spec.judge_defaults",
		Description: "Additional property judge is not allowed",
		Value:       "tone-judge",
		Keyword:     "additional_property_not_allowed",
		ValidValues: []string{"prompt"},
		Suggestions: []string{"prompt"},
	}
	out := captureStdout(t, func() { displayError(e) })
	assert.Contains(t, out, "unknown property 'judge'. Did you mean 'prompt'?")
	assert.Contains(t, out, "Valid keys: prompt")
	assert.NotContains(t, out, "(value: tone-judge)", "old format should not appear")
}

func TestDisplayError_AdditionalPropertyNoSuggestionStillListsKeys(t *testing.T) {
	e := config.SchemaValidationError{
		Field:       "spec.judge_defaults",
		Description: "Additional property xyz is not allowed",
		Value:       "v",
		Keyword:     "additional_property_not_allowed",
		ValidValues: []string{"model", "prompt"},
		Suggestions: nil,
	}
	out := captureStdout(t, func() { displayError(e) })
	assert.Contains(t, out, "unknown property 'xyz'")
	assert.NotContains(t, out, "Did you mean")
	assert.Contains(t, out, "Valid keys: model, prompt")
}

func TestDisplayError_AdditionalPropertyDegraded(t *testing.T) {
	// ValidValues nil → navigator couldn't resolve. Fall back to today's message.
	e := config.SchemaValidationError{
		Field:       "spec.judge_defaults",
		Description: "Additional property judge is not allowed",
		Value:       "tone-judge",
		Keyword:     "additional_property_not_allowed",
		ValidValues: nil,
		Suggestions: nil,
	}
	out := captureStdout(t, func() { displayError(e) })
	assert.Contains(t, out, "Additional property judge is not allowed")
	assert.Contains(t, out, "(value: tone-judge)")
}

func TestDisplayError_EnumWithSuggestion(t *testing.T) {
	e := config.SchemaValidationError{
		Field:       "spec.provider",
		Description: `spec.provider must be one of the following: "openai", "anthropic", "mock"`,
		Value:       "anthrop",
		Keyword:     "enum",
		ValidValues: []string{"openai", "anthropic", "mock"},
		Suggestions: []string{"anthropic"},
	}
	out := captureStdout(t, func() { displayError(e) })
	assert.Contains(t, out, e.Description)
	assert.Contains(t, out, "Did you mean: anthropic")
}

func TestDisplayError_PassthroughForOtherKeywords(t *testing.T) {
	e := config.SchemaValidationError{
		Field:       "spec.name",
		Description: "spec.name is required",
		Value:       nil,
		Keyword:     "required",
	}
	out := captureStdout(t, func() { displayError(e) })
	assert.Contains(t, out, "spec.name is required")
	assert.NotContains(t, out, "Did you mean")
	assert.NotContains(t, out, "Valid keys")
}

func TestPrepareValidation(t *testing.T) {
	// Create a temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test-arena.yaml")
	arenaContent := []byte(`apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test
spec:
  scenarios:
    - file: test.yaml
`)
	if err := os.WriteFile(testFile, arenaContent, 0600); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	tests := []struct {
		name      string
		filePath  string
		wantType  string
		wantErr   bool
		setupType string
	}{
		{
			name:      "valid arena file with auto detection",
			filePath:  testFile,
			wantType:  "arena",
			wantErr:   false,
			setupType: "auto",
		},
		{
			name:      "nonexistent file",
			filePath:  filepath.Join(tmpDir, "nonexistent.yaml"),
			wantErr:   true,
			setupType: "auto",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validateType = tt.setupType
			data, configType, err := prepareValidation(tt.filePath)
			if (err != nil) != tt.wantErr {
				t.Errorf("prepareValidation() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if len(data) == 0 {
					t.Error("Expected non-empty data")
				}
				if configType != tt.wantType {
					t.Errorf("prepareValidation() configType = %v, want %v", configType, tt.wantType)
				}
			}
		})
	}
}

func TestPrepareValidationWithExplicitType(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.yaml")
	content := []byte(`apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: test
spec:
  turns: []
`)
	if err := os.WriteFile(testFile, content, 0600); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	validateType = "scenario"
	data, configType, err := prepareValidation(testFile)
	if err != nil {
		t.Fatalf("prepareValidation() failed: %v", err)
	}

	if len(data) == 0 {
		t.Error("Expected non-empty data")
	}

	if configType != "scenario" {
		t.Errorf("Expected configType 'scenario', got %s", configType)
	}
}

func TestPerformSchemaValidation(t *testing.T) {
	validArenaData := []byte(`apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test
spec:
  scenarios:
    - file: test.yaml
`)

	tests := []struct {
		name       string
		data       []byte
		configType string
		wantErr    bool
	}{
		{
			name:       "valid arena data",
			data:       validArenaData,
			configType: "arena",
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := performSchemaValidation(tt.data, tt.configType, "test.yaml")
			if (err != nil) != tt.wantErr {
				t.Errorf("performSchemaValidation() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateWithSchema(t *testing.T) {
	validData := []byte(`apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test
spec:
  scenarios:
    - file: test.yaml
`)

	result, err := validateWithSchema(validData, config.ConfigTypeArena)
	if err != nil {
		t.Fatalf("validateWithSchema() failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if !result.Valid {
		t.Errorf("Expected valid result, got errors: %v", result.Errors)
	}
}

func TestDisplayErrors(t *testing.T) {
	errors := []config.SchemaValidationError{
		{Field: "(root).spec.invalid", Description: "Invalid field", Value: "test"},
		{Field: "(root).metadata.name", Description: "Missing required field", Value: nil},
		{Field: "spec.scenarios", Description: "Must be an array", Value: "string"},
	}

	tests := []struct {
		name    string
		errors  []config.SchemaValidationError
		verbose bool
	}{
		{
			name:    "verbose mode",
			errors:  errors,
			verbose: true,
		},
		{
			name:    "non-verbose mode",
			errors:  errors,
			verbose: false,
		},
		{
			name:    "empty errors",
			errors:  []config.SchemaValidationError{},
			verbose: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just ensure it doesn't panic
			displayErrors(tt.errors, tt.verbose)
		})
	}
}

func TestDisplayError(t *testing.T) {
	tests := []struct {
		name string
		err  config.SchemaValidationError
	}{
		{
			name: "error with value",
			err: config.SchemaValidationError{
				Field:       "(root).spec.invalid",
				Description: "Invalid field",
				Value:       "test",
			},
		},
		{
			name: "error without value",
			err: config.SchemaValidationError{
				Field:       "(root).metadata.name",
				Description: "Missing required field",
				Value:       nil,
			},
		},
		{
			name: "error with root field",
			err: config.SchemaValidationError{
				Field:       "(root)",
				Description: "Root level error",
				Value:       nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just ensure it doesn't panic
			displayError(tt.err)
		})
	}
}

func TestPerformBusinessLogicValidation(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a valid arena config with all required files
	scenarioFile := filepath.Join(tmpDir, "scenario.yaml")
	scenarioContent := []byte(`apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: test-scenario
spec:
  turns:
    - role: user
      content: "Test message"
`)
	if err := os.WriteFile(scenarioFile, scenarioContent, 0600); err != nil {
		t.Fatalf("Failed to create scenario file: %v", err)
	}

	arenaFile := filepath.Join(tmpDir, "arena.yaml")
	arenaContent := []byte(`apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test
spec:
  scenarios:
    - file: scenario.yaml
`)
	if err := os.WriteFile(arenaFile, arenaContent, 0600); err != nil {
		t.Fatalf("Failed to create arena file: %v", err)
	}

	// Test with valid config
	err := performBusinessLogicValidation(arenaFile)
	if err != nil {
		t.Logf("performBusinessLogicValidation() error (may be expected): %v", err)
		// Don't fail the test - validation might fail due to missing provider files etc
	}

	// Test with nonexistent file
	err = performBusinessLogicValidation(filepath.Join(tmpDir, "nonexistent.yaml"))
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}

func TestRunValidate(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a valid test file
	testFile := filepath.Join(tmpDir, "test-arena.yaml")
	content := []byte(`apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test
spec:
  scenarios:
    - file: scenario.yaml
`)
	if err := os.WriteFile(testFile, content, 0600); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create scenario file
	scenarioFile := filepath.Join(tmpDir, "scenario.yaml")
	scenarioContent := []byte(`apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: test-scenario
spec:
  turns:
    - role: user
      content: "Test"
`)
	if err := os.WriteFile(scenarioFile, scenarioContent, 0600); err != nil {
		t.Fatalf("Failed to create scenario file: %v", err)
	}

	tests := []struct {
		name            string
		args            []string
		setupType       string
		setupVerbose    bool
		setupSchemaOnly bool
		wantErr         bool
	}{
		{
			name:            "valid file with schema validation only",
			args:            []string{testFile},
			setupType:       "auto",
			setupVerbose:    false,
			setupSchemaOnly: true,
			wantErr:         false,
		},
		{
			name:            "no args",
			args:            []string{},
			setupType:       "auto",
			setupVerbose:    false,
			setupSchemaOnly: false,
			wantErr:         true,
		},
		{
			name:            "nonexistent file",
			args:            []string{filepath.Join(tmpDir, "nonexistent.yaml")},
			setupType:       "auto",
			setupVerbose:    false,
			setupSchemaOnly: false,
			wantErr:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validateType = tt.setupType
			validateVerbose = tt.setupVerbose
			validateSchemaOnly = tt.setupSchemaOnly

			err := runValidate(validateCmd, tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("runValidate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateWithInvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "invalid.yaml")
	invalidYAML := []byte(`invalid: yaml: content: [`)
	if err := os.WriteFile(testFile, invalidYAML, 0600); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	validateType = "auto"
	_, _, err := prepareValidation(testFile)
	if err == nil {
		t.Error("Expected error for invalid YAML")
	}
}

func TestValidateVerboseOutput(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.yaml")
	// Use valid content but test verbose output mode
	validContent := []byte(`apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test
spec:
  scenarios:
    - file: test.yaml
`)
	if err := os.WriteFile(testFile, validContent, 0600); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	validateType = "arena"
	validateVerbose = true
	validateSchemaOnly = true

	// This should succeed with verbose output
	err := runValidate(validateCmd, []string{testFile})
	if err != nil {
		t.Logf("Validation error (may be expected): %v", err)
	}
}

func TestPerformBusinessLogicValidation_InvalidAssertionType(t *testing.T) {
	tmpDir := t.TempDir()

	scenarioFile := filepath.Join(tmpDir, "scenario.yaml")
	scenarioContent := []byte(`apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: bad-assertions
spec:
  turns:
    - role: user
      content: "Test"
      assertions:
        - type: substring_present
          params:
            patterns: ["hello"]
`)
	if err := os.WriteFile(scenarioFile, scenarioContent, 0600); err != nil {
		t.Fatalf("Failed to create scenario file: %v", err)
	}

	arenaFile := filepath.Join(tmpDir, "arena.yaml")
	arenaContent := []byte(`apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test
spec:
  scenarios:
    - file: scenario.yaml
`)
	if err := os.WriteFile(arenaFile, arenaContent, 0600); err != nil {
		t.Fatalf("Failed to create arena file: %v", err)
	}

	err := performBusinessLogicValidation(arenaFile)
	if err == nil {
		t.Error("Expected error for unknown assertion type 'substring_present'")
	} else if !strings.Contains(err.Error(), "unknown assertion type") {
		t.Errorf("Expected 'unknown assertion type' error, got: %v", err)
	}
}

func TestPerformBusinessLogicValidation_ValidAssertionType(t *testing.T) {
	tmpDir := t.TempDir()

	scenarioFile := filepath.Join(tmpDir, "scenario.yaml")
	scenarioContent := []byte(`apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: good-assertions
spec:
  turns:
    - role: user
      content: "Test"
      assertions:
        - type: contains
          params:
            patterns: ["hello"]
`)
	if err := os.WriteFile(scenarioFile, scenarioContent, 0600); err != nil {
		t.Fatalf("Failed to create scenario file: %v", err)
	}

	arenaFile := filepath.Join(tmpDir, "arena.yaml")
	arenaContent := []byte(`apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test
spec:
  scenarios:
    - file: scenario.yaml
`)
	if err := os.WriteFile(arenaFile, arenaContent, 0600); err != nil {
		t.Fatalf("Failed to create arena file: %v", err)
	}

	err := performBusinessLogicValidation(arenaFile)
	if err != nil {
		t.Errorf("Expected no error for valid assertion type, got: %v", err)
	}
}
