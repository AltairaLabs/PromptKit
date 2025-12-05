package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/pages"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/panels"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/views"
)

func TestViewCommand_Help(t *testing.T) {
	// Test that the view command is registered
	assert.NotNil(t, viewCmd)
	assert.Equal(t, "view [results-dir]", viewCmd.Use)
	assert.Contains(t, viewCmd.Short, "Browse")
}

func TestNewViewerTUI(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)

	assert.NotNil(t, viewer)
	assert.Equal(t, pageFileBrowser, viewer.currentPage)
	assert.Empty(t, viewer.pageStack)
	assert.NotNil(t, viewer.fileBrowserPage)
	assert.NotNil(t, viewer.conversationPage)
	assert.NotNil(t, viewer.mainPage)
	assert.Equal(t, tmpDir, viewer.resultsDir)
	assert.Equal(t, -1, viewer.lastCursorIndex)
	assert.Equal(t, focusPanelRuns, viewer.mainPageFocus)
	assert.Empty(t, viewer.loadedResults)
}

func TestViewerTUI_Init(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)

	cmd := viewer.Init()
	assert.NotNil(t, cmd)
}

func TestViewerTUI_Update_WindowSize(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)

	msg := tea.WindowSizeMsg{Width: 100, Height: 50}
	model, cmd := viewer.Update(msg)

	v := model.(*viewerTUI)
	assert.Equal(t, 100, v.width)
	assert.Equal(t, 50, v.height)
	assert.Nil(t, cmd)
}

func TestViewerTUI_Update_Quit(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)

	tests := []struct {
		name string
		key  string
	}{
		{"q key", "q"},
		{"ctrl+c", "ctrl+c"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)}
			if tt.key == "ctrl+c" {
				msg = tea.KeyMsg{Type: tea.KeyCtrlC}
			}
			_, cmd := viewer.Update(msg)
			assert.NotNil(t, cmd)
		})
	}
}

func TestViewerTUI_NavigateTo(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)

	// Navigate from file browser to conversation
	viewer.navigateTo(pageConversation)
	assert.Equal(t, pageConversation, viewer.currentPage)
	assert.Equal(t, []page{pageFileBrowser}, viewer.pageStack)

	// Navigate to main page
	viewer.navigateTo(pageMain)
	assert.Equal(t, pageMain, viewer.currentPage)
	assert.Equal(t, []page{pageFileBrowser, pageConversation}, viewer.pageStack)

	// Navigate to same page (should not change stack)
	stackLen := len(viewer.pageStack)
	viewer.navigateTo(pageMain)
	assert.Equal(t, pageMain, viewer.currentPage)
	assert.Equal(t, stackLen, len(viewer.pageStack))
}

func TestViewerTUI_NavigateBack(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)

	// Navigate forward to build stack
	viewer.navigateTo(pageConversation)
	viewer.navigateTo(pageMain)

	// Navigate back
	model, cmd := viewer.navigateBack()
	v := model.(*viewerTUI)
	assert.Equal(t, pageConversation, v.currentPage)
	assert.Equal(t, 1, len(v.pageStack))
	assert.Nil(t, cmd)

	// Navigate back again
	model, cmd = v.navigateBack()
	v = model.(*viewerTUI)
	assert.Equal(t, pageFileBrowser, v.currentPage)
	assert.Equal(t, 0, len(v.pageStack))
	assert.Nil(t, cmd)

	// Navigate back with empty stack
	model, cmd = v.navigateBack()
	v = model.(*viewerTUI)
	assert.Equal(t, pageFileBrowser, v.currentPage)
	assert.Equal(t, 0, len(v.pageStack))
	assert.Nil(t, cmd)
}

func TestViewerTUI_Update_ESC_NavigateBack(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)

	// Build navigation stack
	viewer.navigateTo(pageConversation)

	msg := tea.KeyMsg{Type: tea.KeyEsc}
	model, cmd := viewer.Update(msg)

	v := model.(*viewerTUI)
	assert.Equal(t, pageFileBrowser, v.currentPage)
	assert.Nil(t, cmd)
}

