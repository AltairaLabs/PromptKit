package views

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/viewmodels"
)

func TestNewSummaryView(t *testing.T) {
	view := NewSummaryView(100, false)
	assert.NotNil(t, view)
	assert.Equal(t, 100, view.width)
	assert.False(t, view.ciMode)

	ciView := NewSummaryView(80, true)
	assert.NotNil(t, ciView)
	assert.True(t, ciView.ciMode)
}

func TestSummaryView_Render_TUIMode(t *testing.T) {
	data := &viewmodels.SummaryData{
		TotalRuns:     10,
		CompletedRuns: 8,
		FailedRuns:    2,
		TotalTokens:   5000,
		TotalCost:     1.50,
		TotalDuration: 30 * time.Second,
		AvgDuration:   3 * time.Second,
		ProviderStats: map[string]viewmodels.ProviderStat{
			"openai": {Runs: 5, Tokens: 2500},
			"claude": {Runs: 5, Tokens: 2500},
		},
		ScenarioCount: 5,
		Regions:       []string{"us-east-1", "eu-west-1"},
		OutputDir:     "/tmp/results",
		HTMLReport:    "/tmp/report.html",
	}

	vm := viewmodels.NewSummaryViewModel(data)
	view := NewSummaryView(100, false)
	output := view.Render(vm)

	// Verify header
	assert.Contains(t, output, "Run Summary")
	assert.Contains(t, output, "╔═════")
	assert.Contains(t, output, "╚═════")

	// Verify stats
	assert.Contains(t, output, "Total Runs:")
	assert.Contains(t, output, "10")
	assert.Contains(t, output, "Successful:")
	assert.Contains(t, output, "8")
	assert.Contains(t, output, "Failed:")
	assert.Contains(t, output, "2")

	// Verify cost and performance
	assert.Contains(t, output, "Total Cost:")
	assert.Contains(t, output, "$1.50")
	assert.Contains(t, output, "Total Tokens:")
	assert.Contains(t, output, "5,000")
	assert.Contains(t, output, "Total Duration:")
	assert.Contains(t, output, "30s")

	// Verify providers
	assert.Contains(t, output, "Providers:")
	assert.Contains(t, output, "openai")
	assert.Contains(t, output, "claude")

	// Verify regions
	assert.Contains(t, output, "Regions:")
	assert.Contains(t, output, "us-east-1")
	assert.Contains(t, output, "eu-west-1")

	// Verify output info
	assert.Contains(t, output, "Results saved to:")
	assert.Contains(t, output, "/tmp/results")
	assert.Contains(t, output, "HTML Report:")
	assert.Contains(t, output, "/tmp/report.html")
}

func TestSummaryView_Render_CIMode(t *testing.T) {
	data := &viewmodels.SummaryData{
		TotalRuns:     5,
		CompletedRuns: 5,
		FailedRuns:    0,
		TotalTokens:   2000,
		TotalCost:     0.50,
		TotalDuration: 10 * time.Second,
		AvgDuration:   2 * time.Second,
		ProviderStats: map[string]viewmodels.ProviderStat{
			"openai": {Runs: 5, Tokens: 2000},
		},
		ScenarioCount: 3,
		OutputDir:     "/tmp/ci-results",
	}

	vm := viewmodels.NewSummaryViewModel(data)
	view := NewSummaryView(100, true)
	output := view.Render(vm)

	// Verify CI mode header (simpler)
	assert.Contains(t, output, "Run Summary")
	assert.Contains(t, output, "╔═══")
	assert.Contains(t, output, "╚═══")

	// Verify stats with proper formatting
	assert.Contains(t, output, "Total Runs:       5")
	assert.Contains(t, output, "Successful:       5")
	assert.Contains(t, output, "Failed:           0")
	assert.Contains(t, output, "Total Cost:       $0.50")

	// No HTML report in this test
	assert.NotContains(t, output, "HTML Report:")
}

