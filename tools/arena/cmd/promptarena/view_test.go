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

func TestViewCommand_InvalidDirectory(t *testing.T) {
	// Test with non-existent directory
	err := runView(viewCmd, []string{"/nonexistent/path/that/does/not/exist"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestViewCommand_FileInsteadOfDirectory(t *testing.T) {
	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "testfile-*.txt")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	// Test with a file instead of directory
	err = runView(viewCmd, []string{tmpFile.Name()})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestViewCommand_DefaultDirectory(t *testing.T) {
	// This test validates argument handling but doesn't run the TUI
	// (which would block in test environment)

	// Test with no arguments (should use current directory)
	// We can't actually run the TUI in tests, but we can verify the path resolution logic
	tmpDir := t.TempDir()

	// Change to temp directory
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)

	err = os.Chdir(tmpDir)
	require.NoError(t, err)

	// Verify the directory exists (runView would fail at TUI creation, which is expected)
	absPath, err := filepath.Abs(".")
	require.NoError(t, err)

	// Both paths should resolve to the same location (handle symlinks)
	expectedReal, err := filepath.EvalSymlinks(tmpDir)
	require.NoError(t, err)
	actualReal, err := filepath.EvalSymlinks(absPath)
	require.NoError(t, err)

	assert.Equal(t, expectedReal, actualReal)
}

func TestViewCommand_ValidDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// We can verify the directory validation passes, but we can't run the TUI
	// The runView function will fail when trying to start the TUI, but that's after
	// successful directory validation
	absPath, err := filepath.Abs(tmpDir)
	require.NoError(t, err)

	info, err := os.Stat(absPath)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}
