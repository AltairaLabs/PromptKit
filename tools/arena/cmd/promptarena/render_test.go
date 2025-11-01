package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateHTMLFromIndex_InvalidPath(t *testing.T) {
	err := generateHTMLFromIndex("/nonexistent/index.json", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read index file")
}

func TestGenerateHTMLFromIndex_InvalidJSON(t *testing.T) {
	// Create temp file with invalid JSON
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "invalid.json")
	err := os.WriteFile(indexPath, []byte("{invalid json"), 0644)
	require.NoError(t, err)

	err = generateHTMLFromIndex(indexPath, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse index file")
}

func TestGenerateHTMLFromIndex_EmptyIndex(t *testing.T) {
	// Create temp directory with empty index
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "index.json")

	index := struct {
		RunIDs []string `json:"run_ids"`
	}{
		RunIDs: []string{},
	}

	data, err := json.Marshal(index)
	require.NoError(t, err)
	err = os.WriteFile(indexPath, data, 0644)
	require.NoError(t, err)

	// Should succeed but generate empty report
	outputPath := filepath.Join(tmpDir, "report.html")
	err = generateHTMLFromIndex(indexPath, outputPath)

	// Error expected because no result files exist
	assert.Error(t, err)
}

func TestGenerateHTMLFromIndex_WithValidResults(t *testing.T) {
	// Create temp directory structure
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "index.json")

	// Create a simple result file
	runID := "test-run-001"
	resultPath := filepath.Join(tmpDir, runID+".json")

	result := engine.RunResult{
		RunID:      runID,
		ScenarioID: "test-scenario",
		ProviderID: "test-provider",
		Region:     "us-east-1",
		Messages:   []types.Message{},
		Error:      "", // Empty error means success
	}

	resultData, err := json.Marshal(result)
	require.NoError(t, err)
	err = os.WriteFile(resultPath, resultData, 0644)
	require.NoError(t, err)

	// Create index pointing to result
	index := struct {
		RunIDs []string `json:"run_ids"`
	}{
		RunIDs: []string{runID},
	}

	indexData, err := json.Marshal(index)
	require.NoError(t, err)
	err = os.WriteFile(indexPath, indexData, 0644)
	require.NoError(t, err)

	// Generate HTML report
	outputPath := filepath.Join(tmpDir, "report.html")
	err = generateHTMLFromIndex(indexPath, outputPath)

	// Should succeed
	require.NoError(t, err)

	// Verify HTML file was created
	_, err = os.Stat(outputPath)
	assert.NoError(t, err, "HTML report should be created")

	// Verify HTML content is non-empty
	content, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.NotEmpty(t, content, "HTML report should have content")
	assert.Contains(t, string(content), "<html", "Output should be HTML")
}

func TestGenerateHTMLFromIndex_DefaultOutputPath(t *testing.T) {
	// Create temp directory with valid index and results
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "index.json")

	runID := "test-run-002"
	resultPath := filepath.Join(tmpDir, runID+".json")

	result := engine.RunResult{
		RunID:      runID,
		ScenarioID: "test-scenario",
		ProviderID: "test-provider",
		Region:     "us-east-1",
		Messages:   []types.Message{},
		Error:      "",
	}

	resultData, err := json.Marshal(result)
	require.NoError(t, err)
	err = os.WriteFile(resultPath, resultData, 0644)
	require.NoError(t, err)

	index := struct {
		RunIDs []string `json:"run_ids"`
	}{
		RunIDs: []string{runID},
	}

	indexData, err := json.Marshal(index)
	require.NoError(t, err)
	err = os.WriteFile(indexPath, indexData, 0644)
	require.NoError(t, err)

	// Generate with empty output path (should use default)
	err = generateHTMLFromIndex(indexPath, "")
	require.NoError(t, err)

	// Verify a report-*.html file was created in tmpDir
	files, err := filepath.Glob(filepath.Join(tmpDir, "report-*.html"))
	require.NoError(t, err)
	assert.NotEmpty(t, files, "Default output file should be created")
}

func TestGenerateHTMLFromIndex_MissingResultFiles(t *testing.T) {
	// Create index that references non-existent result files
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "index.json")

	index := struct {
		RunIDs []string `json:"run_ids"`
	}{
		RunIDs: []string{"missing-run-001", "missing-run-002"},
	}

	indexData, err := json.Marshal(index)
	require.NoError(t, err)
	err = os.WriteFile(indexPath, indexData, 0644)
	require.NoError(t, err)

	outputPath := filepath.Join(tmpDir, "report.html")
	err = generateHTMLFromIndex(indexPath, outputPath)

	// Should error when NO valid result files can be loaded
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no valid result files")
}
