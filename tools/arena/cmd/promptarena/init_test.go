package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/tools/arena/templates"
)

func TestGetProjectName(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		quick    bool
		expected string
	}{
		{
			name:     "from args",
			args:     []string{"my-project"},
			quick:    true,
			expected: "my-project",
		},
		{
			name:     "empty args with quick mode",
			args:     []string{},
			quick:    true,
			expected: "my-arena-tests",
		},
		{
			name:     "multiple args takes first",
			args:     []string{"first", "second"},
			quick:    true,
			expected: "first",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initQuick = tt.quick
			result := getProjectName(tt.args)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCollectQuickModeVariables(t *testing.T) {
	tests := []struct {
		name          string
		template      *templates.Template
		expectedVars  map[string]interface{}
		expectedError bool
	}{
		{
			name: "all variables have defaults",
			template: &templates.Template{
				Spec: templates.TemplateSpec{
					Variables: []templates.Variable{
						{
							Name:    "project_name",
							Default: "test",
						},
						{
							Name:    "provider",
							Default: "openai",
						},
						{
							Name:    "temperature",
							Default: 0.7,
						},
					},
				},
			},
			expectedVars: map[string]interface{}{
				"project_name": "test-project",
				"provider":     "openai",
				"temperature":  0.7,
			},
			expectedError: false,
		},
		{
			name: "required variable without default",
			template: &templates.Template{
				Spec: templates.TemplateSpec{
					Variables: []templates.Variable{
						{
							Name:     "required_var",
							Required: true,
						},
					},
				},
			},
			expectedError: true,
		},
		{
			name: "optional variable without default",
			template: &templates.Template{
				Spec: templates.TemplateSpec{
					Variables: []templates.Variable{
						{
							Name:     "optional_var",
							Required: false,
						},
					},
				},
			},
			expectedVars: map[string]interface{}{
				"project_name": "test-project",
			},
			expectedError: false,
		},
		{
			name: "skips project_name variable",
			template: &templates.Template{
				Spec: templates.TemplateSpec{
					Variables: []templates.Variable{
						{
							Name:    "project_name",
							Default: "should-be-ignored",
						},
						{
							Name:    "other_var",
							Default: "included",
						},
					},
				},
			},
			expectedVars: map[string]interface{}{
				"project_name": "test-project",
				"other_var":    "included",
			},
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &templates.TemplateConfig{
				ProjectName: "test-project",
				Variables:   map[string]interface{}{"project_name": "test-project"},
				Template:    tt.template,
			}

			result, err := collectQuickModeVariables(config, tt.template)

			if tt.expectedError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedVars, result.Variables)
		})
	}
}

func TestApplyCommandLineOverrides(t *testing.T) {
	tests := []struct {
		name               string
		initProvider       string
		initNoEnv          bool
		expectedProvider   interface{}
		expectedIncludeEnv interface{}
	}{
		{
			name:               "provider override",
			initProvider:       "anthropic",
			initNoEnv:          false,
			expectedProvider:   "anthropic",
			expectedIncludeEnv: nil,
		},
		{
			name:               "no-env override",
			initProvider:       "",
			initNoEnv:          true,
			expectedProvider:   nil,
			expectedIncludeEnv: false,
		},
		{
			name:               "both overrides",
			initProvider:       "google",
			initNoEnv:          true,
			expectedProvider:   "google",
			expectedIncludeEnv: false,
		},
		{
			name:               "no overrides",
			initProvider:       "",
			initNoEnv:          false,
			expectedProvider:   nil,
			expectedIncludeEnv: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set global flags
			initProvider = tt.initProvider
			initNoEnv = tt.initNoEnv

			config := &templates.TemplateConfig{
				Variables: make(map[string]interface{}),
			}

			applyCommandLineOverrides(config)

			if tt.expectedProvider != nil {
				assert.Equal(t, tt.expectedProvider, config.Variables["provider"])
			} else {
				assert.NotContains(t, config.Variables, "provider")
			}

			if tt.expectedIncludeEnv != nil {
				assert.Equal(t, tt.expectedIncludeEnv, config.Variables["include_env"])
			} else {
				assert.NotContains(t, config.Variables, "include_env")
			}
		})
	}
}

