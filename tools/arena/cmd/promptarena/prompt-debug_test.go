package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{
			name:     "empty string",
			text:     "",
			expected: 0,
		},
		{
			name:     "short text",
			text:     "Hello, world!",
			expected: 3, // 13 chars / 4 = 3.25 -> 3
		},
		{
			name:     "longer text",
			text:     "This is a longer text with multiple words and punctuation.",
			expected: 14, // 59 chars / 4 = 14.75 -> 14
		},
		{
			name:     "exactly 4 characters",
			text:     "test",
			expected: 1,
		},
		{
			name:     "exactly 8 characters",
			text:     "testtext",
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := estimateTokens(tt.text)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestOutputPromptJSON(t *testing.T) {
	tests := []struct {
		name             string
		systemPrompt     string
		region           string
		taskType         string
		configFile       string
		variables        map[string]string
		promptPacksCount int
		providersCount   int
		scenariosCount   int
		expectError      bool
	}{
		{
			name:             "basic output",
			systemPrompt:     "You are a helpful assistant.",
			region:           "us-east-1",
			taskType:         "chat",
			configFile:       "config.yaml",
			variables:        map[string]string{"var1": "value1"},
			promptPacksCount: 2,
			providersCount:   3,
			scenariosCount:   5,
			expectError:      false,
		},
		{
			name:             "empty prompt",
			systemPrompt:     "",
			region:           "us-west-2",
			taskType:         "summarization",
			configFile:       "test.yaml",
			variables:        map[string]string{},
			promptPacksCount: 1,
			providersCount:   1,
			scenariosCount:   1,
			expectError:      false,
		},
		{
			name:             "nil variables",
			systemPrompt:     "Test prompt",
			region:           "eu-west-1",
			taskType:         "task1",
			configFile:       "arena.yaml",
			variables:        nil,
			promptPacksCount: 0,
			providersCount:   0,
			scenariosCount:   0,
			expectError:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stdout
			old := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			params := promptJSONParams{
				SystemPrompt:     tt.systemPrompt,
				Region:           tt.region,
				TaskType:         tt.taskType,
				ConfigFile:       tt.configFile,
				Variables:        tt.variables,
				PromptPacksCount: tt.promptPacksCount,
				ProvidersCount:   tt.providersCount,
				ScenariosCount:   tt.scenariosCount,
			}
			err := outputPromptJSON(params)

			// Restore stdout
			w.Close()
			os.Stdout = old

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)

				// Read captured output
				var buf bytes.Buffer
				_, _ = io.Copy(&buf, r) // Ignore error and bytes written in test
				output := buf.String()

				// Verify JSON contains expected fields
				assert.Contains(t, output, `"region"`)
				assert.Contains(t, output, `"task_type"`)
				assert.Contains(t, output, `"system_prompt"`)
				assert.Contains(t, output, tt.region)
				assert.Contains(t, output, tt.taskType)
			}
		})
	}
}

