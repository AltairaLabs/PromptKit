package json_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/results"
	jsonrepo "github.com/AltairaLabs/PromptKit/tools/arena/results/json"
)

// Test helpers
func createTestResult(runID, scenario, provider string, hasError bool, cost float64) engine.RunResult {
	result := engine.RunResult{
		RunID:      runID,
		ScenarioID: scenario,
		ProviderID: provider,
		Region:     "us-east-1",
		Cost: types.CostInfo{
			InputTokens:  100,
			OutputTokens: 50,
			TotalCost:    cost,
		},
		Duration:  2 * time.Second,
		StartTime: time.Now(),
		EndTime:   time.Now().Add(2 * time.Second),
	}

	if hasError {
		result.Error = "test error"
	}

	return result
}

func createTestSummary() *results.ResultSummary {
	return &results.ResultSummary{
		TotalTests:    3,
		Passed:        2,
		Failed:        1,
		TotalCost:     0.006,
		AverageCost:   0.002,
		TotalTokens:   450,
		TotalDuration: 6 * time.Second,
		Timestamp:     time.Date(2025, 11, 4, 12, 0, 0, 0, time.UTC),
		ConfigFile:    "test-config.yaml",
		RunIDs:        []string{"run-001", "run-002", "run-003"},
		Scenarios:     []string{"scenario-1", "scenario-2"},
		Providers:     []string{"openai", "anthropic"},
		Regions:       []string{"us-east-1"},
		PromptPacks:   []string{"pack-1"},
	}
}

func TestNewJSONResultRepository(t *testing.T) {
	repo := jsonrepo.NewJSONResultRepository("/tmp/test")

	assert.NotNil(t, repo)
	assert.Equal(t, "/tmp/test", repo.GetOutputDir())
}

func TestJSONResultRepository_SaveResults(t *testing.T) {
	tmpDir := t.TempDir()
	repo := jsonrepo.NewJSONResultRepository(tmpDir)

	testResults := []engine.RunResult{
		createTestResult("run-001", "scenario-1", "openai", false, 0.001),
		createTestResult("run-002", "scenario-1", "anthropic", true, 0.002),
		createTestResult("run-003", "scenario-2", "openai", false, 0.003),
	}

	err := repo.SaveResults(testResults)
	require.NoError(t, err)

	// Verify individual result files were created
	for _, result := range testResults {
		filename := filepath.Join(tmpDir, result.RunID+".json")
		assert.FileExists(t, filename)

		// Verify file content
		data, err := os.ReadFile(filename)
		require.NoError(t, err)

		var savedResult engine.RunResult
		err = json.Unmarshal(data, &savedResult)
		require.NoError(t, err)

		assert.Equal(t, result.RunID, savedResult.RunID)
		assert.Equal(t, result.ScenarioID, savedResult.ScenarioID)
		assert.Equal(t, result.ProviderID, savedResult.ProviderID)
		assert.Equal(t, result.Error, savedResult.Error)
	}
}

