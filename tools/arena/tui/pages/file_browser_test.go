package pages

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/reader"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

func TestFileBrowserPage_New(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	assert.NotNil(t, page)
	assert.NotNil(t, page.filePicker)
	assert.NotNil(t, page.reader)
	assert.True(t, page.loading)
	assert.Nil(t, page.selected)
	assert.Nil(t, page.err)
}

func TestFileBrowserPage_SetDimensions(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	page.SetDimensions(100, 50)

	assert.Equal(t, 100, page.width)
	assert.Equal(t, 50, page.height)
	assert.Equal(t, 40, page.filePicker.Height) // 50 - 10
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

	// Test loading state
	view := page.View()
	assert.Contains(t, view, "Loading")

	// Test error state
	page.loading = false
	page.err = assert.AnError
	view = page.View()
	assert.Contains(t, view, "Error")
}

func TestFileBrowserPage_UpdateWithMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	// Create a metadata message
	msg := fileBrowserMetadataMsg{
		metadata: []reader.ResultMetadata{
			{RunID: "run-1", Scenario: "test"},
		},
	}

	model, cmd := page.Update(msg)
	updatedPage := model.(*FileBrowserPage)

	assert.False(t, updatedPage.loading)
	assert.Len(t, updatedPage.metadata, 1)
	assert.Nil(t, cmd)
}

func TestFileBrowserPage_UpdateWithError(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	// Create an error message
	msg := fileBrowserErrorMsg{err: assert.AnError}

	model, cmd := page.Update(msg)
	updatedPage := model.(*FileBrowserPage)

	assert.False(t, updatedPage.loading)
	assert.NotNil(t, updatedPage.err)
	assert.Nil(t, cmd)
}

func TestFileBrowserPage_UpdateQuitKey(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	// Test quit key
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
	_, cmd := page.Update(msg)

	assert.NotNil(t, cmd)
}

func TestFileBrowserPage_GetMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	page.metadata = []reader.ResultMetadata{
		{RunID: "run-1"},
		{RunID: "run-2"},
	}

	metadata := page.GetMetadata()
	assert.Len(t, metadata, 2)
}

func TestFileBrowserPage_SelectedResult(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	assert.Nil(t, page.SelectedResult())

	// Set a selected result
	result := &statestore.RunResult{RunID: "test-run"}
	page.selected = result

	assert.Equal(t, result, page.SelectedResult())
}

func TestFileBrowserPage_LoadResult(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a test result file
	testResult := statestore.RunResult{
		RunID:      "test-123",
		ScenarioID: "test-scenario",
		ProviderID: "openai",
		Region:     "us-east-1",
	}

	data, err := json.Marshal(testResult)
	require.NoError(t, err)

	filePath := filepath.Join(tmpDir, "test-123.json")
	err = os.WriteFile(filePath, data, 0644)
	require.NoError(t, err)

	page := NewFileBrowserPage(tmpDir)

	// Execute the load command
	cmd := page.loadResult(filePath)
	msg := cmd()

	// Should return a result message
	resultMsg, ok := msg.(fileBrowserResultMsg)
	assert.True(t, ok)
	assert.NotNil(t, resultMsg.result)
	assert.Equal(t, "test-123", resultMsg.result.RunID)
}

func TestFileBrowserPage_LoadResultNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	// Try to load a non-existent file
	cmd := page.loadResult(filepath.Join(tmpDir, "nonexistent.json"))
	msg := cmd()

	// Should return an error message
	errorMsg, ok := msg.(fileBrowserErrorMsg)
	assert.True(t, ok)
	assert.NotNil(t, errorMsg.err)
}

func TestFileBrowserPage_RenderResultPreview(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)
	page.SetDimensions(100, 50)

	// Set a selected result
	page.selected = &statestore.RunResult{
		RunID:      "test-run",
		ScenarioID: "test-scenario",
		ProviderID: "openai",
		Region:     "us-east-1",
		Duration:   time.Second * 5,
		Cost: types.CostInfo{
			TotalCost: 0.05,
		},
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
	}

	preview := page.renderResultPreview()
	assert.Contains(t, preview, "test-run")
	assert.Contains(t, preview, "test-scenario")
	assert.Contains(t, preview, "openai")
	assert.Contains(t, preview, "Success")
}