func TestParsePromptDebugFlags(t *testing.T) {
	tests := []struct {
		name        string
		flags       map[string]interface{}
		expectError bool
		validate    func(t *testing.T, opts *promptDebugOptions)
	}{
		{
			name: "all flags set",
			flags: map[string]interface{}{
				"config":      "test.yaml",
				"scenario":    "scenario.yaml",
				"region":      "us-east-1",
				"task-type":   "chat",
				"persona":     "assistant",
				"context":     "test context",
				"domain":      "testing",
				"user":        "developer",
				"show-prompt": true,
				"show-meta":   false,
				"show-stats":  true,
				"json":        false,
				"list":        true,
				"verbose":     false,
			},
			expectError: false,
			validate: func(t *testing.T, opts *promptDebugOptions) {
				assert.Equal(t, "test.yaml", opts.ConfigFile)
				assert.Equal(t, "scenario.yaml", opts.ScenarioFile)
				assert.Equal(t, "us-east-1", opts.Region)
				assert.Equal(t, "chat", opts.TaskType)
				assert.Equal(t, "assistant", opts.Persona)
				assert.Equal(t, "test context", opts.Context)
				assert.Equal(t, "testing", opts.Domain)
				assert.Equal(t, "developer", opts.User)
				assert.True(t, opts.ShowPrompt)
				assert.False(t, opts.ShowMeta)
				assert.True(t, opts.ShowStats)
				assert.False(t, opts.OutputJSON)
				assert.True(t, opts.ListConfigs)
				assert.False(t, opts.Verbose)
			},
		},
		{
			name: "default flags",
			flags: map[string]interface{}{
				"config": "arena.yaml",
			},
			expectError: false,
			validate: func(t *testing.T, opts *promptDebugOptions) {
				assert.Equal(t, "arena.yaml", opts.ConfigFile)
				assert.Empty(t, opts.ScenarioFile)
				assert.Empty(t, opts.Region)
				assert.Empty(t, opts.TaskType)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}

			// Add flags to command
			cmd.Flags().StringP("config", "c", "arena.yaml", "Configuration file path")
			cmd.Flags().StringP("scenario", "", "", "Scenario file path")
			cmd.Flags().StringP("region", "r", "", "Region for prompt generation")
			cmd.Flags().StringP("task-type", "t", "", "Task type for prompt generation")
			cmd.Flags().StringP("persona", "", "", "Persona ID to test")
			cmd.Flags().StringP("context", "", "", "Context slot content")
			cmd.Flags().StringP("domain", "", "", "Domain hint")
			cmd.Flags().StringP("user", "", "", "User context")
			cmd.Flags().BoolP("show-prompt", "p", true, "Show the full assembled prompt")
			cmd.Flags().BoolP("show-meta", "m", true, "Show metadata and configuration info")
			cmd.Flags().BoolP("show-stats", "s", true, "Show statistics")
			cmd.Flags().BoolP("json", "j", false, "Output as JSON")
			cmd.Flags().BoolP("list", "l", false, "List available regions and task types")
			cmd.Flags().BoolP("verbose", "v", false, "Verbose output with debug info")

			// Set flags
			for key, value := range tt.flags {
				switch v := value.(type) {
				case string:
					err := cmd.Flags().Set(key, v)
					require.NoError(t, err)
				case bool:
					if v {
						err := cmd.Flags().Set(key, "true")
						require.NoError(t, err)
					} else {
						err := cmd.Flags().Set(key, "false")
						require.NoError(t, err)
					}
				}
			}

			opts, err := parsePromptDebugFlags(cmd)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, opts)

			if tt.validate != nil {
				tt.validate(t, opts)
			}
		})
	}
}

func TestParseStringFlags(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().StringP("config", "c", "arena.yaml", "Configuration file path")
	cmd.Flags().StringP("scenario", "", "", "Scenario file path")
	cmd.Flags().StringP("region", "r", "", "Region for prompt generation")
	cmd.Flags().StringP("task-type", "t", "", "Task type for prompt generation")
	cmd.Flags().StringP("persona", "", "", "Persona ID to test")
	cmd.Flags().StringP("context", "", "", "Context slot content")
	cmd.Flags().StringP("domain", "", "", "Domain hint")
	cmd.Flags().StringP("user", "", "", "User context")

	opts := &promptDebugOptions{}

	// Set some test values
	err := cmd.Flags().Set("config", "test.yaml")
	require.NoError(t, err)
	err = cmd.Flags().Set("region", "us-west-2")
	require.NoError(t, err)

	err = parseStringFlags(cmd, opts)
	assert.NoError(t, err)

	assert.Equal(t, "test.yaml", opts.ConfigFile)
	assert.Equal(t, "us-west-2", opts.Region)
	assert.Empty(t, opts.ScenarioFile) // Not set
}