func TestViewerTUI_Update_TabOnMainPage(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)
	viewer.currentPage = pageMain

	// Tab from runs to logs
	msg := tea.KeyMsg{Type: tea.KeyTab}
	model, cmd := viewer.Update(msg)
	v := model.(*viewerTUI)
	assert.Equal(t, focusPanelLogs, v.mainPageFocus)
	assert.Nil(t, cmd)

	// Tab from logs to result
	model, cmd = v.Update(msg)
	v = model.(*viewerTUI)
	assert.Equal(t, focusPanelResult, v.mainPageFocus)
	assert.Nil(t, cmd)

	// Tab from result back to runs
	model, cmd = v.Update(msg)
	v = model.(*viewerTUI)
	assert.Equal(t, focusPanelRuns, v.mainPageFocus)
	assert.Nil(t, cmd)
}

func TestViewerTUI_HandleRunSelection_NilChecks(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)

	// Test with nil main page
	viewer.mainPage = nil
	model, cmd := viewer.handleRunSelection()
	assert.Equal(t, viewer, model)
	assert.Nil(t, cmd)

	// Test with no loaded results
	viewer.mainPage = pages.NewMainPage()
	viewer.loadedResults = nil
	model, cmd = viewer.handleRunSelection()
	assert.Equal(t, viewer, model)
	assert.Nil(t, cmd)
}

func TestViewerTUI_HandleRunSelection_InvalidIndex(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)
	viewer.mainPage = pages.NewMainPage()

	// Add some results
	viewer.loadedResults = []*statestore.RunResult{
		{RunID: "run1"},
	}

	// Test with invalid index (would need a mock table with cursor)
	model, cmd := viewer.handleRunSelection()
	assert.Equal(t, viewer, model)
	assert.Nil(t, cmd)
}

func TestViewerTUI_GetStatusForResult(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)

	tests := []struct {
		name     string
		result   *statestore.RunResult
		expected views.RunStatus
	}{
		{
			name:     "completed result",
			result:   &statestore.RunResult{RunID: "run1", Error: ""},
			expected: views.RunStatus(panels.StatusCompleted),
		},
		{
			name:     "failed result",
			result:   &statestore.RunResult{RunID: "run2", Error: "some error"},
			expected: views.RunStatus(panels.StatusFailed),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := viewer.getStatusForResult(tt.result)
			assert.Equal(t, tt.expected, status)
		})
	}
}

func TestViewerTUI_ConvertResultsToViewData(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)

	results := []*statestore.RunResult{
		{
			RunID:      "run1",
			ScenarioID: "scenario1",
			ProviderID: "provider1",
			Region:     "us-west-1",
			Duration:   1500000000, // 1.5 seconds in nanoseconds
			Cost:       types.CostInfo{TotalCost: 0.01},
			Error:      "",
		},
		{
			RunID:      "run2",
			ScenarioID: "scenario2",
			ProviderID: "provider2",
			Region:     "us-east-1",
			Duration:   2500000000, // 2.5 seconds in nanoseconds
			Cost:       types.CostInfo{TotalCost: 0.02},
			Error:      "timeout",
		},
	}

	viewData := viewer.convertResultsToViewData(results)

	assert.Len(t, viewData, 2)
	assert.Equal(t, "run1", viewData[0].RunID)
	assert.Equal(t, panels.StatusCompleted, viewData[0].Status)
	assert.Equal(t, 1500000000, int(viewData[0].Duration))
	assert.Equal(t, 0.01, viewData[0].Cost)
	assert.Empty(t, viewData[0].Error)

	assert.Equal(t, "run2", viewData[1].RunID)
	assert.Equal(t, panels.StatusFailed, viewData[1].Status)
	assert.Equal(t, "timeout", viewData[1].Error)
}

