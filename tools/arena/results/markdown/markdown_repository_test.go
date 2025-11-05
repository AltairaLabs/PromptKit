package markdown

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMarkdownResultRepository(t *testing.T) {
	tmpDir := t.TempDir()
	repo := NewMarkdownResultRepository(tmpDir)

	assert.Equal(t, tmpDir, repo.outputDir)
	assert.Equal(t, filepath.Join(tmpDir, "results.md"), repo.outputFile)
	assert.True(t, repo.config.IncludeDetails)
}

func TestNewMarkdownResultRepositoryWithFile(t *testing.T) {
	tmpDir := t.TempDir()
	customFile := filepath.Join(tmpDir, "custom-results.md")
	repo := NewMarkdownResultRepositoryWithFile(customFile)

	assert.Equal(t, tmpDir, repo.outputDir)
	assert.Equal(t, customFile, repo.outputFile)
	assert.True(t, repo.config.IncludeDetails)
}

func TestSaveResults_EmptyResults(t *testing.T) {
	tmpDir := t.TempDir()
	repo := NewMarkdownResultRepository(tmpDir)

	err := repo.SaveResults([]engine.RunResult{})
	require.NoError(t, err)

	// Check that file was created
	_, err = os.Stat(repo.GetOutputFile())
	assert.NoError(t, err)

	// Check basic content exists
	content, err := os.ReadFile(repo.GetOutputFile())
	require.NoError(t, err)
	assert.Contains(t, string(content), "PromptArena Evaluation Results")
}

func TestSetIncludeDetails(t *testing.T) {
	tmpDir := t.TempDir()
	repo := NewMarkdownResultRepository(tmpDir)

	// Default is true
	assert.True(t, repo.config.IncludeDetails)

	// Set to false
	repo.SetIncludeDetails(false)
	assert.False(t, repo.config.IncludeDetails)

	// Set back to true
	repo.SetIncludeDetails(true)
	assert.True(t, repo.config.IncludeDetails)
}

