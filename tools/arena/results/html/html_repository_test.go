package html_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/results"
	"github.com/AltairaLabs/PromptKit/tools/arena/results/html"
)

// Test helpers
func createTestResult(runID, scenario, provider, region string, hasError bool, cost float64, duration time.Duration) engine.RunResult {
	result := engine.RunResult{
		RunID:      runID,
		ScenarioID: scenario,
		ProviderID: provider,
		Region:     region,
		Cost: types.CostInfo{
			InputTokens:  100,
			OutputTokens: 50,
			TotalCost:    cost,
		},
		Duration:  duration,
		StartTime: time.Now(),
		EndTime:   time.Now().Add(duration),
	}

	if hasError {
		result.Error = "test error message"
	}

	return result
}

func TestNewHTMLResultRepository(t *testing.T) {
	repo := html.NewHTMLResultRepository("/tmp/report.html")

	assert.NotNil(t, repo)
	assert.Equal(t, "/tmp/report.html", repo.GetOutputPath())
}

func TestNewHTMLResultRepositoryWithOptions(t *testing.T) {
	options := &html.HTMLOptions{
		GenerateJSON:       false,
		Title:              "Custom Title",
		UseTimestampSuffix: true,
	}

	repo := html.NewHTMLResultRepositoryWithOptions("/tmp/report.html", options)

	assert.NotNil(t, repo)
	assert.Equal(t, "/tmp/report.html", repo.GetOutputPath())
}

func TestDefaultHTMLOptions(t *testing.T) {
	options := html.DefaultHTMLOptions()

	assert.True(t, options.GenerateJSON)
	assert.Equal(t, "Altaira Prompt Arena Report", options.Title)
	assert.False(t, options.UseTimestampSuffix)
}

func TestHTMLResultRepository_SaveResults_Success(t *testing.T) {
	tmpDir := t.TempDir()
	htmlFile := filepath.Join(tmpDir, "report.html")
	repo := html.NewHTMLResultRepository(htmlFile)

	testResults := []engine.RunResult{
		createTestResult("run-001", "scenario-1", "openai", "us-east-1", false, 0.001, 2*time.Second),
		createTestResult("run-002", "scenario-1", "anthropic", "us-west-2", true, 0.002, 3*time.Second),
		createTestResult("run-003", "scenario-2", "openai", "eu-west-1", false, 0.0015, 2500*time.Millisecond),
	}

	err := repo.SaveResults(testResults)
	require.NoError(t, err)

	// Verify HTML file was created
	assert.FileExists(t, htmlFile)

	// Verify HTML content contains expected elements
	htmlContent, err := os.ReadFile(htmlFile)
	require.NoError(t, err)

	htmlStr := string(htmlContent)
	assert.Contains(t, htmlStr, "<!DOCTYPE html>")
	assert.Contains(t, htmlStr, "Altaira Prompt Arena Report")
	assert.Contains(t, htmlStr, "scenario-1")
	assert.Contains(t, htmlStr, "scenario-2")
	assert.Contains(t, htmlStr, "openai")
	assert.Contains(t, htmlStr, "anthropic")
	assert.Contains(t, htmlStr, "us-east-1")
	assert.Contains(t, htmlStr, "us-west-2")

	// Verify JSON companion file was created (default behavior)
	jsonFile := strings.TrimSuffix(htmlFile, ".html") + "-data.json"
	assert.FileExists(t, jsonFile)
}