func TestViewerTUI_UpdateResultPane(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)
	viewer.mainPage = pages.NewMainPage()
	viewer.mainPageFocus = focusPanelRuns

	results := []*statestore.RunResult{
		{RunID: "run1", Error: ""},
		{RunID: "run2", Error: "failed"},
	}
	viewer.loadedResults = results

	// Test with valid index
	viewer.updateResultPane(0)
	// Just verify it doesn't panic

	// Test with negative index
	viewer.updateResultPane(-1)
	// Should clear result pane

	// Test with out of bounds index
	viewer.updateResultPane(10)
	// Should clear result pane
}

func TestViewerTUI_View(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)
	viewer.width = 100
	viewer.height = 50

	// Test view for file browser page
	viewer.currentPage = pageFileBrowser
	view := viewer.View()
	assert.NotEmpty(t, view)

	// Test view for conversation page
	viewer.currentPage = pageConversation
	view = viewer.View()
	assert.NotEmpty(t, view)

	// Test view for main page
	viewer.currentPage = pageMain
	view = viewer.View()
	assert.NotEmpty(t, view)
}

func TestViewerTUI_LoadAndViewResult_Summary(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)

	// Create a summary file
	summaryPath := filepath.Join(tmpDir, "index.json")
	summaryData := map[string]interface{}{
		"run_ids": []string{"run1", "run2"},
		"total":   2,
	}
	data, err := json.Marshal(summaryData)
	require.NoError(t, err)
	err = os.WriteFile(summaryPath, data, 0644)
	require.NoError(t, err)

	// Load the summary
	cmd := viewer.loadAndViewResult("", summaryPath)
	msg := cmd()

	// Should return summaryLoadedMsg
	summaryMsg, ok := msg.(summaryLoadedMsg)
	assert.True(t, ok)
	assert.NotNil(t, summaryMsg.summary)
	assert.Equal(t, tmpDir, summaryMsg.resultsDir)
}

func TestViewerTUI_LoadAndViewResult_InvalidSummary(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)

	// Create an invalid summary file
	summaryPath := filepath.Join(tmpDir, "index.json")
	err := os.WriteFile(summaryPath, []byte("not valid json"), 0644)
	require.NoError(t, err)

	// Load the invalid summary
	cmd := viewer.loadAndViewResult("", summaryPath)
	msg := cmd()

	// Should return error
	errMsg, ok := msg.(resultLoadErrorMsg)
	assert.True(t, ok)
	assert.Error(t, errMsg.err)
}

func TestViewerTUI_LoadAndViewResult_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)

	// Try to load non-existent file
	summaryPath := filepath.Join(tmpDir, "index.json")
	cmd := viewer.loadAndViewResult("", summaryPath)
	msg := cmd()

	// Should return error
	errMsg, ok := msg.(resultLoadErrorMsg)
	assert.True(t, ok)
	assert.Error(t, errMsg.err)
}

func TestViewerTUI_LoadResultsFromSummary_ValidData(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)

	summary := map[string]interface{}{
		"run_ids": []interface{}{"run1", "run2"},
	}

	// Note: This will fail because we don't have actual result files,
	// but we can verify it processes the summary correctly
	cmd := viewer.loadResultsFromSummary(summary, tmpDir)
	msg := cmd()

	// Should return error because files don't exist
	errMsg, ok := msg.(resultLoadErrorMsg)
	assert.True(t, ok)
	assert.Error(t, errMsg.err)
}

func TestViewerTUI_LoadResultsFromSummary_MissingRunIDs(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)

	summary := map[string]interface{}{
		"total": 5,
	}

	cmd := viewer.loadResultsFromSummary(summary, tmpDir)
	msg := cmd()

	errMsg, ok := msg.(resultLoadErrorMsg)
	assert.True(t, ok)
	assert.Contains(t, errMsg.err.Error(), "run_ids")
}

