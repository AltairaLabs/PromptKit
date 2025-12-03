package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRenderSummary(t *testing.T) {
	summary := &Summary{
		TotalRuns:     50,
		SuccessCount:  47,
		FailedCount:   3,
		TotalCost:     1.2345,
		TotalTokens:   45231,
		TotalDuration: 2*time.Minute + 34*time.Second,
		AvgDuration:   3*time.Second + 100*time.Millisecond,
		ProviderCounts: map[string]int{
			"openai": 18,
			"claude": 16,
			"gemini": 16,
		},
		ScenarioCount: 10,
		Regions:       []string{"us", "uk", "au"},
		Errors: []ErrorInfo{
			{
				RunID:    "run-1",
				Scenario: "timeout-test",
				Provider: "claude",
				Region:   "uk",
				Error:    "execution timeout",
			},
		},
		OutputDir:  "out/",
		HTMLReport: "out/report.html",
	}

	result := RenderSummary(summary, 80)

	// Check that key information is present
	assert.Contains(t, result, "Run Summary")
	assert.Contains(t, result, "50")
	assert.Contains(t, result, "47")
	assert.Contains(t, result, "3")
	assert.Contains(t, result, "$1.2345")
	assert.Contains(t, result, "45,231")
	assert.Contains(t, result, "2m34s")
	assert.Contains(t, result, "3.1s")
	assert.Contains(t, result, "10 scenarios")
	assert.Contains(t, result, "us, uk, au")
	assert.Contains(t, result, "timeout-test/claude/uk")
	assert.Contains(t, result, "execution timeout")
	assert.Contains(t, result, "out/")
	assert.Contains(t, result, "out/report.html")
}

func TestRenderSummaryCIMode(t *testing.T) {
	summary := &Summary{
		TotalRuns:     50,
		SuccessCount:  47,
		FailedCount:   3,
		TotalCost:     1.2345,
		TotalTokens:   45231,
		TotalDuration: 2*time.Minute + 34*time.Second,
		AvgDuration:   3*time.Second + 100*time.Millisecond,
		ProviderCounts: map[string]int{
			"openai": 18,
			"claude": 16,
			"gemini": 16,
		},
		ScenarioCount: 10,
		Regions:       []string{"us", "uk", "au"},
		Errors: []ErrorInfo{
			{
				RunID:    "run-1",
				Scenario: "timeout-test",
				Provider: "claude",
				Region:   "uk",
				Error:    "execution timeout",
			},
		},
		OutputDir:  "out/",
		HTMLReport: "out/report.html",
	}

	result := RenderSummaryCIMode(summary)

	// Check header box drawing
	assert.Contains(t, result, "╔═══════════════════════════════════════════════════════════╗")
	assert.Contains(t, result, "║                   Run Summary                             ║")
	assert.Contains(t, result, "╚═══════════════════════════════════════════════════════════╝")

	// Check that key information is present (plain text, no ANSI codes)
	assert.Contains(t, result, "Total Runs:       50")
	assert.Contains(t, result, "Successful:       47 (94.0%)")
	assert.Contains(t, result, "Failed:           3 (6.0%)")
	assert.Contains(t, result, "Total Cost:       $1.2345")
	assert.Contains(t, result, "Total Tokens:     45,231")
	assert.Contains(t, result, "Total Duration:   2m34s")
	assert.Contains(t, result, "Avg Duration:     3.1s per run")
	assert.Contains(t, result, "Scenarios:        10 scenarios")
	assert.Contains(t, result, "Regions:          us, uk, au")
	assert.Contains(t, result, "timeout-test/claude/uk: execution timeout")
	assert.Contains(t, result, "Results saved to: out/")
	assert.Contains(t, result, "HTML Report:      out/report.html")

	// Ensure no ANSI escape codes (lipgloss colors) in CI mode
	assert.NotContains(t, result, "\x1b[")
}

func TestRenderSummary_ZeroRuns(t *testing.T) {
	summary := &Summary{
		TotalRuns:      0,
		SuccessCount:   0,
		FailedCount:    0,
		TotalCost:      0,
		TotalTokens:    0,
		TotalDuration:  0,
		AvgDuration:    0,
		ProviderCounts: map[string]int{},
		ScenarioCount:  0,
		Regions:        []string{},
		Errors:         []ErrorInfo{},
		OutputDir:      "out/",
	}

	result := RenderSummary(summary, 80)

	assert.Contains(t, result, "Total Runs:")
	assert.Contains(t, result, "0")
	assert.Contains(t, result, "0.0%") // Should handle division by zero gracefully
}

func TestRenderSummary_NoErrors(t *testing.T) {
	summary := &Summary{
		TotalRuns:      10,
		SuccessCount:   10,
		FailedCount:    0,
		TotalCost:      0.5,
		TotalTokens:    5000,
		TotalDuration:  30 * time.Second,
		AvgDuration:    3 * time.Second,
		ProviderCounts: map[string]int{"openai": 10},
		ScenarioCount:  5,
		Regions:        []string{"us"},
		Errors:         []ErrorInfo{},
		OutputDir:      "out/",
	}

	result := RenderSummary(summary, 80)

	assert.Contains(t, result, "Total Runs:")
	assert.Contains(t, result, "10")
	assert.NotContains(t, result, "Errors:") // No errors section
}