func TestParseBoolFlags(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().BoolP("show-prompt", "p", true, "Show the full assembled prompt")
	cmd.Flags().BoolP("show-meta", "m", true, "Show metadata and configuration info")
	cmd.Flags().BoolP("show-stats", "s", true, "Show statistics")
	cmd.Flags().BoolP("json", "j", false, "Output as JSON")
	cmd.Flags().BoolP("list", "l", false, "List available regions and task types")
	cmd.Flags().BoolP("verbose", "v", false, "Verbose output with debug info")

	opts := &promptDebugOptions{}

	// Set some test values (override defaults)
	err := cmd.Flags().Set("show-prompt", "false")
	require.NoError(t, err)
	err = cmd.Flags().Set("json", "true")
	require.NoError(t, err)

	err = parseBoolFlags(cmd, opts)
	assert.NoError(t, err)

	assert.False(t, opts.ShowPrompt) // Overridden to false
	assert.True(t, opts.OutputJSON)  // Overridden to true
	assert.True(t, opts.ShowMeta)    // Default value
}

func TestNormalizeConfigPath(t *testing.T) {
	tests := []struct {
		name           string
		configFile     string
		setupDir       bool
		expectedResult string
	}{
		{
			name:           "file path unchanged",
			configFile:     "config.yaml",
			setupDir:       false,
			expectedResult: "config.yaml",
		},
		{
			name:           "directory path gets arena.yaml appended",
			configFile:     "", // Will be set to temp dir in test
			setupDir:       true,
			expectedResult: "", // Will be checked in test
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &promptDebugOptions{
				ConfigFile: tt.configFile,
			}

			if tt.setupDir {
				// Create a temporary directory
				tmpDir, err := os.MkdirTemp("", "test_config_dir")
				require.NoError(t, err)
				defer os.RemoveAll(tmpDir)

				opts.ConfigFile = tmpDir
				expectedPath := filepath.Join(tmpDir, "arena.yaml")

				normalizeConfigPath(opts)
				assert.Equal(t, expectedPath, opts.ConfigFile)
			} else {
				normalizeConfigPath(opts)
				assert.Equal(t, tt.expectedResult, opts.ConfigFile)
			}
		})
	}
}

func TestApplyScenarioData(t *testing.T) {
	opts := &promptDebugOptions{
		Domain: "existing-domain", // This should not be overridden
		User:   "",                // This should be set from scenario
	}

	scenario := &config.Scenario{
		TaskType: "test-task",
		ContextMetadata: &config.ContextMetadata{
			Domain:   "scenario-domain",
			UserRole: "test-user",
		},
		Context: map[string]interface{}{
			"user_context": "test context from map",
		},
	}

	applyScenarioData(opts, scenario)

	assert.Equal(t, "test-task", opts.TaskType)
	assert.Equal(t, "existing-domain", opts.Domain)        // Should not be overridden
	assert.Equal(t, "test-user", opts.User)                // Should be set from scenario
	assert.Equal(t, "test context from map", opts.Context) // Should be set from context map
}

func TestApplyContextMetadata(t *testing.T) {
	tests := []struct {
		name           string
		opts           *promptDebugOptions
		scenario       *config.Scenario
		expectedDomain string
		expectedUser   string
	}{
		{
			name: "nil context metadata",
			opts: &promptDebugOptions{},
			scenario: &config.Scenario{
				ContextMetadata: nil,
			},
			expectedDomain: "",
			expectedUser:   "",
		},
		{
			name: "empty flags get filled",
			opts: &promptDebugOptions{
				Domain: "",
				User:   "",
			},
			scenario: &config.Scenario{
				ContextMetadata: &config.ContextMetadata{
					Domain:   "test-domain",
					UserRole: "test-role",
				},
			},
			expectedDomain: "test-domain",
			expectedUser:   "test-role",
		},
		{
			name: "existing flags not overridden",
			opts: &promptDebugOptions{
				Domain: "existing-domain",
				User:   "existing-user",
			},
			scenario: &config.Scenario{
				ContextMetadata: &config.ContextMetadata{
					Domain:   "scenario-domain",
					UserRole: "scenario-role",
				},
			},
			expectedDomain: "existing-domain",
			expectedUser:   "existing-user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applyContextMetadata(tt.opts, tt.scenario)
			assert.Equal(t, tt.expectedDomain, tt.opts.Domain)
			assert.Equal(t, tt.expectedUser, tt.opts.User)
		})
	}
}