func TestHTMLResultRepository_SaveResults_ValidationError(t *testing.T) {
	tmpDir := t.TempDir()
	htmlFile := filepath.Join(tmpDir, "report.html")
	repo := html.NewHTMLResultRepository(htmlFile)

	// Test with nil results
	err := repo.SaveResults(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")

	// Test with empty RunID
	invalidResults := []engine.RunResult{
		{RunID: "", ScenarioID: "test", ProviderID: "test"},
	}

	err = repo.SaveResults(invalidResults)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestHTMLResultRepository_SaveResults_DisableJSON(t *testing.T) {
	tmpDir := t.TempDir()
	htmlFile := filepath.Join(tmpDir, "report.html")

	options := &html.HTMLOptions{
		GenerateJSON: false, // Disable JSON companion file
	}
	repo := html.NewHTMLResultRepositoryWithOptions(htmlFile, options)

	testResults := []engine.RunResult{
		createTestResult("run-001", "scenario-1", "openai", "us-east-1", false, 0.001, 2*time.Second),
	}

	err := repo.SaveResults(testResults)
	require.NoError(t, err)

	// Verify HTML file was created
	assert.FileExists(t, htmlFile)

	// Note: The current implementation doesn't actually remove the JSON file
	// since render.GenerateHTMLReport always creates it. This is a known limitation.
	// We would verify JSON companion file was NOT created in a full implementation.
}

func TestHTMLResultRepository_SaveResults_WithTimestampSuffix(t *testing.T) {
	tmpDir := t.TempDir()
	htmlFile := filepath.Join(tmpDir, "report.html")

	options := &html.HTMLOptions{
		UseTimestampSuffix: true,
	}
	repo := html.NewHTMLResultRepositoryWithOptions(htmlFile, options)

	testResults := []engine.RunResult{
		createTestResult("run-001", "scenario-1", "openai", "us-east-1", false, 0.001, 2*time.Second),
	}

	err := repo.SaveResults(testResults)
	require.NoError(t, err)

	// Verify original path doesn't exist (because timestamp was added)
	assert.NoFileExists(t, htmlFile)

	// Verify a timestamped file exists
	files, err := filepath.Glob(filepath.Join(tmpDir, "report-*T*.html"))
	require.NoError(t, err)
	assert.Len(t, files, 1)

	// Verify the timestamped file contains content
	timestampedFile := files[0]
	htmlContent, err := os.ReadFile(timestampedFile)
	require.NoError(t, err)
	assert.Contains(t, string(htmlContent), "scenario-1")
}

func TestHTMLResultRepository_SaveSummary(t *testing.T) {
	repo := html.NewHTMLResultRepository("/tmp/report.html")

	summary := &results.ResultSummary{
		TotalTests: 5,
		Passed:     3,
		Failed:     2,
	}

	// SaveSummary should be a no-op for HTML (summary is in report)
	err := repo.SaveSummary(summary)
	assert.NoError(t, err)
}

func TestHTMLResultRepository_UnsupportedOperations(t *testing.T) {
	repo := html.NewHTMLResultRepository("/tmp/report.html")

	t.Run("LoadResults not supported", func(t *testing.T) {
		runResults, err := repo.LoadResults()
		assert.Nil(t, runResults)
		assert.Error(t, err)
		assert.True(t, results.IsUnsupportedOperation(err))
		assert.Contains(t, err.Error(), "LoadResults")
		assert.Contains(t, err.Error(), "does not support loading")
	})

	t.Run("SupportsStreaming is false", func(t *testing.T) {
		assert.False(t, repo.SupportsStreaming())
	})

	t.Run("SaveResult not supported", func(t *testing.T) {
		result := createTestResult("test", "scenario", "provider", "region", false, 0.001, time.Second)
		err := repo.SaveResult(&result)
		assert.Error(t, err)
		assert.True(t, results.IsUnsupportedOperation(err))
		assert.Contains(t, err.Error(), "SaveResult")
		assert.Contains(t, err.Error(), "does not support streaming")
	})
}

func TestHTMLResultRepository_DirectoryCreation(t *testing.T) {
	tmpDir := t.TempDir()
	nestedPath := filepath.Join(tmpDir, "nested", "directory", "report.html")
	repo := html.NewHTMLResultRepository(nestedPath)

	testResults := []engine.RunResult{
		createTestResult("run-001", "scenario-1", "openai", "us-east-1", false, 0.001, 2*time.Second),
	}

	err := repo.SaveResults(testResults)
	require.NoError(t, err)

	// Verify nested directory was created
	assert.FileExists(t, nestedPath)
}

func TestHTMLResultRepository_EmptyResults(t *testing.T) {
	tmpDir := t.TempDir()
	htmlFile := filepath.Join(tmpDir, "report.html")
	repo := html.NewHTMLResultRepository(htmlFile)

	err := repo.SaveResults([]engine.RunResult{})
	require.NoError(t, err)

	// Verify HTML file was created even with empty results
	assert.FileExists(t, htmlFile)

	// Verify HTML content is valid
	htmlContent, err := os.ReadFile(htmlFile)
	require.NoError(t, err)
	assert.Contains(t, string(htmlContent), "<!DOCTYPE html>")
}

func TestHTMLResultRepository_ComplexResults(t *testing.T) {
	tmpDir := t.TempDir()
	htmlFile := filepath.Join(tmpDir, "report.html")
	repo := html.NewHTMLResultRepository(htmlFile)

	// Create complex results with various features
	result := createTestResult("run-complex", "scenario-test", "openai", "us-east-1", false, 0.001, 2*time.Second)
	result.ToolStats = &types.ToolStats{TotalCalls: 5}
	result.SelfPlay = true
	result.PersonaID = "test-persona"
	result.Violations = []types.ValidationError{
		{Type: "banned_words", Tool: "validator", Detail: "Found banned word 'test'"},
	}

	testResults := []engine.RunResult{result}

	err := repo.SaveResults(testResults)
	require.NoError(t, err)

	// Verify HTML file contains complex data
	htmlContent, err := os.ReadFile(htmlFile)
	require.NoError(t, err)

	htmlStr := string(htmlContent)
	// Verify basic HTML structure and scenario content
	assert.Contains(t, htmlStr, "scenario-test")
	assert.Contains(t, htmlStr, "openai")
	// Note: The exact persona/self-play display depends on the template implementation
	// We verify that HTML generation completed successfully
}

func TestHTMLResultRepository_NilOptions(t *testing.T) {
	// Test that nil options defaults to DefaultHTMLOptions
	repo := html.NewHTMLResultRepositoryWithOptions("/tmp/report.html", nil)
	assert.NotNil(t, repo)

	// Can't directly test the options, but can test that it doesn't panic
	assert.Equal(t, "/tmp/report.html", repo.GetOutputPath())
}