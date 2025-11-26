package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const (
	percentMultiplier = 100
	errorMarginLeft   = 2
)

// Summary represents the final execution summary displayed after all runs complete
type Summary struct {
	TotalRuns      int
	SuccessCount   int
	FailedCount    int
	TotalCost      float64
	TotalTokens    int64
	TotalDuration  time.Duration
	AvgDuration    time.Duration
	ProviderCounts map[string]int
	ScenarioCount  int
	Regions        []string
	Errors         []ErrorInfo
	OutputDir      string
	HTMLReport     string
}

// ErrorInfo represents a failed run with details
type ErrorInfo struct {
	RunID    string
	Scenario string
	Provider string
	Region   string
	Error    string
}

// RenderSummary renders the final summary screen for TUI mode
func RenderSummary(summary *Summary, width int) string {
	var sb strings.Builder

	// Header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("12")).
		Width(width).
		Align(lipgloss.Center)

	sb.WriteString(headerStyle.Render("╔═════════════════════════════════════════════════════════════╗"))
	sb.WriteString("\n")
	sb.WriteString(headerStyle.Render("║                   Run Summary                               ║"))
	sb.WriteString("\n")
	sb.WriteString(headerStyle.Render("╚═════════════════════════════════════════════════════════════╝"))
	sb.WriteString("\n\n")

	// Execution stats
	successRate := float64(0)
	if summary.TotalRuns > 0 {
		successRate = float64(summary.SuccessCount) / float64(summary.TotalRuns) * percentMultiplier
	}

	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	failStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))

	sb.WriteString(labelStyle.Render("Total Runs:       "))
	sb.WriteString(valueStyle.Render(fmt.Sprintf("%d\n", summary.TotalRuns)))

	sb.WriteString(labelStyle.Render("Successful:       "))
	sb.WriteString(successStyle.Render(fmt.Sprintf("%d (%.1f%%)\n", summary.SuccessCount, successRate)))

	sb.WriteString(labelStyle.Render("Failed:           "))
	sb.WriteString(failStyle.Render(fmt.Sprintf("%d (%.1f%%)\n", summary.FailedCount, percentMultiplier-successRate)))

	sb.WriteString("\n")

	// Cost and performance
	sb.WriteString(labelStyle.Render("Total Cost:       "))
	sb.WriteString(valueStyle.Render(fmt.Sprintf("$%.4f\n", summary.TotalCost)))

	sb.WriteString(labelStyle.Render("Total Tokens:     "))
	sb.WriteString(valueStyle.Render(formatNumber(summary.TotalTokens)))
	sb.WriteString("\n")

	sb.WriteString(labelStyle.Render("Total Duration:   "))
	sb.WriteString(valueStyle.Render(formatDuration(summary.TotalDuration)))
	sb.WriteString("\n")

	sb.WriteString(labelStyle.Render("Avg Duration:     "))
	sb.WriteString(valueStyle.Render(formatDuration(summary.AvgDuration)))
	sb.WriteString(" per run\n")

	sb.WriteString("\n")

	// Provider breakdown
	if len(summary.ProviderCounts) > 0 {
		providerList := make([]string, 0, len(summary.ProviderCounts))
		for provider, count := range summary.ProviderCounts {
			providerList = append(providerList, fmt.Sprintf("%s (%d)", provider, count))
		}
		sb.WriteString(labelStyle.Render("Providers:        "))
		sb.WriteString(valueStyle.Render(strings.Join(providerList, ", ")))
		sb.WriteString("\n")
	}

	sb.WriteString(labelStyle.Render("Scenarios:        "))
	sb.WriteString(valueStyle.Render(fmt.Sprintf("%d scenarios\n", summary.ScenarioCount)))

	if len(summary.Regions) > 0 {
		sb.WriteString(labelStyle.Render("Regions:          "))
		sb.WriteString(valueStyle.Render(strings.Join(summary.Regions, ", ")))
		sb.WriteString("\n")
	}

	// Errors
	if len(summary.Errors) > 0 {
		sb.WriteString("\n")
		sb.WriteString(labelStyle.Render("Errors:\n"))
		errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9")).MarginLeft(errorMarginLeft)
		for _, errInfo := range summary.Errors {
			runDesc := fmt.Sprintf("%s/%s/%s", errInfo.Scenario, errInfo.Provider, errInfo.Region)
			sb.WriteString(errorStyle.Render(fmt.Sprintf("• %s: %s\n", runDesc, errInfo.Error)))
		}
	}

	// Output info
	sb.WriteString("\n")
	sb.WriteString(labelStyle.Render("Results saved to: "))
	sb.WriteString(valueStyle.Render(summary.OutputDir))
	sb.WriteString("\n")

	if summary.HTMLReport != "" {
		sb.WriteString(labelStyle.Render("HTML Report:      "))
		sb.WriteString(valueStyle.Render(summary.HTMLReport))
		sb.WriteString("\n")
	}

	return sb.String()
}

