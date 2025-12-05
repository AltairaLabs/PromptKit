package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/tools/arena/reader/filesystem"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/pages"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/panels"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/views"
)

var viewCmd = &cobra.Command{
	Use:   "view [results-dir]",
	Short: "Browse and view past Arena test results",
	Long: `View opens a TUI file browser for exploring past Arena test results.

You can browse JSON result files, preview result metadata, and view full 
conversation details for any completed test run.

If no directory is specified, it defaults to the current working directory.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runView,
}

func init() {
	rootCmd.AddCommand(viewCmd)
}

// NOTE: runView is defined in view_interactive.go
// This function is excluded from coverage testing as it involves interactive terminal operations.

// page constants for the viewer TUI
type page int

const (
	pageFileBrowser page = iota
	pageConversation
	pageMain // For summary/index files
)

// Panel focus constants
const (
	focusPanelRuns   = "runs"
	focusPanelLogs   = "logs"
	focusPanelResult = "result"
)

// Error overlay styling constants
const (
	errorOverlayPaddingHorizontal = 2
	errorOverlayBorderPadding     = 4
	errorOverlayMaxWidth          = 80
)

// viewerTUI manages navigation between file browser and conversation view
type viewerTUI struct {
	currentPage      page
	pageStack        []page // Navigation history for back button
	fileBrowserPage  *pages.FileBrowserPage
	conversationPage *pages.ConversationPage
	mainPage         *pages.MainPage
	loadedResults    []*statestore.RunResult // Store loaded results for selection
	lastCursorIndex  int                     // Track cursor position for result pane updates
	mainPageFocus    string                  // Track which panel has focus on main page ("runs", "logs", "result")
	lastError        error                   // Store last error for display
	resultsDir       string
	width            int
	height           int
}

// newViewerTUI creates a new viewer TUI with file browser and conversation pages
func newViewerTUI(resultsDir string) *viewerTUI {
	return &viewerTUI{
		currentPage:      pageFileBrowser,
		pageStack:        []page{},
		fileBrowserPage:  pages.NewFileBrowserPage(resultsDir),
		conversationPage: pages.NewConversationPage(),
		mainPage:         pages.NewMainPage(),
		resultsDir:       resultsDir,
		lastCursorIndex:  -1,
		mainPageFocus:    focusPanelRuns,
	}
}

// Init initializes the viewer TUI
func (m *viewerTUI) Init() tea.Cmd {
	return m.fileBrowserPage.Init()
}

// Update handles messages and page switching
func (m *viewerTUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleWindowSize(msg)
	case pages.FileSelectedMsg:
		return m.handleFileSelected(msg)
	case resultLoadedMsg:
		return m.handleResultLoaded(msg)
	case summaryLoadedMsg:
		return m.handleSummaryLoaded(msg)
	case resultsLoadedMsg:
		return m.handleResultsLoaded(msg)
	case resultLoadErrorMsg:
		return m.handleResultLoadError(msg)
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	}

	// Forward message to current page
	return m.forwardToCurrentPage(msg)
}

// handleWindowSize updates dimensions
func (m *viewerTUI) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	return m, nil
}

// handleFileSelected loads the selected file
func (m *viewerTUI) handleFileSelected(msg pages.FileSelectedMsg) (tea.Model, tea.Cmd) {
	m.pageStack = []page{} // Clear navigation history for new file
	cmd := m.loadAndViewResult(msg.RunID, msg.Path)
	return m, cmd
}

// handleResultLoaded switches to conversation view with loaded result
func (m *viewerTUI) handleResultLoaded(msg resultLoadedMsg) (tea.Model, tea.Cmd) {
	result := msg.result.(*statestore.RunResult)
	m.navigateTo(pageConversation)
	m.conversationPage.SetData(
		result.RunID,
		result.ScenarioID,
		result.ProviderID,
		result,
	)
	return m, nil
}

// handleSummaryLoaded switches to main page and triggers results loading
func (m *viewerTUI) handleSummaryLoaded(msg summaryLoadedMsg) (tea.Model, tea.Cmd) {
	m.navigateTo(pageMain)
	m.mainPage.SetData([]panels.RunInfo{}, []panels.LogEntry{}, focusPanelRuns, nil)
	cmd := m.loadResultsFromSummary(msg.summary, msg.resultsDir)
	return m, cmd
}

// handleResultsLoaded populates main page with loaded results
func (m *viewerTUI) handleResultsLoaded(msg resultsLoadedMsg) (tea.Model, tea.Cmd) {
	m.loadedResults = msg.results
	m.lastError = nil // Clear any previous errors
	_ = m.convertResultsToViewData(msg.results)
	m.updateResultPane(0)
	return m, nil
}

// handleResultLoadError stores error for display
func (m *viewerTUI) handleResultLoadError(msg resultLoadErrorMsg) (tea.Model, tea.Cmd) {
	m.lastError = msg.err
	// Stay on current page and let View() display the error
	return m, nil
}

// handleKeyMsg processes keyboard input
func (m *viewerTUI) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// If error is displayed, any key dismisses it
	if m.lastError != nil {
		m.lastError = nil
		return m, nil
	}

	// Global quit
	if key == "q" || key == "ctrl+c" {
		return m, tea.Quit
	}

	// Tab on main page cycles focus
	if m.currentPage == pageMain && key == "tab" {
		return m.cycleFocus()
	}

	// Enter on main page opens conversation
	if m.currentPage == pageMain && key == "enter" {
		return m.handleRunSelection()
	}

	// ESC goes back
	if key == "esc" && len(m.pageStack) > 0 {
		return m.navigateBack()
	}

	// Forward to current page
	return m.forwardToCurrentPage(msg)
}

// cycleFocus cycles through panel focus on main page
func (m *viewerTUI) cycleFocus() (tea.Model, tea.Cmd) {
	if m.mainPage == nil {
		return m, nil
	}

	nextFocus := map[string]string{
		focusPanelRuns:   focusPanelLogs,
		focusPanelLogs:   focusPanelResult,
		focusPanelResult: focusPanelRuns,
	}

	m.mainPageFocus = nextFocus[m.mainPageFocus]
	m.mainPage.SetFocusedPanel(m.mainPageFocus)
	m.updatePanelFocus()
	return m, nil
}

// updatePanelFocus updates focus state for all panels
func (m *viewerTUI) updatePanelFocus() {
	if m.mainPage == nil {
		return
	}

	// Set focus for each panel
	if panel := m.mainPage.RunsPanel(); panel != nil {
		panel.SetFocus(m.mainPageFocus == focusPanelRuns)
	}
	if panel := m.mainPage.LogsPanel(); panel != nil {
		panel.SetFocus(m.mainPageFocus == focusPanelLogs)
	}
	if panel := m.mainPage.ResultPanel(); panel != nil {
		panel.SetFocus(m.mainPageFocus == focusPanelResult)
	}
}

// forwardToCurrentPage forwards messages to the active page
func (m *viewerTUI) forwardToCurrentPage(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch m.currentPage {
	case pageFileBrowser:
		_, cmd = m.fileBrowserPage.Update(msg)
	case pageConversation:
		cmd = m.conversationPage.Update(msg)
	case pageMain:
		cmd = m.updateMainPage(msg)
	}

	return m, cmd
}

// updateMainPage updates the focused panel on main page
func (m *viewerTUI) updateMainPage(msg tea.Msg) tea.Cmd {
	if m.mainPage == nil {
		return nil
	}

	switch m.mainPageFocus {
	case focusPanelRuns:
		return m.updateRunsPanel(msg)
	case focusPanelLogs:
		return m.updateLogsPanel(msg)
	case focusPanelResult:
		return m.updateResultPanel(msg)
	}

	return nil
}

// updateRunsPanel updates the runs panel and tracks cursor changes
func (m *viewerTUI) updateRunsPanel(msg tea.Msg) tea.Cmd {
	panel := m.mainPage.RunsPanel()
	if panel == nil {
		return nil
	}

	table := panel.Table()
	if table == nil {
		return nil
	}

	oldCursor := table.Cursor()
	var cmd tea.Cmd
	*table, cmd = table.Update(msg)
	newCursor := table.Cursor()

	if oldCursor != newCursor {
		m.updateResultPane(newCursor)
	}

	return cmd
}

// updateLogsPanel updates the logs panel viewport
func (m *viewerTUI) updateLogsPanel(msg tea.Msg) tea.Cmd {
	panel := m.mainPage.LogsPanel()
	if panel == nil {
		return nil
	}

	viewport := panel.Viewport()
	if viewport == nil {
		return nil
	}

	var cmd tea.Cmd
	*viewport, cmd = viewport.Update(msg)
	return cmd
}

// updateResultPanel updates the result panel viewport
func (m *viewerTUI) updateResultPanel(msg tea.Msg) tea.Cmd {
	panel := m.mainPage.ResultPanel()
	if panel == nil {
		return nil
	}

	viewport := panel.Viewport()
	if viewport == nil {
		return nil
	}

	var cmd tea.Cmd
	*viewport, cmd = viewport.Update(msg)
	return cmd
}

// View renders the current page with consistent header and footer using shared chrome
func (m *viewerTUI) View() string {
	// If there's an error, display it as an overlay
	if m.lastError != nil {
		return m.renderErrorOverlay()
	}

	// Get key bindings from current page
	var keyBindings []views.KeyBinding
	switch m.currentPage {
	case pageFileBrowser:
		keyBindings = m.fileBrowserPage.GetKeyBindings()
	case pageConversation:
		keyBindings = m.conversationPage.GetKeyBindings()
	case pageMain:
		keyBindings = []views.KeyBinding{
			{Keys: "q", Description: "quit"},
			{Keys: "esc", Description: "back"},
			{Keys: "tab", Description: "cycle focus"},
			{Keys: "enter", Description: "open conversation"},
			{Keys: "↑/↓", Description: "navigate/scroll"},
		}
	}

	return views.RenderWithChrome(
		views.ChromeConfig{
			Width:          m.width,
			Height:         m.height,
			ConfigFile:     "Results Viewer",
			CompletedCount: 0,
			TotalRuns:      0,
			Elapsed:        0,
			KeyBindings:    keyBindings,
		},
		func(contentHeight int) string {
			switch m.currentPage {
			case pageFileBrowser:
				m.fileBrowserPage.SetDimensions(m.width, contentHeight)
				return m.fileBrowserPage.Render()
			case pageConversation:
				m.conversationPage.SetDimensions(m.width, contentHeight)
				return m.conversationPage.Render()
			case pageMain:
				m.mainPage.SetDimensions(m.width, contentHeight)
				return m.mainPage.Render()
			default:
				return ""
			}
		},
	)
}

// loadAndViewResult loads a result and switches to appropriate view (conversation or main)
func (m *viewerTUI) loadAndViewResult(runID, path string) tea.Cmd {
	return func() tea.Msg {
		// Check if this is a summary file (index.json)
		if filepath.Base(path) == "index.json" {
			// Load summary data from index.json
			//nolint:gosec // Path is validated and comes from trusted file browser selection
			data, err := os.ReadFile(path)
			if err != nil {
				return resultLoadErrorMsg{err: fmt.Errorf("failed to load summary: %w", err)}
			}

			var summary map[string]interface{}
			if err := json.Unmarshal(data, &summary); err != nil {
				return resultLoadErrorMsg{err: fmt.Errorf("failed to parse summary: %w", err)}
			}

			return summaryLoadedMsg{summary: summary, resultsDir: m.resultsDir}
		}

		// Load individual run result
		reader := filesystem.NewFilesystemResultReader(m.resultsDir)
		result, err := reader.LoadResult(runID)
		if err != nil {
			return resultLoadErrorMsg{err: err}
		}
		return resultLoadedMsg{result: result}
	}
}

// resultLoadedMsg is sent when a single run result has been loaded
type resultLoadedMsg struct {
	result interface{} // Using interface{} to avoid importing statestore here
}

// summaryLoadedMsg is sent when a summary file has been loaded
type summaryLoadedMsg struct {
	summary    map[string]interface{}
	resultsDir string
}

// resultsLoadedMsg is sent when all results have been loaded for summary view
type resultsLoadedMsg struct {
	results []*statestore.RunResult
}

// resultLoadErrorMsg is sent when result loading fails
type resultLoadErrorMsg struct {
	err error
}

// loadResultsFromSummary loads individual results based on run_ids from summary file
func (m *viewerTUI) loadResultsFromSummary(summary map[string]interface{}, resultsDir string) tea.Cmd {
	return func() tea.Msg {
		// Extract run_ids from summary
		runIDsInterface, ok := summary["run_ids"]
		if !ok {
			return resultLoadErrorMsg{err: fmt.Errorf("summary missing run_ids field")}
		}

		runIDsSlice, ok := runIDsInterface.([]interface{})
		if !ok {
			return resultLoadErrorMsg{err: fmt.Errorf("run_ids is not an array")}
		}

		// Convert to string slice
		runIDs := make([]string, 0, len(runIDsSlice))
		for _, id := range runIDsSlice {
			if strID, ok := id.(string); ok {
				runIDs = append(runIDs, strID)
			}
		}

		if len(runIDs) == 0 {
			return resultLoadErrorMsg{err: fmt.Errorf("no run IDs found in summary")}
		}

		// Load each result
		reader := filesystem.NewFilesystemResultReader(resultsDir)
		results, err := reader.LoadResults(runIDs)
		if err != nil {
			return resultLoadErrorMsg{err: fmt.Errorf("failed to load results: %w", err)}
		}

		return resultsLoadedMsg{results: results}
	}
} // handleRunSelection handles Enter key on main page to open selected run
func (m *viewerTUI) handleRunSelection() (tea.Model, tea.Cmd) {
	if m.mainPage == nil || m.mainPage.RunsPanel() == nil {
		return m, nil
	}

	table := m.mainPage.RunsPanel().Table()
	if table == nil {
		return m, nil
	}

	idx := table.Cursor()
	if idx < 0 || idx >= len(m.loadedResults) {
		return m, nil
	}

	result := m.loadedResults[idx]

	// Switch to conversation page and set data
	m.navigateTo(pageConversation)
	m.conversationPage.SetData(
		result.RunID,
		result.ScenarioID,
		result.ProviderID,
		result,
	)

	return m, nil
}

// navigateTo switches to a new page and pushes current page to stack
func (m *viewerTUI) navigateTo(newPage page) {
	if m.currentPage != newPage {
		m.pageStack = append(m.pageStack, m.currentPage)
		m.currentPage = newPage
	}
}

// navigateBack pops the navigation stack and returns to previous page
func (m *viewerTUI) navigateBack() (tea.Model, tea.Cmd) {
	if len(m.pageStack) == 0 {
		return m, nil
	}

	// Pop the last page from stack
	prevPage := m.pageStack[len(m.pageStack)-1]
	m.pageStack = m.pageStack[:len(m.pageStack)-1]
	m.currentPage = prevPage

	// If returning to file browser, reset its selection state
	if prevPage == pageFileBrowser {
		m.fileBrowserPage.Reset()
	}

	return m, nil
}

// updateResultPane updates the result pane when cursor changes on main page
func (m *viewerTUI) updateResultPane(cursorIdx int) {
	if cursorIdx < 0 || cursorIdx >= len(m.loadedResults) {
		// Clear result pane
		m.mainPage.SetData(
			m.convertResultsToViewData(m.loadedResults),
			[]panels.LogEntry{},
			m.mainPageFocus,
			nil,
		)
		return
	}

	result := m.loadedResults[cursorIdx]
	resultData := &panels.ResultPanelData{
		Result: result,
		Status: m.getStatusForResult(result),
	}

	m.mainPage.SetData(
		m.convertResultsToViewData(m.loadedResults),
		[]panels.LogEntry{},
		m.mainPageFocus,
		resultData,
	)
}

// getStatusForResult converts result error state to RunStatus
func (m *viewerTUI) getStatusForResult(result *statestore.RunResult) views.RunStatus {
	if result.Error != "" {
		return views.RunStatus(panels.StatusFailed)
	}
	return views.RunStatus(panels.StatusCompleted)
}

// convertResultsToViewData converts loaded results to viewmodel format for MainPanel
func (m *viewerTUI) convertResultsToViewData(results []*statestore.RunResult) []panels.RunInfo {
	runs := make([]panels.RunInfo, len(results))
	for i, result := range results {
		status := panels.StatusCompleted
		if result.Error != "" {
			status = panels.StatusFailed
		}

		runs[i] = panels.RunInfo{
			RunID:    result.RunID,
			Scenario: result.ScenarioID,
			Provider: result.ProviderID,
			Region:   result.Region,
			Status:   status,
			Duration: result.Duration,
			Cost:     result.Cost.TotalCost,
			Error:    result.Error,
		}
	}
	return runs
}

// renderErrorOverlay renders an error message overlay
func (m *viewerTUI) renderErrorOverlay() string {
	errorStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("9")).
		Padding(1, errorOverlayPaddingHorizontal).
		Width(m.width - errorOverlayBorderPadding).
		MaxWidth(errorOverlayMaxWidth)

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("9")).
		Render("Error")

	message := fmt.Sprintf("%s\n\n%s\n\nPress any key to continue", title, m.lastError.Error())

	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		errorStyle.Render(message),
	)
}
