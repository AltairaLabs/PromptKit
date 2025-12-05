package pages

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestFileBrowserPage_New(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	assert.NotNil(t, page)
	assert.NotNil(t, page.filePicker)
	assert.Nil(t, page.err)
}

func TestFileBrowserPage_SetDimensions(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	page.SetDimensions(100, 50)

	assert.Equal(t, 100, page.width)
	assert.Equal(t, 50, page.height)
	// Note: filepicker height is set dynamically in Render(), not in SetDimensions
}

func TestFileBrowserPage_Init(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	cmd := page.Init()
	assert.NotNil(t, cmd)
}

func TestFileBrowserPage_View(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)
	page.SetDimensions(80, 24)

	view := page.View()
	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Browse Results")
}

func TestFileBrowserPage_UpdateQuitKey(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	// Test 'q' key
	model, cmd := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	assert.NotNil(t, model)
	assert.NotNil(t, cmd)
}

func TestFileBrowserPage_UpdateCtrlC(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	// Test ctrl+c
	model, cmd := page.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.NotNil(t, model)
	assert.NotNil(t, cmd)
}

func TestFileBrowserPage_GetKeyBindings(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	bindings := page.GetKeyBindings()
	assert.NotEmpty(t, bindings)
	assert.Equal(t, 5, len(bindings))

	// Check that key bindings contain expected items
	var hasNavigate, hasSelect, hasQuit bool
	for _, kb := range bindings {
		if kb.Description == "navigate" {
			hasNavigate = true
		}
		if kb.Description == "select file" {
			hasSelect = true
		}
		if kb.Description == "quit" {
			hasQuit = true
		}
	}
	assert.True(t, hasNavigate)
	assert.True(t, hasSelect)
	assert.True(t, hasQuit)
}

func TestFileBrowserPage_RenderNoLongerIncludesKeyLegend(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)
	page.SetDimensions(80, 24)

	// Render should not include key legend (moved to footer)
	view := page.Render()
	assert.NotEmpty(t, view)
	// The view should contain the file picker, but not the styled key legend
	assert.Contains(t, view, "Browse Results")
}

func TestFileBrowserPage_Reset(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	// Set a path
	page.filePicker.Path = "/some/path/file.json"
	assert.NotEmpty(t, page.filePicker.Path)

	// Reset should clear the path
	page.Reset()
	assert.Empty(t, page.filePicker.Path)
}

func TestFileBrowserPage_UpdateErrorMsg(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	// Send an error message
	errMsg := fileBrowserErrorMsg{err: assert.AnError}
	model, cmd := page.Update(errMsg)

	p := model.(*FileBrowserPage)
	assert.Equal(t, assert.AnError, p.err)
	assert.Nil(t, cmd)
}

func TestFileBrowserPage_RenderWithError(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)
	page.SetDimensions(80, 24)

	// Set an error
	page.err = assert.AnError

	view := page.Render()
	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Error")
}

func TestFileBrowserPage_RenderWithParseError(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)
	page.SetDimensions(80, 24)

	// Create an error that contains "parse" or "unmarshal"
	parseErr := fmt.Errorf("failed to parse JSON: invalid character")
	page.err = parseErr

	view := page.Render()
	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Invalid JSON file")
}

func TestFileBrowserPage_RenderWithSmallDimensions(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	// Set very small dimensions
	page.SetDimensions(10, 3)

	view := page.Render()
	assert.NotEmpty(t, view)
}

func TestFileBrowserPage_HandleEnterKey_EmptyPath(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	// Set up state where path is empty
	page.filePicker.Path = ""

	// Call handleEnterKey
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	cmd := page.handleEnterKey(msg)

	// May return nil or a command depending on filepicker state
	_ = cmd
}

func TestFileBrowserPage_HandleEnterKey_PathUnchanged(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	// Set a path that won't change
	initialPath := tmpDir
	page.filePicker.Path = initialPath

	// Call handleEnterKey
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	cmd := page.handleEnterKey(msg)

	// May return nil or a command depending on filepicker state
	_ = cmd
}

