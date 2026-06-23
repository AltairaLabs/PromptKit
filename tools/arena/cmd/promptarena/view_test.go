package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestViewCommand_Help(t *testing.T) {
	// Test that the view command is registered
	assert.NotNil(t, viewCmd)
	assert.Equal(t, "view [results-dir]", viewCmd.Use)
	assert.Contains(t, viewCmd.Short, "Browse")
}

func TestValidateResultsDir_Valid(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, validateResultsDir(tmpDir))
}

func TestValidateResultsDir_DoesNotExist(t *testing.T) {
	err := validateResultsDir(filepath.Join(t.TempDir(), "nonexistent"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestValidateResultsDir_FileNotDir(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "arena.yaml")
	require.NoError(t, os.WriteFile(filePath, []byte("x"), 0600))
	err := validateResultsDir(filePath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}
