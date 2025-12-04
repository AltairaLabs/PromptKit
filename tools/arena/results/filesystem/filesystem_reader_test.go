package filesystem

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/results"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

func TestNewFilesystemResultReader(t *testing.T) {
	reader := NewFilesystemResultReader("/tmp/test")
	assert.NotNil(t, reader)
	assert.Equal(t, "/tmp/test", reader.baseDir)
	assert.NotNil(t, reader.cache)
}

func TestFilesystemResultReader_ListResults_EmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	reader := NewFilesystemResultReader(tmpDir)
	metadata, err := reader.ListResults()

	require.NoError(t, err)
	assert.Empty(t, metadata)
}

func TestFilesystemResultReader_LoadResult(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test result
	testResult := statestore.RunResult{
		RunID:      "run-123",
		ScenarioID: "test-scenario",
		ProviderID: "openai",
		Region:     "us-east-1",
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
		Cost: types.CostInfo{
			TotalCost: 0.05,
		},
	}

	data, err := json.Marshal(testResult)
	require.NoError(t, err)

	filePath := filepath.Join(tmpDir, "run-123.json")
	err = os.WriteFile(filePath, data, 0644)
	require.NoError(t, err)

	// Test LoadResult
	reader := NewFilesystemResultReader(tmpDir)
	result, err := reader.LoadResult("run-123")

	require.NoError(t, err)
	assert.Equal(t, "run-123", result.RunID)
	assert.Equal(t, "test-scenario", result.ScenarioID)
	assert.Equal(t, "openai", result.ProviderID)
}

func TestFilesystemResultReader_LoadResult_NotFound(t *testing.T) {
	tmpDir := t.TempDir()

	reader := NewFilesystemResultReader(tmpDir)
	_, err := reader.LoadResult("nonexistent-run")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestFilesystemResultReader_SupportsFiltering(t *testing.T) {
	reader := NewFilesystemResultReader("/tmp/test")
	assert.False(t, reader.SupportsFiltering())
}

func TestFilesystemResultReader_ListResults_WithMultipleFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple test results
	results := []statestore.RunResult{
		{
			RunID:      "run-001",
			ScenarioID: "scenario-1",
			ProviderID: "openai",
			Region:     "us-east-1",
			Error:      "",
			Cost:       types.CostInfo{TotalCost: 0.05},
		},
		{
			RunID:      "run-002",
			ScenarioID: "scenario-2",
			ProviderID: "anthropic",
			Region:     "us-west-2",
			Error:      "timeout",
			Cost:       types.CostInfo{TotalCost: 0.03},
		},
		{
			RunID:      "run-003",
			ScenarioID: "scenario-1",
			ProviderID: "gemini",
			Region:     "us-central1",
			Error:      "",
			Cost:       types.CostInfo{TotalCost: 0.02},
		},
	}

	for _, result := range results {
		data, err := json.Marshal(result)
		require.NoError(t, err)
		filePath := filepath.Join(tmpDir, result.RunID+".json")
		err = os.WriteFile(filePath, data, 0644)
		require.NoError(t, err)
	}

	// Test ListResults
	reader := NewFilesystemResultReader(tmpDir)
	metadata, err := reader.ListResults()

	require.NoError(t, err)
	assert.Len(t, metadata, 3)

	// Verify metadata content
	runIDs := make([]string, len(metadata))
	for i, meta := range metadata {
		runIDs[i] = meta.RunID
	}
	assert.Contains(t, runIDs, "run-001")
	assert.Contains(t, runIDs, "run-002")
	assert.Contains(t, runIDs, "run-003")

	// Check status mapping
	var failedMeta *statestore.RunResult
	for _, meta := range metadata {
		if meta.RunID == "run-002" {
			assert.Equal(t, "failed", meta.Status)
			assert.Equal(t, "timeout", meta.Error)
		} else {
			assert.Equal(t, "success", meta.Status)
		}
	}
	_ = failedMeta
}

func TestFilesystemResultReader_LoadResults(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test results
	testResults := []statestore.RunResult{
		{RunID: "run-A", ScenarioID: "test-1", ProviderID: "openai", Region: "us-east-1"},
		{RunID: "run-B", ScenarioID: "test-2", ProviderID: "anthropic", Region: "us-west-2"},
		{RunID: "run-C", ScenarioID: "test-3", ProviderID: "gemini", Region: "us-central1"},
	}

	for _, result := range testResults {
		data, err := json.Marshal(result)
		require.NoError(t, err)
		filePath := filepath.Join(tmpDir, result.RunID+".json")
		err = os.WriteFile(filePath, data, 0644)
		require.NoError(t, err)
	}

	reader := NewFilesystemResultReader(tmpDir)
	results, err := reader.LoadResults([]string{"run-A", "run-C"})

	require.NoError(t, err)
	assert.Len(t, results, 2)

	runIDs := make([]string, len(results))
	for i, result := range results {
		runIDs[i] = result.RunID
	}
	assert.Contains(t, runIDs, "run-A")
	assert.Contains(t, runIDs, "run-C")
}

func TestFilesystemResultReader_LoadResults_PartialFailure(t *testing.T) {
	tmpDir := t.TempDir()

	// Create only one result
	testResult := statestore.RunResult{
		RunID:      "run-exists",
		ScenarioID: "test",
		ProviderID: "openai",
	}

	data, err := json.Marshal(testResult)
	require.NoError(t, err)
	filePath := filepath.Join(tmpDir, "run-exists.json")
	err = os.WriteFile(filePath, data, 0644)
	require.NoError(t, err)

	reader := NewFilesystemResultReader(tmpDir)
	results, err := reader.LoadResults([]string{"run-exists", "run-missing"})

	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "run-exists", results[0].RunID)
}

