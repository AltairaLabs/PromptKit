package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/render"
	"github.com/spf13/cobra"
)

var renderCmd = &cobra.Command{
	Use:   "render [index.json path]",
	Short: "Generate HTML report from existing results",
	Long: `Generate an HTML report from an existing index.json file and its associated result files.
	
This command reads an index.json file (typically found in the output directory of a previous run),
loads all the referenced result files, and generates a comprehensive HTML report.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		indexPath := args[0]
		outputPath, err := cmd.Flags().GetString("output")
		if err != nil {
			return fmt.Errorf("failed to get output flag: %w", err)
		}

		return generateHTMLFromIndex(indexPath, outputPath)
	},
}

func init() {
	rootCmd.AddCommand(renderCmd)

	renderCmd.Flags().StringP("output", "o", "", "Output HTML file path (defaults to report-[timestamp].html in same directory)")
}

func generateHTMLFromIndex(indexPath, outputPath string) error {
	// Read and parse index.json
	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		return fmt.Errorf("failed to read index file %s: %w", indexPath, err)
	}

	var index struct {
		RunIDs []string `json:"run_ids"`
	}

	if err := json.Unmarshal(indexData, &index); err != nil {
		return fmt.Errorf("failed to parse index file: %w", err)
	}

	if len(index.RunIDs) == 0 {
		return fmt.Errorf("no run IDs found in index file")
	}

	// Get the directory containing the index file
	indexDir := filepath.Dir(indexPath)

	// Load all result files
	results := make([]engine.RunResult, 0, len(index.RunIDs))
	for _, runID := range index.RunIDs {
		resultPath := filepath.Join(indexDir, runID+".json")

		resultData, err := os.ReadFile(resultPath)
		if err != nil {
			fmt.Printf("Warning: failed to read result file %s: %v\n", resultPath, err)
			continue
		}

		var result engine.RunResult
		if err := json.Unmarshal(resultData, &result); err != nil {
			fmt.Printf("Warning: failed to parse result file %s: %v\n", resultPath, err)
			continue
		}

		results = append(results, result)
	}

	if len(results) == 0 {
		return fmt.Errorf("no valid result files could be loaded")
	}

	// Determine output path
	if outputPath == "" {
		timestamp := time.Now().Format("2006-01-02T15-04Z")
		outputPath = filepath.Join(indexDir, fmt.Sprintf("report-%s.html", timestamp))
	}

	// Generate HTML report
	if err := render.GenerateHTMLReport(results, outputPath); err != nil {
		return fmt.Errorf("failed to generate HTML report: %w", err)
	}

	// Convert to absolute path for display
	absOutputPath, err := filepath.Abs(outputPath)
	if err != nil {
		absOutputPath = outputPath
	}

	fmt.Printf("✅ HTML report generated successfully!\n")
	fmt.Printf("📁 Source: %s\n", indexPath)
	fmt.Printf("📊 Loaded %d results\n", len(results))
	fmt.Printf("🌐 Report: %s\n", absOutputPath)

	return nil
}