func TestJSONResultRepository_SaveResults_ValidationError(t *testing.T) {
	tmpDir := t.TempDir()
	repo := jsonrepo.NewJSONResultRepository(tmpDir)

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

func TestJSONResultRepository_SaveSummary(t *testing.T) {
	tmpDir := t.TempDir()
	repo := jsonrepo.NewJSONResultRepository(tmpDir)

	summary := createTestSummary()
	err := repo.SaveSummary(summary)
	require.NoError(t, err)

	// Verify index.json was created
	indexFile := filepath.Join(tmpDir, "index.json")
	assert.FileExists(t, indexFile)

	// Verify index content
	data, err := os.ReadFile(indexFile)
	require.NoError(t, err)

	var savedIndex map[string]interface{}
	err = json.Unmarshal(data, &savedIndex)
	require.NoError(t, err)

	// Check legacy format fields
	assert.Equal(t, float64(3), savedIndex["total_runs"])
	assert.Equal(t, float64(2), savedIndex["successful"])
	assert.Equal(t, float64(1), savedIndex["errors"])
	assert.Equal(t, "test-config.yaml", savedIndex["config_file"])

	// Check extended fields
	assert.Equal(t, 0.006, savedIndex["total_cost"])
	assert.Equal(t, 0.002, savedIndex["average_cost"])
	assert.Equal(t, float64(450), savedIndex["total_tokens"])

	// Check arrays
	runIDs, ok := savedIndex["run_ids"].([]interface{})
	require.True(t, ok)
	assert.Len(t, runIDs, 3)
}

func TestJSONResultRepository_SaveSummary_NilSummary(t *testing.T) {
	tmpDir := t.TempDir()
	repo := jsonrepo.NewJSONResultRepository(tmpDir)

	err := repo.SaveSummary(nil)
	assert.Error(t, err)

	var validationErr *results.ValidationError
	require.ErrorAs(t, err, &validationErr)
	assert.Equal(t, "summary", validationErr.Field)
}

func TestJSONResultRepository_LoadResults(t *testing.T) {
	tmpDir := t.TempDir()
	repo := jsonrepo.NewJSONResultRepository(tmpDir)

	// Create test data
	testResults := []engine.RunResult{
		createTestResult("run-001", "scenario-1", "openai", false, 0.001),
		createTestResult("run-002", "scenario-1", "anthropic", true, 0.002),
	}

	// Save results and summary
	err := repo.SaveResults(testResults)
	require.NoError(t, err)

	summary := &results.ResultSummary{
		TotalTests: 2,
		Passed:     1,
		Failed:     1,
		RunIDs:     []string{"run-001", "run-002"},
		ConfigFile: "test-config.yaml",
		Timestamp:  time.Now(),
	}
	err = repo.SaveSummary(summary)
	require.NoError(t, err)

	// Load results
	loadedResults, err := repo.LoadResults()
	require.NoError(t, err)

	assert.Len(t, loadedResults, 2)

	// Find results by ID for comparison
	resultMap := make(map[string]engine.RunResult)
	for _, result := range loadedResults {
		resultMap[result.RunID] = result
	}

	// Verify loaded results match original
	for _, original := range testResults {
		loaded, exists := resultMap[original.RunID]
		require.True(t, exists, "Result %s not found in loaded results", original.RunID)
		assert.Equal(t, original.RunID, loaded.RunID)
		assert.Equal(t, original.ScenarioID, loaded.ScenarioID)
		assert.Equal(t, original.ProviderID, loaded.ProviderID)
		assert.Equal(t, original.Error, loaded.Error)
	}
}

func TestJSONResultRepository_LoadResults_NoIndex(t *testing.T) {
	tmpDir := t.TempDir()
	repo := jsonrepo.NewJSONResultRepository(tmpDir)

	// Try to load when no index exists
	results, err := repo.LoadResults()
	assert.Error(t, err)
	assert.Nil(t, results)
	assert.Contains(t, err.Error(), "index file not found")
}

func TestJSONResultRepository_LoadResults_EmptyIndex(t *testing.T) {
	tmpDir := t.TempDir()
	repo := jsonrepo.NewJSONResultRepository(tmpDir)

	// Create empty index
	emptyIndex := map[string]interface{}{
		"total_runs": 0,
		"run_ids":    []interface{}{},
	}

	indexFile := filepath.Join(tmpDir, "index.json")
	data, err := json.Marshal(emptyIndex)
	require.NoError(t, err)
	err = os.WriteFile(indexFile, data, 0600)
	require.NoError(t, err)

	// Load results
	results, err := repo.LoadResults()
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestJSONResultRepository_SupportsStreaming(t *testing.T) {
	repo := jsonrepo.NewJSONResultRepository("/tmp/test")
	assert.True(t, repo.SupportsStreaming())
}

func TestJSONResultRepository_SaveResult(t *testing.T) {
	tmpDir := t.TempDir()
	repo := jsonrepo.NewJSONResultRepository(tmpDir)

	testResult := createTestResult("run-001", "scenario-1", "openai", false, 0.001)
	err := repo.SaveResult(&testResult)
	require.NoError(t, err)

	// Verify file was created
	filename := filepath.Join(tmpDir, "run-001.json")
	assert.FileExists(t, filename)

	// Verify content
	data, err := os.ReadFile(filename)
	require.NoError(t, err)

	var savedResult engine.RunResult
	err = json.Unmarshal(data, &savedResult)
	require.NoError(t, err)

	assert.Equal(t, testResult.RunID, savedResult.RunID)
	assert.Equal(t, testResult.ScenarioID, savedResult.ScenarioID)
}

func TestJSONResultRepository_SaveResult_Validation(t *testing.T) {
	tmpDir := t.TempDir()
	repo := jsonrepo.NewJSONResultRepository(tmpDir)

	t.Run("nil result", func(t *testing.T) {
		err := repo.SaveResult(nil)
		assert.Error(t, err)

		var validationErr *results.ValidationError
		require.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "result", validationErr.Field)
	})

	t.Run("empty RunID", func(t *testing.T) {
		result := createTestResult("", "scenario", "provider", false, 0.001)
		err := repo.SaveResult(&result)
		assert.Error(t, err)

		var validationErr *results.ValidationError
		require.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "RunID", validationErr.Field)
	})
}

func TestJSONResultRepository_ListResults(t *testing.T) {
	tmpDir := t.TempDir()
	repo := jsonrepo.NewJSONResultRepository(tmpDir)

	// Create some test files
	testResults := []engine.RunResult{
		createTestResult("run-001", "scenario-1", "openai", false, 0.001),
		createTestResult("run-002", "scenario-1", "anthropic", true, 0.002),
	}

	err := repo.SaveResults(testResults)
	require.NoError(t, err)

	// Create summary
	summary := createTestSummary()
	err = repo.SaveSummary(summary)
	require.NoError(t, err)

	// List results
	resultFiles, err := repo.ListResults()
	require.NoError(t, err)

	// Should find 2 result files (not the index.json)
	assert.Len(t, resultFiles, 2)
	assert.Contains(t, resultFiles, "run-001.json")
	assert.Contains(t, resultFiles, "run-002.json")
	assert.NotContains(t, resultFiles, "index.json")
}