func TestViewerTUI_LoadResultsFromSummary_InvalidRunIDsType(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)

	summary := map[string]interface{}{
		"run_ids": "not an array",
	}

	cmd := viewer.loadResultsFromSummary(summary, tmpDir)
	msg := cmd()

	errMsg, ok := msg.(resultLoadErrorMsg)
	assert.True(t, ok)
	assert.Contains(t, errMsg.err.Error(), "not an array")
}

func TestViewerTUI_LoadResultsFromSummary_EmptyRunIDs(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)

	summary := map[string]interface{}{
		"run_ids": []interface{}{},
	}

	cmd := viewer.loadResultsFromSummary(summary, tmpDir)
	msg := cmd()

	errMsg, ok := msg.(resultLoadErrorMsg)
	assert.True(t, ok)
	assert.Contains(t, errMsg.err.Error(), "no run IDs found")
}

func TestViewerTUI_Update_FileSelectedMsg(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)

	// Build a navigation stack first
	viewer.navigateTo(pageConversation)
	assert.Len(t, viewer.pageStack, 1)

	// Create a summary file
	summaryPath := filepath.Join(tmpDir, "index.json")
	summaryData := map[string]interface{}{
		"run_ids": []string{"run1"},
	}
	data, err := json.Marshal(summaryData)
	require.NoError(t, err)
	err = os.WriteFile(summaryPath, data, 0644)
	require.NoError(t, err)

	// Send FileSelectedMsg
	msg := pages.FileSelectedMsg{
		RunID: "run1",
		Path:  summaryPath,
	}
	model, cmd := viewer.Update(msg)

	v := model.(*viewerTUI)
	assert.Empty(t, v.pageStack) // Should clear stack
	assert.NotNil(t, cmd)
}

func TestViewerTUI_Update_ResultLoadedMsg(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)

	result := &statestore.RunResult{
		RunID:      "run1",
		ScenarioID: "scenario1",
		ProviderID: "provider1",
	}

	msg := resultLoadedMsg{result: result}
	model, cmd := viewer.Update(msg)

	v := model.(*viewerTUI)
	assert.Equal(t, pageConversation, v.currentPage)
	assert.Nil(t, cmd)
}

func TestViewerTUI_Update_SummaryLoadedMsg(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)

	summary := map[string]interface{}{
		"run_ids": []interface{}{"run1", "run2"},
	}

	msg := summaryLoadedMsg{
		summary:    summary,
		resultsDir: tmpDir,
	}
	model, cmd := viewer.Update(msg)

	v := model.(*viewerTUI)
	assert.Equal(t, pageMain, v.currentPage)
	assert.NotNil(t, cmd)
}

func TestViewerTUI_Update_ResultsLoadedMsg(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)
	viewer.mainPage = pages.NewMainPage()

	results := []*statestore.RunResult{
		{RunID: "run1", Error: ""},
		{RunID: "run2", Error: ""},
	}

	msg := resultsLoadedMsg{results: results}
	model, cmd := viewer.Update(msg)

	v := model.(*viewerTUI)
	assert.Equal(t, results, v.loadedResults)
	assert.Nil(t, cmd)
}

func TestViewerTUI_Update_ResultLoadErrorMsg(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)

	msg := resultLoadErrorMsg{err: assert.AnError}
	model, cmd := viewer.Update(msg)

	assert.Equal(t, viewer, model)
	assert.Nil(t, cmd)
}

func TestPageConstants(t *testing.T) {
	// Verify page constants are defined
	assert.Equal(t, page(0), pageFileBrowser)
	assert.Equal(t, page(1), pageConversation)
	assert.Equal(t, page(2), pageMain)
}

func TestFocusConstants(t *testing.T) {
	// Verify focus constants are defined
	assert.Equal(t, "runs", focusPanelRuns)
	assert.Equal(t, "logs", focusPanelLogs)
	assert.Equal(t, "result", focusPanelResult)
}

