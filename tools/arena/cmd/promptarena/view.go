package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
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

func runView(cmd *cobra.Command, args []string) error {
	// Determine results directory
	resultsDir := "."
	if len(args) > 0 {
		resultsDir = args[0]
	}

	// Resolve to absolute path
	absPath, err := filepath.Abs(resultsDir)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	// Check if directory exists
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("directory does not exist: %s", absPath)
		}
		return fmt.Errorf("failed to access directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", absPath)
	}

	// Create and run the multi-page viewer TUI
	viewer := newViewerTUI(absPath)

	// Create the bubbletea program
	p := tea.NewProgram(
		viewer,
		tea.WithAltScreen(),
	)

	// Run the program
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("failed to run TUI: %w", err)
	}

	return nil
}

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
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case pages.FileSelectedMsg:
		// File selected - clear navigation stack and load result
		m.pageStack = []page{} // Clear navigation history for new file
		cmd := m.loadAndViewResult(msg.RunID, msg.Path)
		return m, cmd

	case resultLoadedMsg:
		// Result loaded successfully - switch to conversation view
		result := msg.result.(*statestore.RunResult)
		m.navigateTo(pageConversation)
		m.conversationPage.SetData(
			result.RunID,
			result.ScenarioID,
			result.ProviderID,
			result,
		)
		return m, nil

	case summaryLoadedMsg:
		// Summary loaded - switch to main page and trigger results loading
		m.navigateTo(pageMain)
		// Set initial empty state while loading
		m.mainPage.SetData([]panels.RunInfo{}, []panels.LogEntry{}, focusPanelRuns, nil)
		// Extract run IDs from summary and trigger async loading
		cmd := m.loadResultsFromSummary(msg.summary, msg.resultsDir)
		return m, cmd

	case resultsLoadedMsg:
		// All results loaded - populate main page
		m.loadedResults = msg.results // Store for selection
		_ = m.convertResultsToViewData(msg.results)
		// Update result pane with first result
		m.updateResultPane(0)
		return m, nil

	case resultLoadErrorMsg:
		// TODO: Display error in UI
		return m, nil

	case tea.KeyMsg:
		// Handle quit on all pages
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		// Handle Tab on main page to cycle focus
		if m.currentPage == pageMain && msg.String() == "tab" && m.mainPage != nil {
			switch m.mainPageFocus {
			case focusPanelRuns:
				m.mainPageFocus = focusPanelLogs
				m.mainPage.SetFocusedPanel(focusPanelLogs)
				if m.mainPage.LogsPanel() != nil {
					m.mainPage.LogsPanel().SetFocus(true)
				}
				if m.mainPage.RunsPanel() != nil {
					m.mainPage.RunsPanel().SetFocus(false)
				}
			case focusPanelLogs:
				m.mainPageFocus = focusPanelResult
				m.mainPage.SetFocusedPanel(focusPanelResult)
				if m.mainPage.ResultPanel() != nil {
					m.mainPage.ResultPanel().SetFocus(true)
				}
				if m.mainPage.LogsPanel() != nil {
					m.mainPage.LogsPanel().SetFocus(false)
				}
			case focusPanelResult:
				m.mainPageFocus = focusPanelRuns
				m.mainPage.SetFocusedPanel(focusPanelRuns)
				if m.mainPage.RunsPanel() != nil {
					m.mainPage.RunsPanel().SetFocus(true)
				}
				if m.mainPage.ResultPanel() != nil {
					m.mainPage.ResultPanel().SetFocus(false)
				}
			}
			return m, nil
		}

		// Handle Enter on main page to open conversation
		if m.currentPage == pageMain && msg.String() == "enter" {
			return m.handleRunSelection()
		}

		// Handle ESC to go back in navigation stack
		if msg.String() == "esc" && len(m.pageStack) > 0 {
			return m.navigateBack()
		}
	} // Forward message to current page
	var cmd tea.Cmd
	switch m.currentPage {
	case pageFileBrowser:
		_, cmd = m.fileBrowserPage.Update(msg)
	case pageConversation:
		cmd = m.conversationPage.Update(msg)
	case pageMain:
		// Forward message to focused panel
		if m.mainPage != nil {
			switch m.mainPageFocus {
			case "runs":
				if m.mainPage.RunsPanel() != nil {
					table := m.mainPage.RunsPanel().Table()
					if table != nil {
						oldCursor := table.Cursor()
						*table, cmd = table.Update(msg)
						newCursor := table.Cursor()

						// Update result pane if cursor moved
						if oldCursor != newCursor {
							m.updateResultPane(newCursor)
						}
					}
				}
			case "logs":
				if m.mainPage.LogsPanel() != nil {
					viewport := m.mainPage.LogsPanel().Viewport()
					if viewport != nil {
						*viewport, cmd = viewport.Update(msg)
					}
				}
			case "result":
				if m.mainPage.ResultPanel() != nil {
					viewport := m.mainPage.ResultPanel().Viewport()
					if viewport != nil {
						*viewport, cmd = viewport.Update(msg)
					}
				}
			}
		}
	}

	return m, cmd
}

// View renders the current page with consistent header and footer using shared chrome
func (m *viewerTUI) View() string {
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