func TestRenderSummary_NoHTMLReport(t *testing.T) {
	summary := &Summary{
		TotalRuns:      5,
		SuccessCount:   5,
		FailedCount:    0,
		TotalCost:      0.1,
		TotalTokens:    1000,
		TotalDuration:  10 * time.Second,
		AvgDuration:    2 * time.Second,
		ProviderCounts: map[string]int{"openai": 5},
		ScenarioCount:  2,
		Regions:        []string{"us"},
		Errors:         []ErrorInfo{},
		OutputDir:      "out/",
		HTMLReport:     "", // No HTML report
	}

	result := RenderSummary(summary, 80)

	assert.Contains(t, result, "Results saved to:")
	assert.Contains(t, result, "out/")
	assert.NotContains(t, result, "HTML Report:") // No HTML report line
}

func TestModel_BuildSummary(t *testing.T) {
	m := NewModel("test-config.yaml", 10)

	// Simulate some runs
	m.mu.Lock()
	m.activeRuns = []RunInfo{
		{
			RunID:    "run-1",
			Scenario: "scenario-1",
			Provider: "openai",
			Region:   "us",
			Status:   StatusCompleted,
			Duration: 2 * time.Second,
			Cost:     0.01,
		},
		{
			RunID:    "run-2",
			Scenario: "scenario-2",
			Provider: "claude",
			Region:   "uk",
			Status:   StatusFailed,
			Error:    "timeout",
		},
		{
			RunID:    "run-3",
			Scenario: "scenario-1",
			Provider: "gemini",
			Region:   "au",
			Status:   StatusCompleted,
			Duration: 3 * time.Second,
			Cost:     0.02,
		},
	}
	m.completedCount = 3
	m.successCount = 2
	m.failedCount = 1
	m.totalCost = 0.03
	m.totalDuration = 5 * time.Second
	m.mu.Unlock()

	summary := m.BuildSummary("out/", "out/report.html")

	assert.Equal(t, 10, summary.TotalRuns)
	assert.Equal(t, 2, summary.SuccessCount)
	assert.Equal(t, 1, summary.FailedCount)
	assert.Equal(t, 0.03, summary.TotalCost)
	assert.Equal(t, int64(0), summary.TotalTokens) // Tokens only available via statestore
	assert.Equal(t, 2, summary.ScenarioCount)      // scenario-1 and scenario-2
	assert.Equal(t, 3, len(summary.ProviderCounts))
	assert.Equal(t, 1, summary.ProviderCounts["openai"])
	assert.Equal(t, 1, summary.ProviderCounts["claude"])
	assert.Equal(t, 1, summary.ProviderCounts["gemini"])
	assert.Equal(t, 3, len(summary.Regions))
	assert.Contains(t, summary.Regions, "us")
	assert.Contains(t, summary.Regions, "uk")
	assert.Contains(t, summary.Regions, "au")
	assert.Len(t, summary.Errors, 1)
	assert.Equal(t, "run-2", summary.Errors[0].RunID)
	assert.Equal(t, "timeout", summary.Errors[0].Error)
	assert.Equal(t, "out/", summary.OutputDir)
	assert.Equal(t, "out/report.html", summary.HTMLReport)
}

func TestModel_BuildSummary_AvgDuration(t *testing.T) {
	m := NewModel("test-config.yaml", 5)

	m.mu.Lock()
	m.completedCount = 4
	m.totalDuration = 12 * time.Second
	m.mu.Unlock()

	summary := m.BuildSummary("out/", "")

	// Average should be 12s / 4 = 3s
	assert.Equal(t, 3*time.Second, summary.AvgDuration)
}

func TestModel_BuildSummary_ZeroCompleted(t *testing.T) {
	m := NewModel("test-config.yaml", 5)

	m.mu.Lock()
	m.completedCount = 0
	m.totalDuration = 0
	m.mu.Unlock()

	summary := m.BuildSummary("out/", "")

	// Average should be 0 when no runs completed
	assert.Equal(t, time.Duration(0), summary.AvgDuration)
}

func TestModel_View_WithSummary(t *testing.T) {
	m := NewModel("test-config.yaml", 10)

	m.mu.Lock()
	m.isTUIMode = true // Ensure TUI mode is enabled for this test
	m.width = 100
	m.height = 40
	m.mu.Unlock()

	view := m.View()

	// Should render the regular TUI view (summary display was removed)
	// The view should not be empty or "Loading..."
	assert.NotEmpty(t, view)
	assert.NotEqual(t, "Loading...", view)
}

func TestRenderSummary_MultipleErrors(t *testing.T) {
	summary := &Summary{
		TotalRuns:     10,
		SuccessCount:  7,
		FailedCount:   3,
		TotalCost:     0.5,
		TotalTokens:   5000,
		TotalDuration: 30 * time.Second,
		AvgDuration:   3 * time.Second,
		ProviderCounts: map[string]int{
			"openai": 5,
			"claude": 5,
		},
		ScenarioCount: 5,
		Regions:       []string{"us", "uk"},
		Errors: []ErrorInfo{
			{Scenario: "test-1", Provider: "openai", Region: "us", Error: "error 1"},
			{Scenario: "test-2", Provider: "claude", Region: "uk", Error: "error 2"},
			{Scenario: "test-3", Provider: "openai", Region: "us", Error: "error 3"},
		},
		OutputDir: "out/",
	}

	result := RenderSummaryCIMode(summary)

	// Check all errors are listed
	assert.Contains(t, result, "test-1/openai/us: error 1")
	assert.Contains(t, result, "test-2/claude/uk: error 2")
	assert.Contains(t, result, "test-3/openai/us: error 3")

	// Count error lines
	errorLines := strings.Count(result, "  •")
	assert.Equal(t, 3, errorLines)
}