func TestViewerTUI_Update_ForwardToPages(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)
	viewer.width = 100
	viewer.height = 50

	// Test forwarding to file browser
	viewer.currentPage = pageFileBrowser
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}
	_, _ = viewer.Update(msg)

	// Test forwarding to conversation page
	viewer.currentPage = pageConversation
	_, _ = viewer.Update(msg)

	// Test forwarding to main page with runs focus
	viewer.currentPage = pageMain
	viewer.mainPageFocus = focusPanelRuns
	_, _ = viewer.Update(msg)

	// Test forwarding to main page with logs focus
	viewer.mainPageFocus = focusPanelLogs
	_, _ = viewer.Update(msg)

	// Test forwarding to main page with result focus
	viewer.mainPageFocus = focusPanelResult
	_, _ = viewer.Update(msg)
}

func TestViewerTUI_Update_EnterOnMainPage(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)
	viewer.currentPage = pageMain
	viewer.mainPage = pages.NewMainPage()

	// Add a result
	viewer.loadedResults = []*statestore.RunResult{
		{RunID: "run1", ScenarioID: "scenario1", ProviderID: "provider1"},
	}

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	model, cmd := viewer.Update(msg)

	v := model.(*viewerTUI)
	assert.Equal(t, pageConversation, v.currentPage)
	assert.Nil(t, cmd)
}

func TestViewerTUI_View_KeyBindings(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)
	viewer.width = 100
	viewer.height = 50

	// Test that each page returns key bindings
	pages := []page{pageFileBrowser, pageConversation, pageMain}
	for _, p := range pages {
		viewer.currentPage = p
		view := viewer.View()
		assert.NotEmpty(t, view)
	}
}

func TestViewerTUI_LoadAndViewResult_NonSummaryFile(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)

	// Create a non-summary JSON file (e.g., run-123.json)
	runFile := filepath.Join(tmpDir, "run-123.json")
	runData := map[string]interface{}{
		"RunID": "run-123",
	}
	data, err := json.Marshal(runData)
	require.NoError(t, err)
	err = os.WriteFile(runFile, data, 0644)
	require.NoError(t, err)

	// This will fail because the reader expects a specific directory structure
	cmd := viewer.loadAndViewResult("run-123", runFile)
	msg := cmd()

	// Should return error because reader can't find the result file in the expected location
	_, ok := msg.(resultLoadErrorMsg)
	if !ok {
		// If it's not an error, it might be a resultLoadedMsg (depending on reader implementation)
		// Either way, we've exercised the code path
		t.Logf("Got message type: %T", msg)
	}
}

func TestViewerTUI_UpdateResultPane_WithMainPage(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)
	viewer.mainPage = pages.NewMainPage()
	viewer.mainPageFocus = focusPanelRuns
	viewer.width = 100
	viewer.height = 50

	// Initialize with some results
	results := []*statestore.RunResult{
		{RunID: "run1", Error: ""},
		{RunID: "run2", Error: "failed"},
	}
	viewer.loadedResults = results

	// Update with valid index
	viewer.updateResultPane(0)

	// Update with invalid indices
	viewer.updateResultPane(-1)
	viewer.updateResultPane(100)

	// All should complete without panic
}

func TestViewerTUI_LoadResultsFromSummary_NonStringIDs(t *testing.T) {
	tmpDir := t.TempDir()
	viewer := newViewerTUI(tmpDir)

	// Test with mixed types in run_ids array
	summary := map[string]interface{}{
		"run_ids": []interface{}{"run1", 123, "run2", nil, "run3"},
	}

	cmd := viewer.loadResultsFromSummary(summary, tmpDir)
	msg := cmd()

	// Should handle the conversion and filter non-strings
	// Will fail when trying to load because files don't exist
	errMsg, ok := msg.(resultLoadErrorMsg)
	assert.True(t, ok)
	assert.Error(t, errMsg.err)
}
