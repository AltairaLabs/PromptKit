// Package json provides JSON file-based result storage for Arena.
// This package implements the ResultRepository interface to save Arena
// test results as individual JSON files plus an index summary file,
// maintaining backward compatibility with existing Arena output format.
package json

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/results"
)

const (
	// File names and error messages
	indexFileName              = "index.json"
	errFailedToCreateOutputDir = "failed to create output directory: %w"
)

// JSONResultRepository stores results as JSON files (one per result + index).
// This matches the existing Arena output format for backward compatibility.
type JSONResultRepository struct {
	outputDir string
}

// NewJSONResultRepository creates a new JSON result repository that writes
// to the specified output directory.
func NewJSONResultRepository(outputDir string) *JSONResultRepository {
	return &JSONResultRepository{outputDir: outputDir}
}

// GetOutputDir returns the output directory for this repository
func (r *JSONResultRepository) GetOutputDir() string {
	return r.outputDir
}

// SaveResults saves all results as individual JSON files plus an index summary.
// This maintains backward compatibility with existing Arena JSON output format.
func (r *JSONResultRepository) SaveResults(runResults []engine.RunResult) error {
	// Validate inputs
	if err := results.ValidateResults(runResults); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Create output directory
	if err := os.MkdirAll(r.outputDir, 0755); err != nil {
		return fmt.Errorf(errFailedToCreateOutputDir, err)
	}

	// Save individual result files
	for i := range runResults {
		if err := r.saveIndividualResult(&runResults[i]); err != nil {
			return fmt.Errorf("failed to save result %s: %w", runResults[i].RunID, err)
		}
	}

	return nil
}

// saveIndividualResult saves a single result to a JSON file
func (r *JSONResultRepository) saveIndividualResult(result *engine.RunResult) error {
	filename := filepath.Join(r.outputDir, result.RunID+".json")
	return r.writeJSONFile(result, filename)
}

// SaveSummary saves a summary of all test results as index.json
func (r *JSONResultRepository) SaveSummary(summary *results.ResultSummary) error {
	if summary == nil {
		return results.NewValidationError("summary", summary, "summary cannot be nil")
	}

	// Create output directory
	if err := os.MkdirAll(r.outputDir, 0755); err != nil {
		return fmt.Errorf(errFailedToCreateOutputDir, err)
	}

	// Convert to legacy format for backward compatibility
	legacyIndex := r.convertSummaryToLegacyFormat(summary)

	indexFile := filepath.Join(r.outputDir, indexFileName)
	return r.writeJSONFile(legacyIndex, indexFile)
}

// convertSummaryToLegacyFormat converts ResultSummary to the legacy index.json format
func (r *JSONResultRepository) convertSummaryToLegacyFormat(summary *results.ResultSummary) map[string]interface{} {
	return map[string]interface{}{
		"total_runs":  summary.TotalTests,
		"successful":  summary.Passed,
		"errors":      summary.Failed,
		"timestamp":   summary.Timestamp,
		"config_file": summary.ConfigFile,
		"run_ids":     summary.RunIDs,
		// Extended fields for richer metadata
		"total_cost":     summary.TotalCost,
		"average_cost":   summary.AverageCost,
		"total_tokens":   summary.TotalTokens,
		"total_duration": summary.TotalDuration.String(),
		"scenarios":      summary.Scenarios,
		"providers":      summary.Providers,
		"regions":        summary.Regions,
		"prompt_packs":   summary.PromptPacks,
	}
}

// LoadResults loads previously saved results from JSON files
func (r *JSONResultRepository) LoadResults() ([]engine.RunResult, error) {
	// Read index to get run IDs
	indexFile := filepath.Join(r.outputDir, indexFileName)
	indexData, err := os.ReadFile(indexFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("index file not found, no results to load")
		}
		return nil, fmt.Errorf("failed to read index file: %w", err)
	}

	var index map[string]interface{}
	if err := json.Unmarshal(indexData, &index); err != nil {
		return nil, fmt.Errorf("failed to parse index file: %w", err)
	}

	// Extract run IDs
	runIDsInterface, exists := index["run_ids"]
	if !exists {
		return []engine.RunResult{}, nil // Empty results
	}

	runIDsSlice, ok := runIDsInterface.([]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid run_ids format in index file")
	}

	// Load individual results
	var loadResults []engine.RunResult
	for _, runIDInterface := range runIDsSlice {
		runID, ok := runIDInterface.(string)
		if !ok {
			continue // Skip invalid run IDs
		}

		result, err := r.loadIndividualResult(runID)
		if err != nil {
			// Log warning but continue with other results
			continue
		}
		loadResults = append(loadResults, *result)
	}

	return loadResults, nil
}

// loadIndividualResult loads a single result from its JSON file
func (r *JSONResultRepository) loadIndividualResult(runID string) (*engine.RunResult, error) {
	filename := filepath.Join(r.outputDir, runID+".json")
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read result file %s: %w", filename, err)
	}

	var result engine.RunResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse result file %s: %w", filename, err)
	}

	return &result, nil
}

// SupportsStreaming returns true as JSON files can be written incrementally
func (r *JSONResultRepository) SupportsStreaming() bool {
	return true
}

// SaveResult saves a single result immediately (for streaming support)
func (r *JSONResultRepository) SaveResult(result *engine.RunResult) error {
	if result == nil {
		return results.NewValidationError("result", result, "result cannot be nil")
	}

	if result.RunID == "" {
		return results.NewValidationError("RunID", result.RunID, "RunID cannot be empty")
	}

	// Create output directory
	if err := os.MkdirAll(r.outputDir, 0755); err != nil {
		return fmt.Errorf(errFailedToCreateOutputDir, err)
	}

	return r.saveIndividualResult(result)
}

// writeJSONFile writes data as formatted JSON to the specified file
func (r *JSONResultRepository) writeJSONFile(data interface{}, filename string) error {
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	if err := os.WriteFile(filename, jsonData, 0600); err != nil {
		return fmt.Errorf("failed to write file %s: %w", filename, err)
	}

	return nil
}

// ListResults returns a list of all result files in the output directory
func (r *JSONResultRepository) ListResults() ([]string, error) {
	entries, err := os.ReadDir(r.outputDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var resultFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" && entry.Name() != indexFileName {
			resultFiles = append(resultFiles, entry.Name())
		}
	}

	return resultFiles, nil
}

// GetSummary loads and returns the summary from index.json
func (r *JSONResultRepository) GetSummary() (map[string]interface{}, error) {
	indexFile := filepath.Join(r.outputDir, indexFileName)
	data, err := os.ReadFile(indexFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("summary not found")
		}
		return nil, fmt.Errorf("failed to read summary: %w", err)
	}

	var summary map[string]interface{}
	if err := json.Unmarshal(data, &summary); err != nil {
		return nil, fmt.Errorf("failed to parse summary: %w", err)
	}

	return summary, nil
}
