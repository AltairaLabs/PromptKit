package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/viewmodels"
)

// SummaryView renders the final execution summary
type SummaryView struct {
	width  int
	ciMode bool
}

// NewSummaryView creates a new summary view
func NewSummaryView(width int, ciMode bool) *SummaryView {
	return &SummaryView{
		width:  width,
		ciMode: ciMode,
	}
}

// Render renders the summary using the provided ViewModel
func (v *SummaryView) Render(vm *viewmodels.SummaryViewModel) string {
	if v.ciMode {
		return v.renderCIMode(vm)
	}
	return v.renderTUIMode(vm)
}

func (v *SummaryView) renderTUIMode(vm *viewmodels.SummaryViewModel) string {
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
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	failStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))

	sb.WriteString(formatLine("Total Runs:", vm.GetFormattedTotalRuns(), &labelStyle, &valueStyle))
	sb.WriteString(formatLine("Successful:", vm.GetFormattedSuccessful(), &labelStyle, &successStyle))
	sb.WriteString(formatLine("Failed:", vm.GetFormattedFailed(), &labelStyle, &failStyle))
	sb.WriteString("\n")

	// Assertions (if any)
	if vm.HasAssertions() {
		sb.WriteString(formatLine("Assertions:", vm.GetFormattedAssertionTotal(), &labelStyle, &valueStyle))
		if vm.HasFailedAssertions() {
			sb.WriteString(formatLine("Assertions Fail:", vm.GetFormattedAssertionFailed(), &labelStyle, &failStyle))
		} else {
			sb.WriteString(formatLine("Assertions Pass:", "all passed", &labelStyle, &successStyle))
		}
		sb.WriteString("\n")
	}

	// Cost and performance
	sb.WriteString(formatLine("Total Cost:", vm.GetFormattedTotalCost(), &labelStyle, &valueStyle))
	sb.WriteString(formatLine("Total Tokens:", vm.GetFormattedTotalTokens(), &labelStyle, &valueStyle))
	sb.WriteString(formatLine("Total Duration:", vm.GetFormattedTotalDuration(), &labelStyle, &valueStyle))
	sb.WriteString(formatLine("Avg Duration:", vm.GetFormattedAvgDurationWithSuffix(), &labelStyle, &valueStyle))
	sb.WriteString("\n")

	// Provider breakdown
	if vm.HasProviders() {
		sb.WriteString(formatLine("Providers:", vm.GetFormattedProviders(), &labelStyle, &valueStyle))
	}

	sb.WriteString(formatLine("Scenarios:", vm.GetFormattedScenarios(), &labelStyle, &valueStyle))

	if vm.HasRegions() {
		sb.WriteString(formatLine("Regions:", vm.GetFormattedRegions(), &labelStyle, &valueStyle))
	}

	// Errors
	if vm.HasErrors() {
		sb.WriteString("\n")
		sb.WriteString(labelStyle.Render("Errors:\n"))
		const errorMarginLeft = 2
		errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9")).MarginLeft(errorMarginLeft)
		for _, errStr := range vm.GetFormattedErrors() {
			sb.WriteString(errorStyle.Render(fmt.Sprintf("• %s\n", errStr)))
		}
	}

	// Output info
	sb.WriteString("\n")
	sb.WriteString(formatLine("Results saved to:", vm.GetOutputDir(), &labelStyle, &valueStyle))

	if vm.HasHTMLReport() {
		sb.WriteString(formatLine("HTML Report:", vm.GetHTMLReport(), &labelStyle, &valueStyle))
	}

	return sb.String()
}

func (v *SummaryView) renderCIMode(vm *viewmodels.SummaryViewModel) string {
	var sb strings.Builder

	// Header
	sb.WriteString("╔═══════════════════════════════════════════════════════════╗\n")
	sb.WriteString("║                   Run Summary                             ║\n")
	sb.WriteString("╚═══════════════════════════════════════════════════════════╝\n\n")

	// Execution stats
	sb.WriteString(fmt.Sprintf("Total Runs:       %s\n", vm.GetFormattedTotalRuns()))
	sb.WriteString(fmt.Sprintf("Successful:       %s\n", vm.GetFormattedSuccessful()))
	sb.WriteString(fmt.Sprintf("Failed:           %s\n", vm.GetFormattedFailed()))
	sb.WriteString("\n")

	// Cost and performance
	sb.WriteString(fmt.Sprintf("Total Cost:       %s\n", vm.GetFormattedTotalCost()))
	sb.WriteString(fmt.Sprintf("Total Tokens:     %s\n", vm.GetFormattedTotalTokens()))
	sb.WriteString(fmt.Sprintf("Total Duration:   %s\n", vm.GetFormattedTotalDuration()))
	sb.WriteString(fmt.Sprintf("Avg Duration:     %s\n", vm.GetFormattedAvgDurationWithSuffix()))
	sb.WriteString("\n")

	// Provider breakdown
	if vm.HasProviders() {
		sb.WriteString(fmt.Sprintf("Providers:        %s\n", vm.GetFormattedProviders()))
	}

	sb.WriteString(fmt.Sprintf("Scenarios:        %s\n", vm.GetFormattedScenarios()))

	if vm.HasRegions() {
		sb.WriteString(fmt.Sprintf("Regions:          %s\n", vm.GetFormattedRegions()))
	}

	// Errors
	if vm.HasErrors() {
		sb.WriteString("\nErrors:\n")
		for _, errStr := range vm.GetFormattedErrors() {
			sb.WriteString(fmt.Sprintf("  • %s\n", errStr))
		}
	}

	// Output info
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("Results saved to: %s\n", vm.GetOutputDir()))

	if vm.HasHTMLReport() {
		sb.WriteString(fmt.Sprintf("HTML Report:      %s\n", vm.GetHTMLReport()))
	}

	return sb.String()
}

func formatLine(label, value string, labelStyle, valueStyle *lipgloss.Style) string {
	const labelWidth = 16
	lbl := labelStyle.Render(fmt.Sprintf("%-*s", labelWidth, label))
	val := valueStyle.Render(value)
	return fmt.Sprintf("%s %s\n", lbl, val)
}
