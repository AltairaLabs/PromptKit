package results

import (
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
)

// SummaryBuilder helps build ResultSummary from RunResult slices
type SummaryBuilder struct {
	configFile string
	timestamp  time.Time
}

// NewSummaryBuilder creates a new summary builder
func NewSummaryBuilder(configFile string) *SummaryBuilder {
	return &SummaryBuilder{
		configFile: configFile,
		timestamp:  time.Now(),
	}
}

// SetTimestamp sets a custom timestamp for the summary
func (b *SummaryBuilder) SetTimestamp(ts time.Time) *SummaryBuilder {
	b.timestamp = ts
	return b
}

// SetGitMetadata sets Git-related metadata for CI/CD integration
func (b *SummaryBuilder) SetGitMetadata(commit, branch string) *SummaryBuilder {
	// Store in a more complete builder if needed later
	return b
}

// SetCIMetadata sets CI/CD-related metadata
func (b *SummaryBuilder) SetCIMetadata(buildID, jobURL string) *SummaryBuilder {
	// Store in a more complete builder if needed later
	return b
}

// BuildSummary creates a ResultSummary from the provided results
func (b *SummaryBuilder) BuildSummary(results []engine.RunResult) *ResultSummary {
	if len(results) == 0 {
		return &ResultSummary{
			TotalTests: 0,
			Timestamp:  b.timestamp,
			ConfigFile: b.configFile,
			RunIDs:     []string{},
		}
	}

	// Count results by status
	passed, failed := CountResultsByStatus(results)
	totalTests := len(results)

	// Calculate performance metrics
	totalCost, totalTokens, totalDuration := CalculatePerformanceMetrics(results)
	averageCost := float64(0)
	if totalTests > 0 {
		averageCost = totalCost / float64(totalTests)
	}

	// Extract unique metadata
	runIDs := ExtractRunIDs(results)
	promptPacks := ExtractUniqueValues(results, func(r engine.RunResult) string { return r.PromptPack })
	scenarios := ExtractUniqueValues(results, func(r engine.RunResult) string { return r.ScenarioID })
	providers := ExtractUniqueValues(results, func(r engine.RunResult) string { return r.ProviderID })
	regions := ExtractUniqueValues(results, func(r engine.RunResult) string { return r.Region })

	return &ResultSummary{
		TotalTests:    totalTests,
		Passed:        passed,
		Failed:        failed,
		Errors:        0, // Arena doesn't distinguish between failures and errors currently
		Skipped:       0, // Arena doesn't have skipped tests currently
		TotalDuration: totalDuration,
		AverageCost:   averageCost,
		TotalCost:     totalCost,
		TotalTokens:   totalTokens,
		Timestamp:     b.timestamp,
		ConfigFile:    b.configFile,
		RunIDs:        runIDs,
		PromptPacks:   promptPacks,
		Scenarios:     scenarios,
		Providers:     providers,
		Regions:       regions,
	}
}

// CountResultsByStatus counts successful and failed results.
// A result is considered successful if:
//  1. There are no errors AND no violations, OR
//  2. There are no errors AND violations occurred AND there are assertions AND all assertions passed.
//     This allows tests that EXPECT guardrails to trigger to pass when they do.
//
// A result is considered failed if:
//  1. There are errors, OR
//  2. There are violations AND no assertions (violations are unexpected), OR
//  3. There are violations AND some assertions fail
func CountResultsByStatus(results []engine.RunResult) (passed, failed int) {
	for _, result := range results {
		if result.Error != "" {
			// Any error = failed
			failed++
			continue
		}

		if len(result.Violations) == 0 {
			// No violations = passed
			passed++
			continue
		}

		// Has violations - check if assertions account for them
		// Only consider it a pass if there ARE assertions and ALL of them passed
		if HasAssertions(&result) && AllAssertionsPassed(&result) {
			passed++
		} else {
			failed++
		}
	}
	return passed, failed
}

// AllAssertionsPassed checks if all assertions in the result passed.
// This includes both turn-level assertions (in message metadata) and
// conversation-level assertions.
func AllAssertionsPassed(result *engine.RunResult) bool {
	// Check conversation-level assertions
	if result.ConversationAssertions.Total > 0 && !result.ConversationAssertions.Passed {
		return false
	}

	// Check turn-level assertions in messages
	for i := range result.Messages {
		if meta := result.Messages[i].Meta; meta != nil {
			if assertions, ok := meta["assertions"].(map[string]interface{}); ok {
				if passed, ok := assertions["passed"].(bool); ok && !passed {
					return false
				}
			}
		}
	}

	return true
}

// HasAssertions checks if the result has any assertions defined.
// This is used to determine if violations should be treated as failures:
// - Violations WITH passing assertions = test passed (guardrails were expected)
// - Violations WITHOUT any assertions = test failed (guardrails were unexpected)
func HasAssertions(result *engine.RunResult) bool {
	// Check conversation-level assertions
	if result.ConversationAssertions.Total > 0 {
		return true
	}

	// Check turn-level assertions in messages
	for i := range result.Messages {
		if meta := result.Messages[i].Meta; meta != nil {
			if _, ok := meta["assertions"].(map[string]interface{}); ok {
				return true
			}
		}
	}

	return false
}

// CalculatePerformanceMetrics calculates cost, token, and duration totals
func CalculatePerformanceMetrics(results []engine.RunResult) (totalCost float64, totalTokens int, totalDuration time.Duration) {
	for _, result := range results {
		totalCost += result.Cost.TotalCost
		totalTokens += result.Cost.InputTokens + result.Cost.OutputTokens
		totalDuration += result.Duration
	}
	return totalCost, totalTokens, totalDuration
}

// ExtractRunIDs extracts all run IDs from results
func ExtractRunIDs(results []engine.RunResult) []string {
	runIDs := make([]string, len(results))
	for i, result := range results {
		runIDs[i] = result.RunID
	}
	return runIDs
}

// ExtractUniqueValues extracts unique values using the provided extractor function
func ExtractUniqueValues(results []engine.RunResult, extractor func(engine.RunResult) string) []string {
	seen := make(map[string]bool)
	var values []string

	for _, result := range results {
		value := extractor(result)
		if value != "" && !seen[value] {
			seen[value] = true
			values = append(values, value)
		}
	}

	return values
}

// ValidateResults performs basic validation on results before processing
func ValidateResults(results []engine.RunResult) error {
	if results == nil {
		return NewValidationError("results", results, "results cannot be nil")
	}

	for i, result := range results {
		if result.RunID == "" {
			return NewValidationError("RunID", result.RunID, fmt.Sprintf("result %d has empty RunID", i))
		}
		if result.ScenarioID == "" {
			return NewValidationError("ScenarioID", result.ScenarioID, fmt.Sprintf("result %d has empty ScenarioID", i))
		}
		if result.ProviderID == "" {
			return NewValidationError("ProviderID", result.ProviderID, fmt.Sprintf("result %d has empty ProviderID", i))
		}
	}

	return nil
}
