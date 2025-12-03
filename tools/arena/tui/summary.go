package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/theme"
)

const (
	percentMultiplier = 100
	errorMarginLeft   = 2
)

// compactString removes excess whitespace and newlines from a string
func compactString(s string) string {
	// Replace newlines and multiple spaces with single space
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	// Collapse multiple spaces into one
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}

func formatLine(label, value string, labelStyle, valueStyle *lipgloss.Style) string {
	const labelWidth = 16
	lbl := labelStyle.Render(fmt.Sprintf("%-*s", labelWidth, label))
	val := valueStyle.Render(value)
	return fmt.Sprintf("%s %s\n", lbl, val)
}

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

	AssertionTotal  int
	AssertionFailed int
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

	sb.WriteString(formatLine(
		"Total Runs:",
		fmt.Sprintf("%d", summary.TotalRuns),
		&labelStyle,
		&valueStyle,
	))
	sb.WriteString(formatLine(
		"Successful:",
		fmt.Sprintf("%d (%.1f%%)", summary.SuccessCount, successRate),
		&labelStyle,
		&successStyle,
	))
	sb.WriteString(formatLine(
		"Failed:",
		fmt.Sprintf("%d (%.1f%%)", summary.FailedCount, percentMultiplier-successRate),
		&labelStyle,
		&failStyle,
	))
	sb.WriteString("\n")

	if summary.AssertionTotal > 0 {
		sb.WriteString(formatLine("Assertions:", fmt.Sprintf("%d total", summary.AssertionTotal), &labelStyle, &valueStyle))
		if summary.AssertionFailed > 0 {
			sb.WriteString(formatLine("Assertions Fail:", fmt.Sprintf("%d", summary.AssertionFailed), &labelStyle, &failStyle))
		} else {
			sb.WriteString(formatLine("Assertions Pass:", "all passed", &labelStyle, &successStyle))
		}
		sb.WriteString("\n")
	}

	// Cost and performance
	sb.WriteString(formatLine("Total Cost:", fmt.Sprintf("$%.4f", summary.TotalCost), &labelStyle, &valueStyle))
	sb.WriteString(formatLine("Total Tokens:", theme.FormatNumber(summary.TotalTokens), &labelStyle, &valueStyle))
	sb.WriteString(formatLine("Total Duration:", theme.FormatDuration(summary.TotalDuration), &labelStyle, &valueStyle))
	sb.WriteString(formatLine(
		"Avg Duration:",
		fmt.Sprintf("%s per run", theme.FormatDuration(summary.AvgDuration)),
		&labelStyle,
		&valueStyle,
	))

	sb.WriteString("\n")

	// Provider breakdown
	if len(summary.ProviderCounts) > 0 {
		providerList := make([]string, 0, len(summary.ProviderCounts))
		for provider, count := range summary.ProviderCounts {
			providerList = append(providerList, fmt.Sprintf("%s (%d)", provider, count))
		}
		sb.WriteString(formatLine("Providers:", strings.Join(providerList, ", "), &labelStyle, &valueStyle))
	}

	sb.WriteString(formatLine("Scenarios:", fmt.Sprintf("%d scenarios", summary.ScenarioCount), &labelStyle, &valueStyle))

	if len(summary.Regions) > 0 {
		sb.WriteString(formatLine("Regions:", strings.Join(summary.Regions, ", "), &labelStyle, &valueStyle))
	}

	// Errors
	if len(summary.Errors) > 0 {
		sb.WriteString("\n")
		sb.WriteString(labelStyle.Render("Errors:\n"))
		errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9")).MarginLeft(errorMarginLeft)
		for _, errInfo := range summary.Errors {
			runDesc := fmt.Sprintf("%s/%s/%s", errInfo.Scenario, errInfo.Provider, errInfo.Region)
			// Compact error message by removing extra whitespace and newlines
			compactError := compactString(errInfo.Error)
			sb.WriteString(errorStyle.Render(fmt.Sprintf("• %s: %s\n", runDesc, compactError)))
		}
	}

	// Output info
	sb.WriteString("\n")
	sb.WriteString(formatLine("Results saved to:", summary.OutputDir, &labelStyle, &valueStyle))

	if summary.HTMLReport != "" {
		sb.WriteString(formatLine("HTML Report:", summary.HTMLReport, &labelStyle, &valueStyle))
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
	sb.WriteString(fmt.Sprintf("Total Tokens:     %s\n", theme.FormatNumber(summary.TotalTokens)))
	sb.WriteString(fmt.Sprintf("Total Duration:   %s\n", theme.FormatDuration(summary.TotalDuration)))
	sb.WriteString(fmt.Sprintf("Avg Duration:     %s per run\n", theme.FormatDuration(summary.AvgDuration)))
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
			// Compact error message by removing extra whitespace and newlines
			compactError := compactString(errInfo.Error)
			sb.WriteString(fmt.Sprintf("  • %s: %s\n", runDesc, compactError))
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
