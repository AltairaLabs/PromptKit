// Package html implements the ResultRepository interface for HTML report generation.
// It wraps the existing render.GenerateHTMLReport functionality to provide
// consistent output handling within the repository pattern.
package html

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/render"
	"github.com/AltairaLabs/PromptKit/tools/arena/results"
)

// HTMLResultRepository implements ResultRepository for HTML report generation.
// It wraps the existing render.GenerateHTMLReport function to provide
// consistent output handling within the repository pattern.
type HTMLResultRepository struct {
	outputPath string
	options    *HTMLOptions
}

// HTMLOptions provides configuration for HTML report generation.
type HTMLOptions struct {
	// GenerateJSON controls whether to generate companion JSON data file
	GenerateJSON bool

	// Title sets the report title (defaults to "Altaira Prompt Arena Report")
	Title string

	// UseTimestampSuffix controls whether to add timestamp to filename
	UseTimestampSuffix bool
}

// DefaultHTMLOptions returns the default HTML options.
func DefaultHTMLOptions() *HTMLOptions {
	return &HTMLOptions{
		GenerateJSON:       true,
		Title:              "Altaira Prompt Arena Report",
		UseTimestampSuffix: false,
	}
}

// NewHTMLResultRepository creates a new HTML result repository.
func NewHTMLResultRepository(outputPath string) *HTMLResultRepository {
	return &HTMLResultRepository{
		outputPath: outputPath,
		options:    DefaultHTMLOptions(),
	}
}

// NewHTMLResultRepositoryWithOptions creates a new HTML result repository with custom options.
func NewHTMLResultRepositoryWithOptions(outputPath string, options *HTMLOptions) *HTMLResultRepository {
	if options == nil {
		options = DefaultHTMLOptions()
	}
	return &HTMLResultRepository{
		outputPath: outputPath,
		options:    options,
	}
}

// GetOutputPath returns the configured output path.
func (r *HTMLResultRepository) GetOutputPath() string {
	return r.outputPath
}

// SaveResults generates an HTML report from the provided results.
// This wraps the existing render.GenerateHTMLReport functionality.
func (r *HTMLResultRepository) SaveResults(runResults []engine.RunResult) error {
	if err := results.ValidateResults(runResults); err != nil {
		return err
	}

	// Determine final output path
	outputPath := r.outputPath
	if r.options.UseTimestampSuffix {
		outputPath = r.addTimestampSuffix(outputPath)
	}

	// Use existing render.GenerateHTMLReport function
	if err := render.GenerateHTMLReport(runResults, outputPath); err != nil {
		return fmt.Errorf("HTML report generation failed: %w", err)
	}

	// Optionally disable JSON companion file generation
	if !r.options.GenerateJSON {
		// Remove the JSON file that was automatically generated
		jsonPath := strings.TrimSuffix(outputPath, ".html") + "-data.json"
		// We don't return an error if JSON removal fails since HTML generation succeeded
		_ = r.removeFile(jsonPath)
	}

	return nil
}

// SaveSummary saves a summary (no-op for HTML - summary is embedded in report).
// HTML reports include summary information within the main report,
// so this operation is not applicable.
func (r *HTMLResultRepository) SaveSummary(summary *results.ResultSummary) error {
	// HTML reports embed summary information, so this is a no-op
	return nil
}

// LoadResults is not supported for HTML repositories.
// HTML files are meant for human consumption and are not designed
// for programmatic parsing back to RunResult structures.
func (r *HTMLResultRepository) LoadResults() ([]engine.RunResult, error) {
	return nil, results.NewUnsupportedOperationError("LoadResults", "HTML repository does not support loading results")
}

// SupportsStreaming returns false - HTML generation requires all results at once.
// HTML reports need to calculate matrices, summaries, and cross-references
// which require the complete dataset.
func (r *HTMLResultRepository) SupportsStreaming() bool {
	return false
}

// SaveResult is not supported for HTML repositories.
// HTML generation requires all results to build comprehensive reports
// with matrices and summaries.
func (r *HTMLResultRepository) SaveResult(result *engine.RunResult) error {
	return results.NewUnsupportedOperationError("SaveResult", "HTML repository does not support streaming - use SaveResults instead")
}

// addTimestampSuffix adds a timestamp suffix to the output path.
func (r *HTMLResultRepository) addTimestampSuffix(outputPath string) string {
	timestamp := time.Now().Format("2006-01-02T15-04-05")
	ext := filepath.Ext(outputPath)
	base := strings.TrimSuffix(outputPath, ext)
	return fmt.Sprintf("%s-%s%s", base, timestamp, ext)
}

// removeFile is a helper to remove a file (used to clean up JSON if disabled).
// This is a separate method to make testing easier.
func (r *HTMLResultRepository) removeFile(path string) error {
	// Note: We ignore errors here since HTML generation succeeded
	// and JSON cleanup is optional
	_ = os.Remove(path)
	return nil
}
