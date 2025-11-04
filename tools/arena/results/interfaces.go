// Package results provides abstract result output layer for Arena.
// This package implements the Repository Pattern to support multiple
// output formats (JSON, JUnit XML, HTML, TAP) simultaneously while
// maintaining clean separation of concerns between execution and output.
package results

import (
	"time"

	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
)

// ResultRepository provides abstract access to test result storage
// and output formatting. Implementations handle specific formats like
// JSON, JUnit XML, HTML, or TAP.
type ResultRepository interface {
	// SaveResults saves test execution results in the repository's format
	SaveResults(results []engine.RunResult) error

	// SaveSummary saves a summary of all test results
	SaveSummary(summary *ResultSummary) error

	// LoadResults loads previously saved results (for report generation)
	// Returns error if the repository format doesn't support loading
	LoadResults() ([]engine.RunResult, error)

	// SupportsStreaming returns true if repository can write results incrementally
	SupportsStreaming() bool

	// SaveResult saves a single result (for streaming support)
	// Returns error if streaming is not supported
	SaveResult(result *engine.RunResult) error
}

// ResultSummary contains aggregate information about test runs
// This provides metadata and statistics that can be used across
// different output formats.
type ResultSummary struct {
	// Test execution counts
	TotalTests int `json:"total_tests"`
	Passed     int `json:"passed"`
	Failed     int `json:"failed"`
	Errors     int `json:"errors"`
	Skipped    int `json:"skipped"`

	// Performance metrics
	TotalDuration time.Duration `json:"total_duration"`
	AverageCost   float64       `json:"average_cost"`
	TotalCost     float64       `json:"total_cost"`
	TotalTokens   int           `json:"total_tokens"`

	// Execution metadata
	Timestamp  time.Time `json:"timestamp"`
	ConfigFile string    `json:"config_file"`

	// CI/CD integration metadata (optional)
	GitCommit string `json:"git_commit,omitempty"`
	GitBranch string `json:"git_branch,omitempty"`
	CIBuildID string `json:"ci_build_id,omitempty"`
	CIJobURL  string `json:"ci_job_url,omitempty"`

	// Arena-specific metadata
	RunIDs      []string `json:"run_ids"`
	PromptPacks []string `json:"prompt_packs"`
	Scenarios   []string `json:"scenarios"`
	Providers   []string `json:"providers"`
	Regions     []string `json:"regions"`
}