func TestFileBrowserPage_RenderResultPreviewWithError(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)
	page.SetDimensions(100, 50)

	// Set a failed result
	page.selected = &statestore.RunResult{
		RunID:      "test-run",
		ScenarioID: "test-scenario",
		ProviderID: "openai",
		Region:     "us-east-1",
		Error:      "API timeout",
		Duration:   time.Second * 5,
		Cost: types.CostInfo{
			TotalCost: 0.05,
		},
	}

	preview := page.renderResultPreview()
	assert.Contains(t, preview, "Failed")
	assert.Contains(t, preview, "API timeout")
}

func TestFileBrowserPage_RenderWithMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)
	page.SetDimensions(100, 50)
	page.loading = false

	// Add metadata
	page.metadata = []reader.ResultMetadata{
		{RunID: "run-1"},
		{RunID: "run-2"},
	}

	view := page.Render()
	assert.Contains(t, view, "Browse Results")
	assert.Contains(t, view, "Found 2 results")
}

func TestFileBrowserPage_RenderWithNoMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)
	page.SetDimensions(100, 50)
	page.loading = false
	page.metadata = []reader.ResultMetadata{}

	view := page.Render()
	assert.Contains(t, view, "Browse Results")
	assert.NotContains(t, view, "Found 0 results")
}

func TestFileBrowserPage_UpdateWithResult(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	// Create a result message
	result := &statestore.RunResult{RunID: "test-run"}
	msg := fileBrowserResultMsg{result: result}

	model, cmd := page.Update(msg)
	updatedPage := model.(*FileBrowserPage)

	assert.Equal(t, result, updatedPage.selected)
	assert.Nil(t, cmd)
}

func TestFileBrowserPage_UpdateCtrlC(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)

	// Test ctrl+c
	msg := tea.KeyMsg{Type: tea.KeyCtrlC}
	_, cmd := page.Update(msg)

	assert.NotNil(t, cmd)
}

func TestFileBrowserPage_LoadMetadata(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a test result file
	testResult := statestore.RunResult{
		RunID:      "test-456",
		ScenarioID: "test-scenario",
		ProviderID: "openai",
		Region:     "us-east-1",
	}

	data, err := json.Marshal(testResult)
	require.NoError(t, err)

	filePath := filepath.Join(tmpDir, "test-456.json")
	err = os.WriteFile(filePath, data, 0644)
	require.NoError(t, err)

	page := NewFileBrowserPage(tmpDir)

	// Execute the load metadata command
	cmd := page.loadMetadata()

	// Should return a metadata message
	metaMsg, ok := cmd.(fileBrowserMetadataMsg)
	assert.True(t, ok)
	assert.Len(t, metaMsg.metadata, 1)
	assert.Equal(t, "test-456", metaMsg.metadata[0].RunID)
}

func TestFileBrowserPage_RenderKeyLegend(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)
	page.SetDimensions(100, 50)
	page.loading = false

	legend := page.renderKeyLegend()

	// Verify key legend contains essential keys
	assert.Contains(t, legend, "navigate")
	assert.Contains(t, legend, "back/open")
	assert.Contains(t, legend, "select file")
	assert.Contains(t, legend, "quit")
	assert.Contains(t, legend, "q/ctrl+c")
}

func TestFileBrowserPage_RenderIncludesKeyLegend(t *testing.T) {
	tmpDir := t.TempDir()
	page := NewFileBrowserPage(tmpDir)
	page.SetDimensions(100, 50)
	page.loading = false
	page.metadata = []reader.ResultMetadata{}

	view := page.Render()

	// Verify the full render includes the key legend
	assert.Contains(t, view, "quit")
	assert.Contains(t, view, "navigate")
}