func TestApplyContextFromMap(t *testing.T) {
	tests := []struct {
		name            string
		opts            *promptDebugOptions
		scenario        *config.Scenario
		expectedContext string
	}{
		{
			name: "context already set - no override",
			opts: &promptDebugOptions{
				Context: "existing-context",
			},
			scenario: &config.Scenario{
				Context: map[string]interface{}{
					"user_context": "scenario-context",
				},
			},
			expectedContext: "existing-context",
		},
		{
			name: "nil context map",
			opts: &promptDebugOptions{
				Context: "",
			},
			scenario: &config.Scenario{
				Context: nil,
			},
			expectedContext: "",
		},
		{
			name: "no user_context key",
			opts: &promptDebugOptions{
				Context: "",
			},
			scenario: &config.Scenario{
				Context: map[string]interface{}{
					"other_key": "value",
				},
			},
			expectedContext: "",
		},
		{
			name: "non-string user_context value",
			opts: &promptDebugOptions{
				Context: "",
			},
			scenario: &config.Scenario{
				Context: map[string]interface{}{
					"user_context": 123,
				},
			},
			expectedContext: "",
		},
		{
			name: "valid string user_context",
			opts: &promptDebugOptions{
				Context: "",
			},
			scenario: &config.Scenario{
				Context: map[string]interface{}{
					"user_context": "valid-context",
				},
			},
			expectedContext: "valid-context",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applyContextFromMap(tt.opts, tt.scenario)
			assert.Equal(t, tt.expectedContext, tt.opts.Context)
		})
	}
}

func TestExtractRegionFromPersonaID(t *testing.T) {
	tests := []struct {
		name     string
		persona  string
		expected string
	}{
		{
			name:     "us persona with prefix",
			persona:  "us-hustler-v1",
			expected: "us",
		},
		{
			name:     "uk persona",
			persona:  "test-uk-assistant-v2",
			expected: "uk",
		},
		{
			name:     "au persona",
			persona:  "service-au-bot-v1",
			expected: "au",
		},
		{
			name:     "us persona with us- pattern",
			persona:  "assistant-us-role-v3",
			expected: "us",
		},
		{
			name:     "unknown persona defaults to us",
			persona:  "unknown-persona-v1",
			expected: "us",
		},
		{
			name:     "empty persona defaults to us",
			persona:  "",
			expected: "us",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractRegionFromPersonaID(tt.persona)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDisplayScenarioInfo(t *testing.T) {
	tests := []struct {
		name     string
		opts     *promptDebugOptions
		scenario *config.Scenario
		verbose  bool
	}{
		{
			name: "verbose disabled - no output",
			opts: &promptDebugOptions{
				Verbose:      false,
				ScenarioFile: "test.yaml",
			},
			scenario: &config.Scenario{
				TaskType: "test-task",
			},
		},
		{
			name: "verbose enabled - shows output",
			opts: &promptDebugOptions{
				Verbose:      true,
				ScenarioFile: "test.yaml",
				Domain:       "test-domain",
				User:         "test-user",
				Context:      "test-context",
			},
			scenario: &config.Scenario{
				TaskType: "test-task",
				ContextMetadata: &config.ContextMetadata{
					Domain:   "scenario-domain",
					UserRole: "scenario-user",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just test that the function doesn't panic
			displayScenarioInfo(tt.opts, tt.scenario)
		})
	}
}

func TestRunPromptDebug_ErrorCases(t *testing.T) {
	// Test with invalid command setup
	cmd := &cobra.Command{}

	// Test without required flags set up
	err := runPromptDebug(cmd)
	assert.Error(t, err, "Should error when flags are not set up")
}

func TestApplyScenarioOverrides_ErrorCases(t *testing.T) {
	opts := &promptDebugOptions{
		ScenarioFile: "/nonexistent/scenario.yaml",
	}
	cfg := &config.Config{}

	// Test with non-existent scenario file
	err := applyScenarioOverrides(opts, cfg)
	assert.Error(t, err, "Should error with non-existent scenario file")

	// Test with empty scenario file (no error case)
	opts.ScenarioFile = ""
	err = applyScenarioOverrides(opts, cfg)
	assert.NoError(t, err, "Should not error when no scenario file specified")
}
