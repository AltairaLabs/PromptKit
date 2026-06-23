package main

import (
	"github.com/spf13/cobra"
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

// NOTE: runView and validateResultsDir are defined in view_interactive.go
// Those functions are excluded from coverage testing as they involve interactive terminal operations.