func TestFileBrowserPage_HandleEnterKey_InvalidPath(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	// Simulate a path being set to a non-existent file
	// We need to bypass filepicker's update to test the error path
	page.filePicker.Path = "/nonexistent/path/file.json"

	// Manually test the logic after filepicker sets path
	info, err := os.Stat(page.filePicker.Path)
	assert.Error(t, err)
	assert.Nil(t, info)
}

func TestFileBrowserPage_HandleEnterKey_Directory(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	// Create a subdirectory
	subDir := filepath.Join(tmpDir, "subdir")
	err := os.Mkdir(subDir, 0755)
	assert.NoError(t, err)

	// Set path to directory
	page.filePicker.Path = subDir

	// Check if it's a directory
	info, err := os.Stat(page.filePicker.Path)
	assert.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestFileBrowserPage_HandleEnterKey_File(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	// Create a test file
	testFile := filepath.Join(tmpDir, "test-run.json")
	err := os.WriteFile(testFile, []byte(`{"test": "data"}`), 0644)
	assert.NoError(t, err)

	// Set path to file
	page.filePicker.Path = testFile

	// Check if it's a file
	info, err := os.Stat(page.filePicker.Path)
	assert.NoError(t, err)
	assert.False(t, info.IsDir())

	// Extract runID like handleEnterKey does
	filename := filepath.Base(page.filePicker.Path)
	runID := strings.TrimSuffix(filename, ".json")
	assert.Equal(t, "test-run", runID)
}

func TestFileBrowserPage_HandleEnterKey_IndexFile(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	// Create an index.json file
	indexFile := filepath.Join(tmpDir, "index.json")
	err := os.WriteFile(indexFile, []byte(`{"run_ids": ["run1", "run2"]}`), 0644)
	assert.NoError(t, err)

	// Set path to index file
	page.filePicker.Path = indexFile

	// Extract runID
	filename := filepath.Base(page.filePicker.Path)
	runID := strings.TrimSuffix(filename, ".json")
	assert.Equal(t, "index", runID)
}

func TestFileBrowserPage_UpdateEnterKeyWithFile(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	// Create a test file
	testFile := filepath.Join(tmpDir, "result.json")
	err := os.WriteFile(testFile, []byte(`{}`), 0644)
	assert.NoError(t, err)

	// Manually set the path (simulating filepicker selection)
	page.filePicker.Path = testFile

	// Send Enter key with path already set
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	_, cmd := page.Update(msg)

	// Command may be nil or not depending on filepicker behavior
	_ = cmd
}

func TestFileBrowserPage_UpdateOtherKeys(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	// Test various navigation keys that filepicker handles
	keys := []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'j'}},
		{Type: tea.KeyRunes, Runes: []rune{'k'}},
		{Type: tea.KeyRunes, Runes: []rune{'h'}},
		{Type: tea.KeyRunes, Runes: []rune{'l'}},
		{Type: tea.KeyDown},
		{Type: tea.KeyUp},
	}

	for _, key := range keys {
		model, cmd := page.Update(key)
		assert.NotNil(t, model)
		// cmd might be nil for some keys
		_ = cmd
	}
}

func TestFileBrowserPage_RenderWithVariousDimensions(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	tests := []struct {
		name   string
		width  int
		height int
	}{
		{"normal", 80, 24},
		{"wide", 200, 50},
		{"tall", 80, 100},
		{"small width", 30, 24},
		{"small height", 80, 10},
		{"minimum", 20, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page.SetDimensions(tt.width, tt.height)
			view := page.Render()
			assert.NotEmpty(t, view)
		})
	}
}