// RenderSummaryCIMode renders the final summary for CI/simple mode (no colors)
func RenderSummaryCIMode(summary *Summary) string {
	var sb strings.Builder

	// Header
	sb.WriteString("╔═══════════════════════════════════════════════════════════╗\n")
	sb.WriteString("║                   Run Summary                             ║\n")
	sb.WriteString("╚═══════════════════════════════════════════════════════════╝\n\n")

	// Execution stats
	successRate := float64(0)
	if summary.TotalRuns > 0 {
		successRate = float64(summary.SuccessCount) / float64(summary.TotalRuns) * percentMultiplier
	}

	sb.WriteString(fmt.Sprintf("Total Runs:       %d\n", summary.TotalRuns))
	sb.WriteString(fmt.Sprintf("Successful:       %d (%.1f%%)\n", summary.SuccessCount, successRate))
	sb.WriteString(fmt.Sprintf("Failed:           %d (%.1f%%)\n", summary.FailedCount, percentMultiplier-successRate))
	sb.WriteString("\n")

	// Cost and performance
	sb.WriteString(fmt.Sprintf("Total Cost:       $%.4f\n", summary.TotalCost))
	sb.WriteString(fmt.Sprintf("Total Tokens:     %s\n", formatNumber(summary.TotalTokens)))
	sb.WriteString(fmt.Sprintf("Total Duration:   %s\n", formatDuration(summary.TotalDuration)))
	sb.WriteString(fmt.Sprintf("Avg Duration:     %s per run\n", formatDuration(summary.AvgDuration)))
	sb.WriteString("\n")

	// Provider breakdown
	if len(summary.ProviderCounts) > 0 {
		providerList := make([]string, 0, len(summary.ProviderCounts))
		for provider, count := range summary.ProviderCounts {
			providerList = append(providerList, fmt.Sprintf("%s (%d)", provider, count))
		}
		sb.WriteString(fmt.Sprintf("Providers:        %s\n", strings.Join(providerList, ", ")))
	}

	sb.WriteString(fmt.Sprintf("Scenarios:        %d scenarios\n", summary.ScenarioCount))

	if len(summary.Regions) > 0 {
		sb.WriteString(fmt.Sprintf("Regions:          %s\n", strings.Join(summary.Regions, ", ")))
	}

	// Errors
	if len(summary.Errors) > 0 {
		sb.WriteString("\nErrors:\n")
		for _, errInfo := range summary.Errors {
			runDesc := fmt.Sprintf("%s/%s/%s", errInfo.Scenario, errInfo.Provider, errInfo.Region)
			sb.WriteString(fmt.Sprintf("  • %s: %s\n", runDesc, errInfo.Error))
		}
	}

	// Output info
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("Results saved to: %s\n", summary.OutputDir))

	if summary.HTMLReport != "" {
		sb.WriteString(fmt.Sprintf("HTML Report:      %s\n", summary.HTMLReport))
	}

	return sb.String()
}