func TestSummaryView_WithAssertions(t *testing.T) {
	data := &viewmodels.SummaryData{
		TotalRuns:       10,
		CompletedRuns:   10,
		FailedRuns:      0,
		TotalTokens:     1000,
		TotalCost:       0.10,
		TotalDuration:   10 * time.Second,
		AvgDuration:     1 * time.Second,
		AssertionTotal:  20,
		AssertionFailed: 2,
		ScenarioCount:   2,
		OutputDir:       "/tmp/test",
	}

	vm := viewmodels.NewSummaryViewModel(data)
	view := NewSummaryView(100, false)
	output := view.Render(vm)

	assert.Contains(t, output, "Assertions:")
	assert.Contains(t, output, "20 total")
	assert.Contains(t, output, "Assertions Fail:")
	assert.Contains(t, output, "2")
}

func TestSummaryView_WithAllAssertionsPassed(t *testing.T) {
	data := &viewmodels.SummaryData{
		TotalRuns:       5,
		CompletedRuns:   5,
		FailedRuns:      0,
		TotalTokens:     500,
		TotalCost:       0.05,
		TotalDuration:   5 * time.Second,
		AvgDuration:     1 * time.Second,
		AssertionTotal:  10,
		AssertionFailed: 0,
		ScenarioCount:   1,
		OutputDir:       "/tmp/test",
	}

	vm := viewmodels.NewSummaryViewModel(data)
	view := NewSummaryView(100, false)
	output := view.Render(vm)

	assert.Contains(t, output, "Assertions:")
	assert.Contains(t, output, "10 total")
	assert.Contains(t, output, "Assertions Pass:")
	assert.Contains(t, output, "all passed")
}

func TestSummaryView_WithErrors(t *testing.T) {
	data := &viewmodels.SummaryData{
		TotalRuns:     5,
		CompletedRuns: 3,
		FailedRuns:    2,
		TotalTokens:   1000,
		TotalCost:     0.20,
		TotalDuration: 10 * time.Second,
		AvgDuration:   2 * time.Second,
		Errors: []viewmodels.ErrorInfo{
			{
				RunID:    "run1",
				Scenario: "test-scenario",
				Provider: "openai",
				Region:   "us-east-1",
				Error:    "Connection timeout",
			},
			{
				RunID:    "run2",
				Scenario: "test-scenario-2",
				Provider: "claude",
				Region:   "eu-west-1",
				Error:    "Rate limit exceeded",
			},
		},
		ScenarioCount: 2,
		OutputDir:     "/tmp/test",
	}

	vm := viewmodels.NewSummaryViewModel(data)
	view := NewSummaryView(100, false)
	output := view.Render(vm)

	assert.Contains(t, output, "Errors:")
	assert.Contains(t, output, "test-scenario/openai/us-east-1")
	assert.Contains(t, output, "Connection timeout")
	assert.Contains(t, output, "test-scenario-2/claude/eu-west-1")
	assert.Contains(t, output, "Rate limit exceeded")
}

func TestSummaryView_MinimalData(t *testing.T) {
	data := &viewmodels.SummaryData{
		TotalRuns:     1,
		CompletedRuns: 1,
		FailedRuns:    0,
		TotalTokens:   100,
		TotalCost:     0.01,
		TotalDuration: 1 * time.Second,
		AvgDuration:   1 * time.Second,
		ScenarioCount: 1,
		OutputDir:     "/tmp/test",
	}

	vm := viewmodels.NewSummaryViewModel(data)
	view := NewSummaryView(80, false)
	output := view.Render(vm)

	// Should not have providers, regions, errors, assertions, or HTML report
	assert.NotContains(t, output, "Providers:")
	assert.NotContains(t, output, "Regions:")
	assert.NotContains(t, output, "Errors:")
	assert.NotContains(t, output, "Assertions:")
	assert.NotContains(t, output, "HTML Report:")

	// Should still have basic info
	assert.Contains(t, output, "Run Summary")
	assert.Contains(t, output, "Total Runs:")
	assert.Contains(t, output, "Results saved to:")
}

func TestFormatLine(t *testing.T) {
	label := "Test:"
	value := "Value"
	result := formatLine(label, value, &labelStyle, &valueStyle)

	// Should contain both label and value
	assert.Contains(t, result, "Test:")
	assert.Contains(t, result, "Value")
	// Should end with newline
	assert.True(t, strings.HasSuffix(result, "\n"))
}

// Mock styles for testing formatLine
var (
	labelStyle = lipgloss.NewStyle().Bold(true)
	valueStyle = lipgloss.NewStyle()
)