func TestFilesystemResultReader_LoadResults_AllFail(t *testing.T) {
	tmpDir := t.TempDir()

	reader := NewFilesystemResultReader(tmpDir)
	_, err := reader.LoadResults([]string{"run-1", "run-2"})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load any results")
}

func TestFilesystemResultReader_LoadAllResults(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test results
	testResults := []statestore.RunResult{
		{RunID: "run-X", ScenarioID: "test-1", ProviderID: "openai"},
		{RunID: "run-Y", ScenarioID: "test-2", ProviderID: "anthropic"},
	}

	for _, result := range testResults {
		data, err := json.Marshal(result)
		require.NoError(t, err)
		filePath := filepath.Join(tmpDir, result.RunID+".json")
		err = os.WriteFile(filePath, data, 0644)
		require.NoError(t, err)
	}

	reader := NewFilesystemResultReader(tmpDir)
	results, err := reader.LoadAllResults()

	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestFilesystemResultReader_FilterResults(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test results
	testResults := []statestore.RunResult{
		{RunID: "run-1", ScenarioID: "scenario-A", ProviderID: "openai", Region: "us-east-1", Error: ""},
		{RunID: "run-2", ScenarioID: "scenario-B", ProviderID: "anthropic", Region: "us-west-2", Error: "failed"},
		{RunID: "run-3", ScenarioID: "scenario-A", ProviderID: "gemini", Region: "us-central1", Error: ""},
	}

	for _, result := range testResults {
		data, err := json.Marshal(result)
		require.NoError(t, err)
		filePath := filepath.Join(tmpDir, result.RunID+".json")
		err = os.WriteFile(filePath, data, 0644)
		require.NoError(t, err)
	}

	reader := NewFilesystemResultReader(tmpDir)

	t.Run("filter by scenario", func(t *testing.T) {
		filter := &results.ResultFilter{Scenarios: []string{"scenario-A"}}
		filtered, err := reader.FilterResults(filter)

		require.NoError(t, err)
		assert.Len(t, filtered, 2)
	})

	t.Run("filter by status", func(t *testing.T) {
		filter := &results.ResultFilter{Status: []string{"success"}}
		filtered, err := reader.FilterResults(filter)

		require.NoError(t, err)
		assert.Len(t, filtered, 2)
	})

	t.Run("filter by provider", func(t *testing.T) {
		filter := &results.ResultFilter{Providers: []string{"openai"}}
		filtered, err := reader.FilterResults(filter)

		require.NoError(t, err)
		assert.Len(t, filtered, 1)
		assert.Equal(t, "run-1", filtered[0].RunID)
	})
}

func TestFilesystemResultReader_Caching(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test result
	testResult := statestore.RunResult{
		RunID:      "run-cached",
		ScenarioID: "test",
		ProviderID: "openai",
	}

	data, err := json.Marshal(testResult)
	require.NoError(t, err)
	filePath := filepath.Join(tmpDir, "run-cached.json")
	err = os.WriteFile(filePath, data, 0644)
	require.NoError(t, err)

	reader := NewFilesystemResultReader(tmpDir)

	// First load
	result1, err := reader.LoadResult("run-cached")
	require.NoError(t, err)
	assert.Equal(t, "run-cached", result1.RunID)

	// Second load (should come from cache)
	result2, err := reader.LoadResult("run-cached")
	require.NoError(t, err)
	assert.Equal(t, "run-cached", result2.RunID)

	// Verify it's the same pointer (cached)
	assert.True(t, result1 == result2)
}

func TestFilesystemResultReader_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()

	// Create invalid JSON file
	filePath := filepath.Join(tmpDir, "invalid.json")
	err := os.WriteFile(filePath, []byte("{invalid json"), 0644)
	require.NoError(t, err)

	reader := NewFilesystemResultReader(tmpDir)
	metadata, err := reader.ListResults()

	// Should succeed but skip invalid file
	require.NoError(t, err)
	assert.Empty(t, metadata)
}

func TestFilesystemResultReader_SkipIndexJSON(t *testing.T) {
	tmpDir := t.TempDir()

	// Create index.json (should be skipped)
	indexPath := filepath.Join(tmpDir, "index.json")
	testResult := statestore.RunResult{RunID: "should-be-skipped"}
	data, err := json.Marshal(testResult)
	require.NoError(t, err)
	err = os.WriteFile(indexPath, data, 0644)
	require.NoError(t, err)

	// Create normal result
	normalPath := filepath.Join(tmpDir, "run-normal.json")
	normalResult := statestore.RunResult{RunID: "run-normal", ScenarioID: "test", ProviderID: "openai"}
	data, err = json.Marshal(normalResult)
	require.NoError(t, err)
	err = os.WriteFile(normalPath, data, 0644)
	require.NoError(t, err)

	reader := NewFilesystemResultReader(tmpDir)
	metadata, err := reader.ListResults()

	require.NoError(t, err)
	assert.Len(t, metadata, 1)
	assert.Equal(t, "run-normal", metadata[0].RunID)
}

func TestFilesystemResultReader_LoadResult_MissingRunID(t *testing.T) {
	tmpDir := t.TempDir()

	// Create result without RunID
	invalidResult := map[string]interface{}{
		"ScenarioID": "test",
		"ProviderID": "openai",
	}

	data, err := json.Marshal(invalidResult)
	require.NoError(t, err)
	filePath := filepath.Join(tmpDir, "invalid-run.json")
	err = os.WriteFile(filePath, data, 0644)
	require.NoError(t, err)

	reader := NewFilesystemResultReader(tmpDir)
	_, err = reader.LoadResult("invalid-run")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing RunID")
}