func TestJSONResultRepository_ListResults_EmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	repo := jsonrepo.NewJSONResultRepository(tmpDir)

	resultFiles, err := repo.ListResults()
	require.NoError(t, err)
	assert.Empty(t, resultFiles)
}

func TestJSONResultRepository_GetSummary(t *testing.T) {
	tmpDir := t.TempDir()
	repo := jsonrepo.NewJSONResultRepository(tmpDir)

	// Save summary first
	summary := createTestSummary()
	err := repo.SaveSummary(summary)
	require.NoError(t, err)

	// Get summary
	loadedSummary, err := repo.GetSummary()
	require.NoError(t, err)

	assert.Equal(t, float64(3), loadedSummary["total_runs"])
	assert.Equal(t, float64(2), loadedSummary["successful"])
	assert.Equal(t, float64(1), loadedSummary["errors"])
	assert.Equal(t, "test-config.yaml", loadedSummary["config_file"])
}

func TestJSONResultRepository_GetSummary_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	repo := jsonrepo.NewJSONResultRepository(tmpDir)

	summary, err := repo.GetSummary()
	assert.Error(t, err)
	assert.Nil(t, summary)
	assert.Contains(t, err.Error(), "summary not found")
}

func TestJSONResultRepository_LoadResults_InvalidIndex(t *testing.T) {
	tmpDir := t.TempDir()
	repo := jsonrepo.NewJSONResultRepository(tmpDir)

	// Create invalid JSON index
	indexFile := filepath.Join(tmpDir, "index.json")
	err := os.WriteFile(indexFile, []byte("invalid json"), 0600)
	require.NoError(t, err)

	results, err := repo.LoadResults()
	assert.Error(t, err)
	assert.Nil(t, results)
	assert.Contains(t, err.Error(), "failed to parse index file")
}

func TestJSONResultRepository_LoadResults_InvalidRunIDs(t *testing.T) {
	tmpDir := t.TempDir()
	repo := jsonrepo.NewJSONResultRepository(tmpDir)

	// Create index with invalid run_ids format
	invalidIndex := map[string]interface{}{
		"total_runs": 1,
		"run_ids":    "not-an-array", // Invalid format
	}

	indexFile := filepath.Join(tmpDir, "index.json")
	data, err := json.Marshal(invalidIndex)
	require.NoError(t, err)
	err = os.WriteFile(indexFile, data, 0600)
	require.NoError(t, err)

	results, err := repo.LoadResults()
	assert.Error(t, err)
	assert.Nil(t, results)
	assert.Contains(t, err.Error(), "invalid run_ids format")
}

func TestJSONResultRepository_ListResults_NonExistentDirectory(t *testing.T) {
	repo := jsonrepo.NewJSONResultRepository("/non/existent/dir")

	resultFiles, err := repo.ListResults()
	require.NoError(t, err)
	assert.Empty(t, resultFiles)
}

func TestJSONResultRepository_GetSummary_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	repo := jsonrepo.NewJSONResultRepository(tmpDir)

	// Create invalid JSON summary
	indexFile := filepath.Join(tmpDir, "index.json")
	err := os.WriteFile(indexFile, []byte("invalid json"), 0600)
	require.NoError(t, err)

	summary, err := repo.GetSummary()
	assert.Error(t, err)
	assert.Nil(t, summary)
	assert.Contains(t, err.Error(), "failed to parse summary")
}

func TestJSONResultRepository_IntegrationTest(t *testing.T) {
	tmpDir := t.TempDir()
	repo := jsonrepo.NewJSONResultRepository(tmpDir)

	// Test complete workflow: Save -> Load -> Verify
	originalResults := []engine.RunResult{
		createTestResult("run-001", "scenario-1", "openai", false, 0.001),
		createTestResult("run-002", "scenario-1", "anthropic", true, 0.002),
		createTestResult("run-003", "scenario-2", "openai", false, 0.003),
	}

	// Build summary
	builder := results.NewSummaryBuilder("integration-test.yaml")
	summary := builder.BuildSummary(originalResults)

	// Save everything
	err := repo.SaveResults(originalResults)
	require.NoError(t, err)

	err = repo.SaveSummary(summary)
	require.NoError(t, err)

	// Load and verify
	loadedResults, err := repo.LoadResults()
	require.NoError(t, err)

	loadedSummary, err := repo.GetSummary()
	require.NoError(t, err)

	// Verify counts match
	assert.Len(t, loadedResults, len(originalResults))
	assert.Equal(t, float64(len(originalResults)), loadedSummary["total_runs"])

	// Verify files exist
	resultFiles, err := repo.ListResults()
	require.NoError(t, err)
	assert.Len(t, resultFiles, len(originalResults))
}