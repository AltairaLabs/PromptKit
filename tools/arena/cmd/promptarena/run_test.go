package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

func TestExtractRunIDs(t *testing.T) {
	tests := []struct {
		name     string
		results  []engine.RunResult
		expected []string
	}{
		{
			name:     "empty results",
			results:  []engine.RunResult{},
			expected: []string{},
		},
		{
			name: "single result",
			results: []engine.RunResult{
				{RunID: "run-001"},
			},
			expected: []string{"run-001"},
		},
		{
			name: "multiple results",
			results: []engine.RunResult{
				{RunID: "run-001"},
				{RunID: "run-002"},
				{RunID: "run-003"},
			},
			expected: []string{"run-001", "run-002", "run-003"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractRunIDs(tt.results)
			assert.Equal(t, tt.expected, result)
		})
	}
}

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

func TestSaveResult(t *testing.T) {
	now := time.Now()
	duration := 5 * time.Second

	tests := []struct {
		name        string
		result      engine.RunResult
		expectError bool
	}{
		{
			name: "valid result",
			result: engine.RunResult{
				RunID:      "test-run-001",
				PromptPack: "test-pack",
				Region:     "us-east-1",
				ScenarioID: "scenario-1",
				ProviderID: "openai",
				Messages:   []types.Message{{Role: "user", Content: "test"}},
				StartTime:  now,
				EndTime:    now.Add(duration),
				Duration:   duration,
			},
			expectError: false,
		},
		{
			name: "minimal result",
			result: engine.RunResult{
				RunID:  "test-run-002",
				Region: "us-west-2",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp file
			tmpDir := t.TempDir()
			filename := filepath.Join(tmpDir, "result.json")

			// Save result
			err := saveResult(&tt.result, filename)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)

				// Verify file exists and contains valid JSON
				data, err := os.ReadFile(filename)
				require.NoError(t, err)

				var loaded engine.RunResult
				err = json.Unmarshal(data, &loaded)
				require.NoError(t, err)

				assert.Equal(t, tt.result.RunID, loaded.RunID)
				assert.Equal(t, tt.result.Region, loaded.Region)
			}
		})
	}
}

func TestSaveJSON(t *testing.T) {
	tests := []struct {
		name        string
		data        interface{}
		expectError bool
	}{
		{
			name: "simple map",
			data: map[string]interface{}{
				"name":    "test",
				"value":   42,
				"enabled": true,
			},
			expectError: false,
		},
		{
			name:        "slice of strings",
			data:        []string{"a", "b", "c"},
			expectError: false,
		},
		{
			name: "nested structure",
			data: map[string]interface{}{
				"config": map[string]interface{}{
					"region": "us-east-1",
					"items":  []int{1, 2, 3},
				},
			},
			expectError: false,
		},
		{
			name:        "empty map",
			data:        map[string]interface{}{},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp file
			tmpDir := t.TempDir()
			filename := filepath.Join(tmpDir, "data.json")

			// Save JSON
			err := saveJSON(tt.data, filename)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)

				// Verify file exists and contains valid JSON
				data, err := os.ReadFile(filename)
				require.NoError(t, err)

				var loaded interface{}
				err = json.Unmarshal(data, &loaded)
				require.NoError(t, err)
				assert.NotNil(t, loaded)
			}
		})
	}
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

func TestCreateSummary(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name         string
		results      []engine.RunResult
		successCount int
		errorCount   int
		configFile   string
		expectedKeys []string
	}{
		{
			name:         "empty results",
			results:      []engine.RunResult{},
			successCount: 0,
			errorCount:   0,
			configFile:   "test.yaml",
			expectedKeys: []string{"total_runs", "successful", "errors", "timestamp", "config_file", "run_ids"},
		},
		{
			name: "with results",
			results: []engine.RunResult{
				{RunID: "run-001"},
				{RunID: "run-002"},
			},
			successCount: 2,
			errorCount:   0,
			configFile:   "arena.yaml",
			expectedKeys: []string{"total_runs", "successful", "errors", "timestamp", "config_file", "run_ids"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := createSummary(tt.results, tt.successCount, tt.errorCount, tt.configFile)

			// Check all expected keys exist
			for _, key := range tt.expectedKeys {
				assert.Contains(t, summary, key)
			}

			// Check specific values
			assert.Equal(t, len(tt.results), summary["total_runs"])
			assert.Equal(t, tt.successCount, summary["successful"])
			assert.Equal(t, tt.errorCount, summary["errors"])
			assert.Equal(t, tt.configFile, summary["config_file"])

			// Check run_ids
			runIDs := summary["run_ids"].([]string)
			assert.Equal(t, len(tt.results), len(runIDs))

			// Check timestamp is recent (within last minute)
			timestamp, ok := summary["timestamp"].(time.Time)
			assert.True(t, ok)
			assert.WithinDuration(t, now, timestamp, time.Minute)
		})
	}
}

func TestResolveHTMLReportPath(t *testing.T) {
	tests := []struct {
		name           string
		outDir         string
		htmlReportPath string
		expectedPrefix string // We'll check if the result starts with this
	}{
		{
			name:           "empty path generates timestamped file",
			outDir:         "/tmp/output",
			htmlReportPath: "",
			expectedPrefix: "/tmp/output/report-",
		},
		{
			name:           "custom path gets resolved",
			outDir:         "/tmp/output",
			htmlReportPath: "custom-report.html",
			expectedPrefix: "/tmp/output/custom-report.html",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveHTMLReportPath(tt.outDir, tt.htmlReportPath)
			assert.True(t, strings.HasPrefix(result, tt.expectedPrefix))
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
