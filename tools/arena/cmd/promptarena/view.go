package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/pages"
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
		tea.WithMouseCellMotion(),
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
)

// viewerTUI manages navigation between file browser and conversation view
type viewerTUI struct {
	currentPage      page
	fileBrowserPage  *pages.FileBrowserPage
	conversationPage *pages.ConversationPage
	width            int
	height           int
}

// newViewerTUI creates a new viewer TUI with file browser and conversation pages
func newViewerTUI(resultsDir string) *viewerTUI {
	return &viewerTUI{
		currentPage:      pageFileBrowser,
		fileBrowserPage:  pages.NewFileBrowserPage(resultsDir),
		conversationPage: pages.NewConversationPage(),
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
		m.fileBrowserPage.SetDimensions(msg.Width, msg.Height)
		m.conversationPage.SetDimensions(msg.Width, msg.Height)
		return m, nil

	case pages.ViewResultMsg:
		// Switch to conversation view with the selected result
		m.currentPage = pageConversation
		m.conversationPage.SetData(
			msg.Result.RunID,
			msg.Result.ScenarioID,
			msg.Result.ProviderID,
			msg.Result,
		)
		return m, nil

	case tea.KeyMsg:
		// Allow ESC to go back to file browser from conversation view
		if m.currentPage == pageConversation && msg.String() == "esc" {
			m.currentPage = pageFileBrowser
			return m, nil
		}
	}

	// Forward message to current page
	var cmd tea.Cmd
	switch m.currentPage {
	case pageFileBrowser:
		_, cmd = m.fileBrowserPage.Update(msg)
	case pageConversation:
		cmd = m.conversationPage.Update(msg)
	}

	return m, cmd
}

// View renders the current page
func (m *viewerTUI) View() string {
	switch m.currentPage {
	case pageFileBrowser:
		return m.fileBrowserPage.Render()
	case pageConversation:
		return m.conversationPage.Render()
	default:
		return ""
	}
}