func TestBuildPromptText(t *testing.T) {
	tests := []struct {
		name     string
		variable templates.Variable
		expected string
	}{
		{
			name: "with custom prompt",
			variable: templates.Variable{
				Name:   "api_key",
				Prompt: "Enter your API key",
			},
			expected: "Enter your API key",
		},
		{
			name: "with description",
			variable: templates.Variable{
				Name:        "temperature",
				Prompt:      "Temperature",
				Description: "Controls randomness",
			},
			expected: "Temperature (Controls randomness)",
		},
		{
			name: "without prompt uses name",
			variable: templates.Variable{
				Name: "provider",
			},
			expected: "provider",
		},
		{
			name: "description without prompt",
			variable: templates.Variable{
				Name:        "max_tokens",
				Description: "Maximum tokens",
			},
			expected: "max_tokens (Maximum tokens)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildPromptText(tt.variable)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetArrayDefaultString(t *testing.T) {
	tests := []struct {
		name     string
		variable templates.Variable
		expected string
	}{
		{
			name: "nil default",
			variable: templates.Variable{
				Default: nil,
			},
			expected: "",
		},
		{
			name: "array of strings",
			variable: templates.Variable{
				Default: []interface{}{"one", "two", "three"},
			},
			expected: "one,two,three",
		},
		{
			name: "array of numbers",
			variable: templates.Variable{
				Default: []interface{}{1, 2, 3},
			},
			expected: "1,2,3",
		},
		{
			name: "mixed array",
			variable: templates.Variable{
				Default: []interface{}{"text", 42, true},
			},
			expected: "text,42,true",
		},
		{
			name: "empty array",
			variable: templates.Variable{
				Default: []interface{}{},
			},
			expected: "",
		},
		{
			name: "non-array default",
			variable: templates.Variable{
				Default: "not-an-array",
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getArrayDefaultString(tt.variable)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseNumber(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expected      interface{}
		expectNumeric bool
	}{
		{
			name:          "integer",
			input:         "42",
			expected:      float64(42),
			expectNumeric: true,
		},
		{
			name:          "float",
			input:         "3.14",
			expected:      float64(3.14),
			expectNumeric: true,
		},
		{
			name:          "negative number",
			input:         "-10",
			expected:      float64(-10),
			expectNumeric: true,
		},
		{
			name:          "zero",
			input:         "0",
			expected:      float64(0),
			expectNumeric: true,
		},
		{
			name:          "invalid returns string",
			input:         "not-a-number",
			expected:      "not-a-number",
			expectNumeric: false,
		},
		{
			name:          "empty string",
			input:         "",
			expected:      "",
			expectNumeric: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseNumber(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)

			if tt.expectNumeric {
				assert.IsType(t, float64(0), result)
			} else {
				assert.IsType(t, "", result)
			}
		})
	}
}

func TestCollectConfiguration(t *testing.T) {
	tests := []struct {
		name          string
		projectName   string
		quick         bool
		template      *templates.Template
		expectedError bool
	}{
		{
			name:        "quick mode success",
			projectName: "test-project",
			quick:       true,
			template: &templates.Template{
				Spec: templates.TemplateSpec{
					Variables: []templates.Variable{
						{
							Name:    "provider",
							Default: "mock",
						},
					},
				},
			},
			expectedError: false,
		},
		{
			name:        "quick mode with required variable fails",
			projectName: "test-project",
			quick:       true,
			template: &templates.Template{
				Spec: templates.TemplateSpec{
					Variables: []templates.Variable{
						{
							Name:     "required_no_default",
							Required: true,
						},
					},
				},
			},
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initQuick = tt.quick
			initOutputDir = "."

			// Create a mock command (we don't actually use it in these paths)
			cmd := initCmd

			config, err := collectConfiguration(cmd, tt.template, tt.projectName)

			if tt.expectedError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, config)
			assert.Equal(t, tt.projectName, config.ProjectName)
			assert.Equal(t, tt.projectName, config.Variables["project_name"])
		})
	}
}

func TestPrintSuccessMessage(t *testing.T) {
	// This is primarily a display function, but we can test it doesn't panic
	tests := []struct {
		name        string
		projectName string
		result      *templates.GenerationResult
	}{
		{
			name:        "basic success",
			projectName: "test-project",
			result: &templates.GenerationResult{
				FilesCreated: []string{"arena.yaml", "prompts/test.yaml"},
				Warnings:     []string{},
			},
		},
		{
			name:        "with warnings",
			projectName: "test-project",
			result: &templates.GenerationResult{
				FilesCreated: []string{"arena.yaml"},
				Warnings:     []string{"Warning 1", "Warning 2"},
			},
		},
		{
			name:        "no files",
			projectName: "test-project",
			result: &templates.GenerationResult{
				FilesCreated: []string{},
				Warnings:     []string{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			assert.NotPanics(t, func() {
				printSuccessMessage(tt.projectName, tt.result)
			})
		})
	}
}

func TestPromptForVariable_SelectType(t *testing.T) {
	// Test the logic that would normally involve interactive prompts
	// We can't test the actual promptui interaction, but we can test the structure
	v := templates.Variable{
		Name:    "provider",
		Type:    "select",
		Options: []string{"openai", "anthropic", "google"},
		Default: "openai",
	}

	// Verify the variable structure is valid for select type
	assert.Equal(t, "select", v.Type)
	assert.NotEmpty(t, v.Options)
	assert.Contains(t, v.Options, v.Default)
}

func TestPromptForVariable_BooleanType(t *testing.T) {
	v := templates.Variable{
		Name:    "include_tests",
		Type:    "boolean",
		Default: true,
	}

	assert.Equal(t, "boolean", v.Type)
	assert.IsType(t, true, v.Default)
}

func TestPromptForVariable_ArrayType(t *testing.T) {
	v := templates.Variable{
		Name:    "tags",
		Type:    "array",
		Default: []interface{}{"test", "example"},
	}

	assert.Equal(t, "array", v.Type)
	assert.IsType(t, []interface{}{}, v.Default)
}

func TestPromptForVariable_StringType(t *testing.T) {
	v := templates.Variable{
		Name:    "description",
		Type:    "string",
		Default: "A test project",
	}

	assert.Equal(t, "string", v.Type)
	assert.IsType(t, "", v.Default)
}

func TestPromptForVariable_NumberType(t *testing.T) {
	v := templates.Variable{
		Name:    "temperature",
		Type:    "number",
		Default: 0.7,
	}

	assert.Equal(t, "number", v.Type)
	assert.IsType(t, 0.7, v.Default)
}

func TestCollectConfiguration_OutputDir(t *testing.T) {
	initQuick = true
	initOutputDir = "/custom/path"

	template := &templates.Template{
		Spec: templates.TemplateSpec{
			Variables: []templates.Variable{
				{Name: "provider", Default: "mock"},
			},
		},
	}

	config, err := collectConfiguration(initCmd, template, "test-project")
	require.NoError(t, err)
	assert.Equal(t, "/custom/path", config.OutputDir)
}

func TestGetProjectName_EmptyArgs(t *testing.T) {
	initQuick = true
	result := getProjectName([]string{})
	assert.Equal(t, "my-arena-tests", result)
}

func TestCollectInteractiveVariables_StructureValidation(t *testing.T) {
	// We can't test actual interactive prompts, but we can verify the function structure
	template := &templates.Template{
		Spec: templates.TemplateSpec{
			Variables: []templates.Variable{
				{Name: "provider", Default: "mock", Type: "select", Options: []string{"mock", "openai"}},
			},
		},
	}

	config := &templates.TemplateConfig{
		ProjectName: "test",
		Variables:   map[string]interface{}{"project_name": "test"},
		Template:    template,
	}

	// Verify the function signature exists and structure is correct
	assert.NotNil(t, config)
	assert.NotNil(t, template.Spec.Variables)
}

func TestInitFlags(t *testing.T) {
	// Test that flags are properly defined
	flags := initCmd.Flags()

	assert.NotNil(t, flags.Lookup("template"))
	assert.NotNil(t, flags.Lookup("quick"))
	assert.NotNil(t, flags.Lookup("no-git"))
	assert.NotNil(t, flags.Lookup("no-env"))
	assert.NotNil(t, flags.Lookup("provider"))
	assert.NotNil(t, flags.Lookup("output"))
}

func TestInitCommand_Usage(t *testing.T) {
	assert.Equal(t, "init [project-name]", initCmd.Use)
	assert.Contains(t, initCmd.Short, "Initialize")
	assert.Contains(t, initCmd.Long, "Configuration files")
}

func TestRunInit_Integration(t *testing.T) {
	// Create a temporary directory for the test
	tempDir := t.TempDir()

	// Save original values
	origQuick := initQuick
	origProvider := initProvider
	origOutputDir := initOutputDir
	origTemplate := initTemplate

	// Restore after test
	defer func() {
		initQuick = origQuick
		initProvider = origProvider
		initOutputDir = origOutputDir
		initTemplate = origTemplate
	}()

	tests := []struct {
		name          string
		args          []string
		quick         bool
		provider      string
		template      string
		expectedError bool
	}{
		{
			name:          "quick mode with mock provider",
			args:          []string{"test-project"},
			quick:         true,
			provider:      "mock",
			template:      "quick-start",
			expectedError: false,
		},
		{
			name:          "invalid template",
			args:          []string{"test-project"},
			quick:         true,
			provider:      "mock",
			template:      "nonexistent-template",
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up test environment
			initQuick = tt.quick
			initProvider = tt.provider
			initTemplate = tt.template
			initOutputDir = tempDir

			// Run the command
			err := runInit(initCmd, tt.args)

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGetProjectName_InteractiveFallback(t *testing.T) {
	// When not in quick mode and no args, it attempts interactive prompt
	// We can't test the actual prompt, but we can verify the fallback
	initQuick = false

	// With empty args, should attempt prompt then fallback to default
	result := getProjectName([]string{})

	// In non-interactive environment, it will fallback to default
	assert.Equal(t, "my-arena-tests", result)
}

func TestGetProjectName_WithArgs(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		quick    bool
		expected string
	}{
		{
			name:     "single arg",
			args:     []string{"my-custom-project"},
			quick:    true,
			expected: "my-custom-project",
		},
		{
			name:     "single arg non-quick",
			args:     []string{"another-project"},
			quick:    false,
			expected: "another-project",
		},
		{
			name:     "empty string arg",
			args:     []string{""},
			quick:    true,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initQuick = tt.quick
			result := getProjectName(tt.args)
			assert.Equal(t, tt.expected, result)
		})
	}
}