func TestUnsupportedOperations(t *testing.T) {
	tmpDir := t.TempDir()
	repo := NewMarkdownResultRepository(tmpDir)

	// Test LoadResults
	results, err := repo.LoadResults()
	assert.Error(t, err)
	assert.Nil(t, results)
	assert.Contains(t, err.Error(), "does not support loading")

	// Test SupportsStreaming
	assert.False(t, repo.SupportsStreaming())

	// Test SaveResult
	err = repo.SaveResult(&engine.RunResult{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not support streaming")
}

// Helper function to create test results
func createTestResults() []engine.RunResult {
	return []engine.RunResult{
		createSuccessfulResult("run-001", "scenario-1", "gpt-4"),
		createFailedResult("run-002", "scenario-2", "claude"),
		createResultWithAssertions("run-003", "scenario-1", "gemini"),
		createResultWithTools("run-004", "scenario-3", "gpt-4"),
		createResultWithViolations("run-005", "scenario-2", "claude"),
	}
}

// Helper function to create test results with specific counts
func createTestResultsWithCounts(total, passed, failed int) []engine.RunResult {
	if total < passed+failed {
		panic("total must be >= passed + failed")
	}

	results := make([]engine.RunResult, 0, total)

	// Add passed results
	for i := 0; i < passed; i++ {
		results = append(results, createSuccessfulResult(fmt.Sprintf("run-%03d", i+1), "scenario-1", "gpt-4"))
	}

	// Add failed results
	for i := 0; i < failed; i++ {
		results = append(results, createFailedResult(fmt.Sprintf("run-%03d", passed+i+1), "scenario-2", "claude"))
	}

	// Add remaining results as passed (to reach total)
	remaining := total - passed - failed
	for i := 0; i < remaining; i++ {
		results = append(results, createSuccessfulResult(fmt.Sprintf("run-%03d", passed+failed+i+1), "scenario-3", "gemini"))
	}

	return results
}

func createSuccessfulResult(runID, scenario, provider string) engine.RunResult {
	return engine.RunResult{
		RunID:      runID,
		ScenarioID: scenario,
		ProviderID: provider,
		Region:     "us-east-1",
		Duration:   time.Millisecond * 1500,
		Cost: types.CostInfo{
			TotalCost:     0.0123,
			InputTokens:   100,
			OutputTokens:  50,
			InputCostUSD:  0.0073,
			OutputCostUSD: 0.0050,
		},
		Messages: []types.Message{
			{Role: "user", Content: "Test question"},
			{Role: "assistant", Content: "Test response"},
		},
		Error:      "",
		Violations: []types.ValidationError{},
	}
}

func createFailedResult(runID, scenario, provider string) engine.RunResult {
	result := createSuccessfulResult(runID, scenario, provider)
	result.Error = "execution failed: timeout"
	return result
}

func createResultWithAssertions(runID, scenario, provider string) engine.RunResult {
	result := createSuccessfulResult(runID, scenario, provider)

	// Add assertion results to message metadata
	result.Messages[1].Meta = map[string]interface{}{
		"assertions": map[string]interface{}{
			"content_includes": map[string]interface{}{
				"passed":  true,
				"message": "Content should include required terms",
				"details": map[string]interface{}{"patterns": []string{"test"}},
			},
			"length_check": map[string]interface{}{
				"passed":  false,
				"message": "Response too long",
				"details": map[string]interface{}{"max_length": 100, "actual": 150},
			},
		},
	}

	return result
}

func createResultWithTools(runID, scenario, provider string) engine.RunResult {
	result := createSuccessfulResult(runID, scenario, provider)
	result.ToolStats = &types.ToolStats{
		TotalCalls: 3,
		ByTool: map[string]int{
			"search":    2,
			"calculate": 1,
		},
	}
	return result
}

func createResultWithViolations(runID, scenario, provider string) engine.RunResult {
	result := createSuccessfulResult(runID, scenario, provider)
	result.Violations = []types.ValidationError{
		{
			Type:   "banned_words",
			Tool:   "validator",
			Detail: "Found banned word: guaranteed",
		},
		{
			Type:   "max_length",
			Tool:   "validator",
			Detail: "Response exceeds maximum length",
		},
	}
	return result
}

func TestSaveResults_WithData(t *testing.T) {
	tmpDir := t.TempDir()
	repo := NewMarkdownResultRepository(tmpDir)

	testResults := createTestResults()
	err := repo.SaveResults(testResults)
	require.NoError(t, err)

	// Check that file was created
	content, err := os.ReadFile(repo.GetOutputFile())
	require.NoError(t, err)

	contentStr := string(content)

	// Check main sections exist
	assert.Contains(t, contentStr, "# 🧪 PromptArena Test Results")
	assert.Contains(t, contentStr, "## 📊 Overview")
	assert.Contains(t, contentStr, "## 🔍 Test Results")
	assert.Contains(t, contentStr, "## 🔍 Failed Tests")
	assert.Contains(t, contentStr, "## 💰 Cost Breakdown")

	// Check overview metrics
	assert.Contains(t, contentStr, "| Tests Run | 5 |")
	assert.Contains(t, contentStr, "| Passed | 2 ✅ |")
	assert.Contains(t, contentStr, "| Failed | 3 ❌ |")

	// Check provider names appear
	assert.Contains(t, contentStr, "gpt-4")
	assert.Contains(t, contentStr, "claude")
	assert.Contains(t, contentStr, "gemini")

	// Check scenario names appear
	assert.Contains(t, contentStr, "scenario-1")
	assert.Contains(t, contentStr, "scenario-2")
}

func TestCalculateSummary(t *testing.T) {
	repo := NewMarkdownResultRepository(t.TempDir())
	testResults := createTestResults()

	summary := repo.calculateSummary(testResults)

	assert.Equal(t, 5, summary.Total)
	assert.Equal(t, 2, summary.Passed) // Only run-001 and run-004 should pass
	assert.Equal(t, 3, summary.Failed) // run-002 (error), run-003 (assertion failure), run-005 (violations)
	assert.InDelta(t, 0.0615, summary.TotalCost, 0.001)
	assert.Equal(t, 750, summary.TotalTokens) // 5 results * 150 tokens each
}

func TestHasFailedAssertions(t *testing.T) {
	repo := NewMarkdownResultRepository(t.TempDir())

	// Test result with no assertions
	noAssertions := createSuccessfulResult("test", "scenario", "provider")
	assert.False(t, repo.hasFailedAssertions(&noAssertions))

	// Test result with passing assertions
	passingAssertions := createSuccessfulResult("test", "scenario", "provider")
	passingAssertions.Messages[1].Meta = map[string]interface{}{
		"assertions": map[string]interface{}{
			"test": map[string]interface{}{
				"passed": true,
			},
		},
	}
	assert.False(t, repo.hasFailedAssertions(&passingAssertions))

	// Test result with failed assertions
	failedAssertions := createResultWithAssertions("test", "scenario", "provider")
	assert.True(t, repo.hasFailedAssertions(&failedAssertions))
}

func TestCountAssertions(t *testing.T) {
	repo := NewMarkdownResultRepository(t.TempDir())

	// Test result with no assertions
	noAssertions := createSuccessfulResult("test", "scenario", "provider")
	assert.Equal(t, 0, repo.countAssertions(&noAssertions))

	// Test result with assertions
	withAssertions := createResultWithAssertions("test", "scenario", "provider")
	assert.Equal(t, 2, repo.countAssertions(&withAssertions))
}

func TestWriteOverviewSection(t *testing.T) {
	repo := NewMarkdownResultRepository(t.TempDir())
	var content strings.Builder

	summary := testSummary{
		Total:       10,
		Passed:      8,
		Failed:      2,
		TotalCost:   1.234,
		TotalTokens: 5000,
		Duration:    time.Second * 30,
	}

	repo.writeOverviewSection(&content, summary)
	result := content.String()

	assert.Contains(t, result, "## 📊 Overview")
	assert.Contains(t, result, "| Tests Run | 10 |")
	assert.Contains(t, result, "| Passed | 8 ✅ |")
	assert.Contains(t, result, "| Failed | 2 ❌ |")
	assert.Contains(t, result, "| Success Rate | 80.0% |")
	assert.Contains(t, result, "| Total Cost | $1.2340 |")
}

func TestWriteResultsMatrix(t *testing.T) {
	repo := NewMarkdownResultRepository(t.TempDir())
	var content strings.Builder

	testResults := []engine.RunResult{createSuccessfulResult("test", "scenario", "provider")}

	repo.writeResultsMatrix(&content, testResults)
	result := content.String()

	assert.Contains(t, result, "## 🔍 Test Results")
	assert.Contains(t, result, "| Provider | Scenario | Region | Status | Duration | Guardrails | Assertions | Tools | Cost |")
	assert.Contains(t, result, "| provider | scenario | us-east-1 | ✅ Pass |")
}

func TestWriteFailedTestsSection(t *testing.T) {
	repo := NewMarkdownResultRepository(t.TempDir())
	var content strings.Builder

	testResults := []engine.RunResult{
		createFailedResult("fail-001", "scenario-1", "provider-1"),
		createResultWithAssertions("fail-002", "scenario-2", "provider-2"),
	}

	repo.writeFailedTestsSection(&content, testResults)
	result := content.String()

	assert.Contains(t, result, "## 🔍 Failed Tests")
	assert.Contains(t, result, "### ❌ scenario-1 → provider-1")
	assert.Contains(t, result, "### ❌ scenario-2 → provider-2")
	assert.Contains(t, result, "execution failed: timeout")
	assert.Contains(t, result, "**Assertion Failures**:")
}

func TestWriteCostSection(t *testing.T) {
	repo := NewMarkdownResultRepository(t.TempDir())
	var content strings.Builder

	testResults := createTestResults()
	summary := repo.calculateSummary(testResults)

	repo.writeCostSection(&content, testResults, summary)
	result := content.String()

	assert.Contains(t, result, "## 💰 Cost Breakdown")
	assert.Contains(t, result, "| Provider | Total Cost | Runs | Avg Cost |")
	assert.Contains(t, result, "| gpt-4 |")
	assert.Contains(t, result, "| claude |")
	assert.Contains(t, result, "| gemini |")
}

func TestAssertionFailureExtraction(t *testing.T) {
	repo := NewMarkdownResultRepository(t.TempDir())

	result := createResultWithAssertions("test", "scenario", "provider")
	failures := repo.collectAssertionFailures(&result)

	assert.Len(t, failures, 1) // Only the failed assertion
	assert.Equal(t, "length_check", failures[0].Type)
	assert.Equal(t, "Response too long", failures[0].Message)
	assert.Contains(t, failures[0].Details, "max_length")
}

func TestSaveSummary(t *testing.T) {
	repo := NewMarkdownResultRepository(t.TempDir())

	// SaveSummary should not return error (it's a no-op for markdown)
	err := repo.SaveSummary(nil)
	assert.NoError(t, err)
}

// TDD Tests for Markdown Configuration Support

func TestNewMarkdownResultRepository_WithConfig(t *testing.T) {
	tmpDir := t.TempDir()

	// Test with nil config (should use defaults)
	repo := NewMarkdownResultRepositoryWithConfig(tmpDir, nil)
	assert.Equal(t, tmpDir, repo.outputDir)
	assert.True(t, repo.config.IncludeDetails)
	assert.True(t, repo.config.ShowOverview)
	assert.True(t, repo.config.ShowResultsMatrix)
	assert.True(t, repo.config.ShowFailedTests)
	assert.True(t, repo.config.ShowCostSummary)

	// Test with custom config
	customConfig := &MarkdownConfig{
		IncludeDetails:    false,
		ShowOverview:      false,
		ShowResultsMatrix: true,
		ShowFailedTests:   true,
		ShowCostSummary:   false,
	}

	repo = NewMarkdownResultRepositoryWithConfig(tmpDir, customConfig)
	assert.Equal(t, customConfig, repo.config)
}

func TestCreateMarkdownConfigFromDefaults(t *testing.T) {
	tests := []struct {
		name     string
		defaults *config.Defaults
		expected *MarkdownConfig
	}{
		{
			name:     "nil defaults should return default config",
			defaults: nil,
			expected: &MarkdownConfig{
				IncludeDetails:    true,
				ShowOverview:      true,
				ShowResultsMatrix: true,
				ShowFailedTests:   true,
				ShowCostSummary:   true,
			},
		},
		{
			name: "custom markdown config from new output structure",
			defaults: &config.Defaults{
				Output: config.OutputConfig{
					Dir:     "test-output",
					Formats: []string{"markdown"},
					Markdown: &config.MarkdownOutputConfig{
						IncludeDetails:    false,
						ShowOverview:      false,
						ShowResultsMatrix: true,
						ShowFailedTests:   true,
						ShowCostSummary:   false,
					},
				},
			},
			expected: &MarkdownConfig{
				IncludeDetails:    false,
				ShowOverview:      false,
				ShowResultsMatrix: true,
				ShowFailedTests:   true,
				ShowCostSummary:   false,
			},
		},
		{
			name: "backward compatibility with old MarkdownConfig",
			defaults: &config.Defaults{
				MarkdownConfig: &config.MarkdownConfig{
					IncludeDetails:    false,
					ShowOverview:      false,
					ShowResultsMatrix: true,
					ShowFailedTests:   true,
					ShowCostSummary:   false,
				},
			},
			expected: &MarkdownConfig{
				IncludeDetails:    false,
				ShowOverview:      false,
				ShowResultsMatrix: true,
				ShowFailedTests:   true,
				ShowCostSummary:   false,
			},
		},
		{
			name: "defaults without markdown config should return default config",
			defaults: &config.Defaults{
				Temperature: 0.7,
				MaxTokens:   1000,
			},
			expected: &MarkdownConfig{
				IncludeDetails:    true,
				ShowOverview:      true,
				ShowResultsMatrix: true,
				ShowFailedTests:   true,
				ShowCostSummary:   true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := CreateMarkdownConfigFromDefaults(tt.defaults)
			assert.Equal(t, tt.expected, config)
		})
	}
}

func TestSaveResults_WithConfiguredSections(t *testing.T) {
	tmpDir := t.TempDir()

	// Create config that hides some sections
	customConfig := &MarkdownConfig{
		IncludeDetails:    false,
		ShowOverview:      true,
		ShowResultsMatrix: false,
		ShowFailedTests:   true,
		ShowCostSummary:   false,
	}

	repo := NewMarkdownResultRepositoryWithConfig(tmpDir, customConfig)

	// Create test results with failures
	results := createTestResultsWithCounts(3, 1, 2) // 3 total, 1 passed, 2 failed

	err := repo.SaveResults(results)
	require.NoError(t, err)

	// Read the generated file
	content, err := os.ReadFile(repo.outputFile)
	require.NoError(t, err)
	markdown := string(content)

	// Should include overview
	assert.Contains(t, markdown, "# 🧪 PromptArena Test Results")
	assert.Contains(t, markdown, "## 📊 Overview")

	// Should NOT include results matrix (ShowResultsMatrix = false)
	assert.NotContains(t, markdown, "## 🔍 Test Results")

	// Should include failed tests
	assert.Contains(t, markdown, "## 🔍 Failed Tests")

	// Should NOT include cost summary (ShowCostSummary = false)
	assert.NotContains(t, markdown, "## 💰 Cost Breakdown")

	// Should NOT include details (IncludeDetails = false) - this would be implemented in failed tests section
	// For now just verify that our sections are working as expected
}

func TestConfigurationIntegration(t *testing.T) {
	// Test that we can load configuration from arena YAML
	tmpDir := t.TempDir()

	// Create a test arena.yaml with new modular output config
	arenaConfig := `apiVersion: "promptkit.ai/v1"
kind: "Arena"
metadata:
  name: "test-config"
spec:
  providers: []
  scenarios: []
  defaults:
    temperature: 0.7
    max_tokens: 1000
    output:
      dir: "test-output"
      formats: ["json", "markdown"]
      markdown:
        include_details: false
        show_overview: true
        show_results_matrix: false
        show_failed_tests: true
        show_cost_summary: false`

	configFile := filepath.Join(tmpDir, "arena.yaml")
	err := os.WriteFile(configFile, []byte(arenaConfig), 0644)
	require.NoError(t, err)

	// Load the config
	cfg, err := config.LoadConfig(configFile)
	require.NoError(t, err)

	// Create markdown config from defaults
	markdownConfig := CreateMarkdownConfigFromDefaults(&cfg.Defaults)

	// Verify the configuration was loaded correctly
	assert.False(t, markdownConfig.IncludeDetails)
	assert.True(t, markdownConfig.ShowOverview)
	assert.False(t, markdownConfig.ShowResultsMatrix)
	assert.True(t, markdownConfig.ShowFailedTests)
	assert.False(t, markdownConfig.ShowCostSummary)

	// Test with repository
	repo := NewMarkdownResultRepositoryWithConfig(tmpDir, markdownConfig)
	assert.Equal(t, markdownConfig, repo.config)
}

func TestSetOutputFile(t *testing.T) {
	tmpDir := t.TempDir()
	repo := NewMarkdownResultRepository(tmpDir)

	// Test setting output file
	testFile := "custom-output.md"
	repo.SetOutputFile(testFile)

	// Verify the file was set
	assert.Equal(t, testFile, repo.GetOutputFile())
}
