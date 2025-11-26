package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

func TestGetStringFromMap(t *testing.T) {
	tests := []struct {
		name     string
		m        map[string]interface{}
		key      string
		expected string
	}{
		{
			name:     "key exists with string value",
			m:        map[string]interface{}{"name": "test"},
			key:      "name",
			expected: "test",
		},
		{
			name:     "key does not exist",
			m:        map[string]interface{}{"name": "test"},
			key:      "missing",
			expected: "",
		},
		{
			name:     "key exists with non-string value",
			m:        map[string]interface{}{"count": 42},
			key:      "count",
			expected: "",
		},
		{
			name:     "empty map",
			m:        map[string]interface{}{},
			key:      "any",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getStringFromMap(tt.m, tt.key)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertToEngineRunResult(t *testing.T) {
	now := time.Now()
	duration := 5 * time.Second

	t.Run("basic conversion without roles", func(t *testing.T) {
		sr := &statestore.RunResult{
			RunID:       "test-run-001",
			PromptPack:  "test-pack",
			Region:      "us-east-1",
			ScenarioID:  "scenario-1",
			ProviderID:  "openai",
			Params:      map[string]interface{}{"temp": 0.7},
			Messages:    []types.Message{{Role: "user", Content: "test"}},
			Commit:      map[string]interface{}{"sha": "abc123"},
			Cost:        types.CostInfo{TotalCost: 0.001},
			ToolStats:   &types.ToolStats{TotalCalls: 5},
			Violations:  []types.ValidationError{},
			StartTime:   now,
			EndTime:     now.Add(duration),
			Duration:    duration,
			Error:       "",
			SelfPlay:    false,
			PersonaID:   "",
			SessionTags: []string{"test", "debug"},
		}

		result := convertToEngineRunResult(sr)

		assert.Equal(t, sr.RunID, result.RunID)
		assert.Equal(t, sr.PromptPack, result.PromptPack)
		assert.Equal(t, sr.Region, result.Region)
		assert.Equal(t, sr.ScenarioID, result.ScenarioID)
		assert.Equal(t, sr.ProviderID, result.ProviderID)
		assert.Equal(t, sr.Params, result.Params)
		assert.Equal(t, sr.Messages, result.Messages)
		assert.Equal(t, sr.Commit, result.Commit)
		assert.Equal(t, sr.Cost, result.Cost)
		assert.Equal(t, sr.ToolStats, result.ToolStats)
		assert.Equal(t, sr.Violations, result.Violations)
		assert.Equal(t, sr.StartTime, result.StartTime)
		assert.Equal(t, sr.EndTime, result.EndTime)
		assert.Equal(t, sr.Duration, result.Duration)
		assert.Equal(t, sr.Error, result.Error)
		assert.Equal(t, sr.SelfPlay, result.SelfPlay)
		assert.Equal(t, sr.PersonaID, result.PersonaID)
		assert.Equal(t, sr.SessionTags, result.SessionTags)
		assert.Nil(t, result.AssistantRole)
		assert.Nil(t, result.UserRole)
	})

	t.Run("conversion with assistant role", func(t *testing.T) {
		sr := &statestore.RunResult{
			RunID:      "test-run-002",
			Region:     "us-west-2",
			ScenarioID: "scenario-2",
			ProviderID: "anthropic",
			Messages:   []types.Message{},
			StartTime:  now,
			EndTime:    now.Add(duration),
			Duration:   duration,
			AssistantRole: map[string]interface{}{
				"Provider": "openai",
				"Model":    "gpt-4",
				"Region":   "us-east-1",
			},
		}

		result := convertToEngineRunResult(sr)

		require.NotNil(t, result.AssistantRole)
		assert.Equal(t, "openai", result.AssistantRole.Provider)
		assert.Equal(t, "gpt-4", result.AssistantRole.Model)
		assert.Equal(t, "us-east-1", result.AssistantRole.Region)
	})

	t.Run("conversion with user role", func(t *testing.T) {
		sr := &statestore.RunResult{
			RunID:      "test-run-003",
			Region:     "eu-west-1",
			ScenarioID: "scenario-3",
			ProviderID: "anthropic",
			Messages:   []types.Message{},
			StartTime:  now,
			EndTime:    now.Add(duration),
			Duration:   duration,
			UserRole: map[string]interface{}{
				"Provider": "anthropic",
				"Model":    "claude-3",
				"Region":   "us-west-2",
			},
		}

		result := convertToEngineRunResult(sr)

		require.NotNil(t, result.UserRole)
		assert.Equal(t, "anthropic", result.UserRole.Provider)
		assert.Equal(t, "claude-3", result.UserRole.Model)
		assert.Equal(t, "us-west-2", result.UserRole.Region)
	})

	t.Run("conversion with both roles", func(t *testing.T) {
		sr := &statestore.RunResult{
			RunID:      "test-run-004",
			Region:     "ap-south-1",
			ScenarioID: "scenario-4",
			ProviderID: "openai",
			Messages:   []types.Message{},
			StartTime:  now,
			EndTime:    now.Add(duration),
			Duration:   duration,
			SelfPlay:   true,
			PersonaID:  "persona-123",
			AssistantRole: map[string]interface{}{
				"Provider": "openai",
				"Model":    "gpt-4",
				"Region":   "us-east-1",
			},
			UserRole: map[string]interface{}{
				"Provider": "anthropic",
				"Model":    "claude-3",
				"Region":   "us-west-2",
			},
		}

		result := convertToEngineRunResult(sr)

		assert.True(t, result.SelfPlay)
		assert.Equal(t, "persona-123", result.PersonaID)
		require.NotNil(t, result.AssistantRole)
		require.NotNil(t, result.UserRole)
		assert.Equal(t, "openai", result.AssistantRole.Provider)
		assert.Equal(t, "anthropic", result.UserRole.Provider)
	})

	t.Run("conversion with invalid role type", func(t *testing.T) {
		sr := &statestore.RunResult{
			RunID:         "test-run-005",
			Region:        "us-east-1",
			ScenarioID:    "scenario-5",
			ProviderID:    "openai",
			Messages:      []types.Message{},
			StartTime:     now,
			EndTime:       now.Add(duration),
			Duration:      duration,
			AssistantRole: "not-a-map", // Invalid type
		}

		result := convertToEngineRunResult(sr)

		// Should handle invalid type gracefully
		assert.Nil(t, result.AssistantRole)
	})
}

func TestCountResultsByStatus(t *testing.T) {
	tests := []struct {
		name            string
		results         []engine.RunResult
		expectedSuccess int
		expectedError   int
	}{
		{
			name:            "no results",
			results:         []engine.RunResult{},
			expectedSuccess: 0,
			expectedError:   0,
		},
		{
			name: "all successful",
			results: []engine.RunResult{
				{RunID: "run-001", Error: ""},
				{RunID: "run-002", Error: ""},
				{RunID: "run-003", Error: ""},
			},
			expectedSuccess: 3,
			expectedError:   0,
		},
		{
			name: "all errors",
			results: []engine.RunResult{
				{RunID: "run-001", Error: "error 1"},
				{RunID: "run-002", Error: "error 2"},
			},
			expectedSuccess: 0,
			expectedError:   2,
		},
		{
			name: "mixed results",
			results: []engine.RunResult{
				{RunID: "run-001", Error: ""},
				{RunID: "run-002", Error: "some error"},
				{RunID: "run-003", Error: ""},
				{RunID: "run-004", Error: "another error"},
			},
			expectedSuccess: 2,
			expectedError:   2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			successCount, errorCount := countResultsByStatus(tt.results)
			assert.Equal(t, tt.expectedSuccess, successCount)
			assert.Equal(t, tt.expectedError, errorCount)
		})
	}
}

func TestMockProviderFlagsRegistered(t *testing.T) {
	// Test that mock provider flags are properly registered
	mockProviderFlag := runCmd.Flags().Lookup("mock-provider")
	require.NotNil(t, mockProviderFlag, "mock-provider flag should be registered")
	assert.Equal(t, "false", mockProviderFlag.DefValue, "mock-provider should default to false")

	mockConfigFlag := runCmd.Flags().Lookup("mock-config")
	require.NotNil(t, mockConfigFlag, "mock-config flag should be registered")
	assert.Equal(t, "", mockConfigFlag.DefValue, "mock-config should default to empty string")
}

func TestExtractRunParameters_MockProviderFlags(t *testing.T) {
	// Create a test command with mock provider flags set
	cfg := &config.Config{
		Defaults: config.Defaults{
			Verbose: false,
		},
	}

	// Set the mock provider flags
	_ = runCmd.Flags().Set("mock-provider", "true")
	_ = runCmd.Flags().Set("mock-config", "/path/to/mock.yaml")
	_ = runCmd.Flags().Set("verbose", "false")
	_ = runCmd.Flags().Set("ci", "false")

	// Extract parameters
	params, err := extractRunParameters(runCmd, cfg)
	require.NoError(t, err)

	// Verify mock provider parameters are extracted
	assert.True(t, params.MockProvider, "MockProvider should be true")
	assert.Equal(t, "/path/to/mock.yaml", params.MockConfig, "MockConfig should match")

	// Clean up flags for other tests
	_ = runCmd.Flags().Set("mock-provider", "false")
	_ = runCmd.Flags().Set("mock-config", "")
}

func TestSetDefaultFilePaths(t *testing.T) {
	tests := []struct {
		name              string
		params            *RunParameters
		expectedOutDir    string
		expectedJUnitFile string
		expectedHTMLFile  string
	}{
		{
			name: "all empty - sets junit default",
			params: &RunParameters{
				OutDir:    "",
				JUnitFile: "",
				HTMLFile:  "",
			},
			expectedOutDir:    "",          // OutDir not changed by function
			expectedJUnitFile: "junit.xml", // Default JUnit file
			expectedHTMLFile:  "",
		},
		{
			name: "custom outdir",
			params: &RunParameters{
				OutDir:    "custom-output",
				JUnitFile: "",
				HTMLFile:  "",
			},
			expectedOutDir:    "custom-output",
			expectedJUnitFile: "custom-output/junit.xml", // JUnit uses OutDir
			expectedHTMLFile:  "",
		},
		{
			name: "custom files - keeps as-is",
			params: &RunParameters{
				OutDir:    "results",
				JUnitFile: "custom.xml",
				HTMLFile:  "custom.html",
			},
			expectedOutDir:    "results",
			expectedJUnitFile: "custom.xml",
			expectedHTMLFile:  "custom.html",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a minimal config for the test
			cfg := &config.Config{}
			setDefaultFilePaths(cfg, tt.params)
			assert.Equal(t, tt.expectedOutDir, tt.params.OutDir)
			assert.Equal(t, tt.expectedJUnitFile, tt.params.JUnitFile)
			assert.Equal(t, tt.expectedHTMLFile, tt.params.HTMLFile)
		})
	}
}

func TestCreateResultSummary(t *testing.T) {
	tests := []struct {
		name         string
		results      []engine.RunResult
		successCount int
		errorCount   int
		configFile   string
	}{
		{
			name:         "empty results",
			results:      []engine.RunResult{},
			successCount: 0,
			errorCount:   0,
			configFile:   "test.yaml",
		},
		{
			name: "successful runs",
			results: []engine.RunResult{
				{RunID: "run-001", Error: ""},
				{RunID: "run-002", Error: ""},
			},
			successCount: 2,
			errorCount:   0,
			configFile:   "test.yaml",
		},
		{
			name: "failed runs",
			results: []engine.RunResult{
				{RunID: "run-001", Error: "timeout error"},
				{RunID: "run-002", Error: "connection error"},
			},
			successCount: 0,
			errorCount:   2,
			configFile:   "test.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := createResultSummary(tt.results, tt.successCount, tt.errorCount, tt.configFile)
			assert.NotNil(t, result)
		})
	}
}

func TestDisplayRunInfo(t *testing.T) {
	params := &RunParameters{
		Regions:      []string{"us-east-1"},
		Providers:    []string{"openai"},
		Scenarios:    []string{"scenario1"},
		Concurrency:  4,
		OutDir:       "output",
		CIMode:       false,
		Verbose:      true,
		MockProvider: true,
		JUnitFile:    "junit.xml",
		HTMLFile:     "report.html",
	}

	configFile := "test.yaml"

	// This function mainly prints to stdout, so we just test it doesn't panic
	t.Run("displays run info without error", func(t *testing.T) {
		assert.NotPanics(t, func() {
			displayRunInfo(params, configFile)
		})
	})
}
