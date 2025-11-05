package markdown

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	assert.True(t, repo.includeDetails)
}

func TestNewMarkdownResultRepositoryWithFile(t *testing.T) {
	tmpDir := t.TempDir()
	customFile := filepath.Join(tmpDir, "custom-results.md")
	repo := NewMarkdownResultRepositoryWithFile(customFile)

	assert.Equal(t, tmpDir, repo.outputDir)
	assert.Equal(t, customFile, repo.outputFile)
	assert.True(t, repo.includeDetails)
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
	assert.True(t, repo.includeDetails)

	// Set to false
	repo.SetIncludeDetails(false)
	assert.False(t, repo.includeDetails)

	// Set back to true
	repo.SetIncludeDetails(true)
	assert.True(t, repo.includeDetails)
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
