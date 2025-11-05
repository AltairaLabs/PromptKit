package markdown

import (
	"os"
	"path/filepath"
	"testing"

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