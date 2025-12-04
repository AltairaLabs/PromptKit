// Package filesystem provides filesystem-based result reading for Arena.
package filesystem

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/AltairaLabs/PromptKit/tools/arena/results"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

// FilesystemResultReader reads results from JSON files on the filesystem
//
//nolint:revive // Name follows established pattern (JSONResultRepository, HTMLResultRepository, etc.)
type FilesystemResultReader struct {
	baseDir string
	cache   map[string]*statestore.RunResult
	mu      sync.RWMutex
}

// NewFilesystemResultReader creates a reader for filesystem-based results
func NewFilesystemResultReader(baseDir string) *FilesystemResultReader {
	return &FilesystemResultReader{
		baseDir: baseDir,
		cache:   make(map[string]*statestore.RunResult),
	}
}

// ListResults scans directory and returns metadata for all JSON result files
func (r *FilesystemResultReader) ListResults() ([]results.ResultMetadata, error) {
	var metadata []results.ResultMetadata

	err := filepath.Walk(r.baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}

		if filepath.Base(path) == "index.json" {
			return nil
		}

		meta, err := r.extractMetadata(path)
		if err != nil {
			return nil
		}

		metadata = append(metadata, meta)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk directory: %w", err)
	}

	return metadata, nil
}

// extractMetadata extracts metadata from a result file
func (r *FilesystemResultReader) extractMetadata(path string) (results.ResultMetadata, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is from filepath.Walk within baseDir
	if err != nil {
		return results.ResultMetadata{}, fmt.Errorf("failed to read file: %w", err)
	}

	var partial struct {
		RunID      string `json:"RunID"`
		ScenarioID string `json:"ScenarioID"`
		ProviderID string `json:"ProviderID"`
		Region     string `json:"Region"`
		Error      string `json:"Error"`
		Cost       struct {
			TotalCost float64 `json:"TotalCost"`
		} `json:"Cost"`
	}

	if err := json.Unmarshal(data, &partial); err != nil {
		return results.ResultMetadata{}, fmt.Errorf("failed to parse JSON: %w", err)
	}

	status := "success"
	if partial.Error != "" {
		status = "failed"
	}

	return results.ResultMetadata{
		RunID:    partial.RunID,
		Scenario: partial.ScenarioID,
		Provider: partial.ProviderID,
		Region:   partial.Region,
		Status:   status,
		Error:    partial.Error,
		Cost:     partial.Cost.TotalCost,
		Location: path,
	}, nil
}

// LoadResult loads a single result by ID
func (r *FilesystemResultReader) LoadResult(runID string) (*statestore.RunResult, error) {
	r.mu.RLock()
	if result, ok := r.cache[runID]; ok {
		r.mu.RUnlock()
		return result, nil
	}
	r.mu.RUnlock()

	var resultPath string
	err := filepath.Walk(r.baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}

		if strings.Contains(path, runID) {
			resultPath = path
			return filepath.SkipAll
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to search for result: %w", err)
	}

	if resultPath == "" {
		return nil, fmt.Errorf("result not found: %s", runID)
	}

	result, err := r.loadFromFile(resultPath)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	r.cache[runID] = result
	r.mu.Unlock()

	return result, nil
}

// loadFromFile loads a result from a specific file path
func (r *FilesystemResultReader) loadFromFile(path string) (*statestore.RunResult, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is from filepath.Walk within baseDir
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var result statestore.RunResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse result: %w", err)
	}

	if result.RunID == "" {
		return nil, fmt.Errorf("invalid result: missing RunID")
	}

	return &result, nil
}

// LoadResults loads multiple results by IDs
func (r *FilesystemResultReader) LoadResults(runIDs []string) ([]*statestore.RunResult, error) {
	loadedResults := make([]*statestore.RunResult, 0, len(runIDs))

	for _, runID := range runIDs {
		result, err := r.LoadResult(runID)
		if err != nil {
			continue
		}
		loadedResults = append(loadedResults, result)
	}

	if len(loadedResults) == 0 && len(runIDs) > 0 {
		return nil, fmt.Errorf("failed to load any results")
	}

	return loadedResults, nil
}

// LoadAllResults loads all available results
func (r *FilesystemResultReader) LoadAllResults() ([]*statestore.RunResult, error) {
	metadata, err := r.ListResults()
	if err != nil {
		return nil, err
	}

	runIDs := make([]string, len(metadata))
	for i := range metadata {
		runIDs[i] = metadata[i].RunID
	}

	return r.LoadResults(runIDs)
}

// SupportsFiltering returns false (filesystem doesn't support server-side filtering)
func (r *FilesystemResultReader) SupportsFiltering() bool {
	return false
}

// FilterResults returns filtered result metadata using client-side filtering
func (r *FilesystemResultReader) FilterResults(filter *results.ResultFilter) ([]results.ResultMetadata, error) {
	allMetadata, err := r.ListResults()
	if err != nil {
		return nil, err
	}

	filtered := make([]results.ResultMetadata, 0)
	for i := range allMetadata {
		if allMetadata[i].MatchesFilter(filter) {
			filtered = append(filtered, allMetadata[i])
		}
	}

	return filtered, nil
}
