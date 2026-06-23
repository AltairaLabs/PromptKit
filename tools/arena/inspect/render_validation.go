package inspect

import (
	"fmt"
	"strings"
)

func printValidationSection(data *InspectionData) {
	fmt.Println(sectionHeaderStyle.Render(" ✅ Validation "))
	fmt.Println()

	lines := buildValidationLines(data)
	fmt.Println(boxStyle.Render(strings.Join(lines, "\n")))
	fmt.Println()
}

// buildValidationLines builds all display lines for validation section
func buildValidationLines(data *InspectionData) []string {
	failedCount := countFailedChecks(data.ValidationChecks)
	var lines []string

	// Summary line
	if data.ValidationPassed && failedCount == 0 {
		lines = append(lines, successStyle.Render("✓ Configuration is valid"))
	} else {
		lines = append(lines, errorStyle.Render("✗ Configuration has errors"))
	}

	// Show check results as a checklist
	if len(data.ValidationChecks) > 0 {
		lines = append(lines, "", labelStyle.Render("Connectivity Checks:"))
		lines = append(lines, buildCheckLines(data.ValidationChecks)...)
	}

	// Show validation error details
	if !data.ValidationPassed && len(data.ValidationErrors) > 0 {
		lines = append(lines, "", errorStyle.Render("Errors:"))
		for _, errMsg := range data.ValidationErrors {
			lines = append(lines, errorStyle.Render("  ☒ ")+valueStyle.Render(errMsg))
		}
	}

	// Show actionable warnings
	actionableWarnings := filterActionableWarnings(data.ValidationWarningDetails)
	if len(actionableWarnings) > 0 {
		lines = append(lines, "", dimStyle.Render("Notes:"))
		for _, warn := range actionableWarnings {
			lines = append(lines, dimStyle.Render("  ℹ "+warn))
		}
	}

	return lines
}

// countFailedChecks counts validation checks that failed (not warnings)
func countFailedChecks(checks []ValidationCheckData) int {
	count := 0
	for _, check := range checks {
		if !check.Passed && !check.Warning {
			count++
		}
	}
	return count
}

// buildCheckLines builds display lines for validation checks
func buildCheckLines(checks []ValidationCheckData) []string {
	var lines []string
	for _, check := range checks {
		lines = append(lines, buildSingleCheckLine(check)...)
	}
	return lines
}

// buildSingleCheckLine builds display lines for a single validation check
func buildSingleCheckLine(check ValidationCheckData) []string {
	var lines []string
	if check.Passed {
		lines = append(lines, successStyle.Render("  ☑ ")+valueStyle.Render(check.Name))
	} else if check.Warning {
		lines = append(lines, warningStyle.Render("  ⚠ ")+valueStyle.Render(check.Name))
		for _, issue := range check.Issues {
			lines = append(lines, dimStyle.Render("      • "+issue))
		}
	} else {
		lines = append(lines, errorStyle.Render("  ☒ ")+valueStyle.Render(check.Name))
		for _, issue := range check.Issues {
			lines = append(lines, errorStyle.Render("      • "+issue))
		}
	}
	return lines
}

// filterActionableWarnings filters out informational "not defined" warnings
// and keeps only warnings that suggest something might need fixing
func filterActionableWarnings(warnings []string) []string {
	var actionable []string
	for _, w := range warnings {
		// Skip informational "not defined" messages for optional features
		if strings.Contains(w, "no personas defined") ||
			strings.Contains(w, "no scenarios defined") ||
			strings.Contains(w, "no prompt configs defined") ||
			strings.Contains(w, "no providers defined") {
			continue
		}
		actionable = append(actionable, w)
	}
	return actionable
}
