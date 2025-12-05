package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

// This file contains interactive TUI logic intentionally split for coverage control.
// See sonar-project.properties: **/*_interactive.go is excluded from coverage gates.

// runView handles the interactive TUI execution (excluded from coverage)
func runView(cmd *cobra.Command, args []string) error {
	// Determine results directory
	resultsDir := "."
	if len(args) > 0 {
		resultsDir = args[0]
	}

	// Validate the directory
	if err := validateResultsDir(resultsDir); err != nil {
		return err
	}

	// Resolve to absolute path
	absPath, err := filepath.Abs(resultsDir)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
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

// validateResultsDir validates that the given path is an accessible directory
func validateResultsDir(path string) error {
	// Resolve to absolute path
	absPath, err := filepath.Abs(path)
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

	return nil
}
