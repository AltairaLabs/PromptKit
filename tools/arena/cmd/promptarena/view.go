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

	// Create and run the TUI
	page := pages.NewFileBrowserPage(absPath)

	// Create the bubbletea program
	p := tea.NewProgram(
		page,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	// Run the program
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("failed to run TUI: %w", err)
	}

	return nil
}