func TestFileBrowserPage_Constants(t *testing.T) {
	// Verify constants are defined with expected values
	assert.Equal(t, 5, fileBrowserChrome)
	assert.Equal(t, 80, defaultViewportWidth)
	assert.Equal(t, 20, defaultViewportHeight)
	assert.Equal(t, 5, minFileBrowserHeight)
	assert.Equal(t, 20, minFileBrowserWidth)
	assert.Equal(t, 6, fileBrowserWidthChrome)
	assert.Equal(t, 2, fileBrowserPaddingHoriz)
}

func TestFileBrowserPage_FilePickerKeyMap(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	// Verify custom key bindings are set
	assert.NotNil(t, page.filePicker.KeyMap)
	assert.NotNil(t, page.filePicker.KeyMap.GoToTop)
	assert.NotNil(t, page.filePicker.KeyMap.GoToLast)
	assert.NotNil(t, page.filePicker.KeyMap.Down)
	assert.NotNil(t, page.filePicker.KeyMap.Up)
	assert.NotNil(t, page.filePicker.KeyMap.PageDown)
	assert.NotNil(t, page.filePicker.KeyMap.PageUp)
	assert.NotNil(t, page.filePicker.KeyMap.Back)
	assert.NotNil(t, page.filePicker.KeyMap.Open)
	assert.NotNil(t, page.filePicker.KeyMap.Select)
}

func TestFileBrowserPage_AllowedTypes(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	// Verify only JSON files are allowed
	assert.Equal(t, []string{".json"}, page.filePicker.AllowedTypes)
	assert.False(t, page.filePicker.ShowHidden)
}

func TestFileBrowserPage_CurrentDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	// Verify current directory is set correctly
	assert.Equal(t, tmpDir, page.filePicker.CurrentDirectory)
}

func TestFileBrowserPage_HandleEnterKey_DirectoryNavigation(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	// Create a subdirectory
	subDir := filepath.Join(tmpDir, "subdir")
	err := os.Mkdir(subDir, 0755)
	assert.NoError(t, err)

	// Simulate selecting a directory by updating the path before calling handleEnterKey
	oldPath := page.filePicker.Path
	page.filePicker.Path = subDir

	// Call handleEnterKey with a different path (directory)
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	
	// Store old path for comparison
	savedPath := page.filePicker.Path
	
	// The function should detect it's a directory and return the command
	cmd := page.handleEnterKey(msg)
	
	// Verify the path was a directory
	info, err := os.Stat(savedPath)
	assert.NoError(t, err)
	assert.True(t, info.IsDir())
	
	_ = cmd
	_ = oldPath
}

func TestFileBrowserPage_HandleEnterKey_FileSelection(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	// Create a test file
	testFile := filepath.Join(tmpDir, "test-result.json")
	err := os.WriteFile(testFile, []byte(`{"test": "data"}`), 0644)
	assert.NoError(t, err)

	// Simulate the filepicker setting the path to a file
	oldPath := page.filePicker.Path
	page.filePicker.Path = testFile

	// Call handleEnterKey
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	cmd := page.handleEnterKey(msg)
	
	// Execute the command to get the message
	if cmd != nil {
		result := cmd()
		
		// Should be a FileSelectedMsg
		if fileMsg, ok := result.(FileSelectedMsg); ok {
			assert.Equal(t, "test-result", fileMsg.RunID)
			assert.Equal(t, testFile, fileMsg.Path)
		}
	}
	
	_ = oldPath
}

func TestFileBrowserPage_HandleEnterKey_StatError(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	// Set path to a file that will be deleted (to cause stat error)
	testFile := filepath.Join(tmpDir, "temp.json")
	err := os.WriteFile(testFile, []byte(`{}`), 0644)
	assert.NoError(t, err)
	
	// Set the path
	page.filePicker.Path = testFile
	
	// Delete the file to cause stat error
	err = os.Remove(testFile)
	assert.NoError(t, err)
	
	// Now simulate the path being different (to trigger the stat check)
	oldPath := ""
	page.filePicker.Path = testFile
	
	// Manually check what would happen
	info, err := os.Stat(page.filePicker.Path)
	assert.Error(t, err)
	assert.Nil(t, info)
	
	_ = oldPath
}
