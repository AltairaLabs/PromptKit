package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/app"
)

// This file contains interactive TUI logic intentionally split for coverage control.
// See sonar-project.properties: **/*_interactive.go is excluded from coverage gates.

// runView handles the interactive TUI execution (excluded from coverage).
// It deep-links directly into the hub's ViewPage, with View as the stack root so
// that Esc/q exits the program (rather than returning to Home).
func runView(cmd *cobra.Command, args []string) error {
	// Determine results directory.
	resultsDir := "."
	if len(args) > 0 {
		resultsDir = args[0]
	}

	// Validate the directory.
	if err := validateResultsDir(resultsDir); err != nil {
		return err
	}

	// Resolve to absolute path.
	absPath, err := filepath.Abs(resultsDir)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	ctx := &app.AppContext{
		Version:    GetVersion(),
		ResultsDir: absPath,
	}

	// Attempt config discovery in the results directory's parent or cwd.
	cwd, err := os.Getwd()
	if err == nil {
		if cfgPath, found := app.DiscoverConfig(cwd); found {
			_ = ctx.LoadConfig(cfgPath) // non-fatal; view works without a config
		}
	}

	// ViewPage is the stack root — Esc/q exits directly.
	return app.Run(ctx, app.NewViewPage(absPath))
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
