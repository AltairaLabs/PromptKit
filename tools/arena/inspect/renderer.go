package inspect

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/theme"
)

const (
	boxWidth            = 78
	maxDescLength       = 55
	maxDescLengthPrompt = 60
	maxGoalsDisplayed   = 3
	maxGoalLength       = 50

	// Label constants for display output
	labelFile        = "  File: "
	labelDesc        = "  Desc: "
	labelModel       = "  Model: "
	labelTemperature = "  Temperature: "
	labelMaxTok      = "  Max Tokens: "
	labelTaskType    = "  Task Type: "
	labelTask        = "  Task: "
	labelVersion     = "  Version: "
	labelVariables   = "  Variables: "
	labelTools       = "  Tools: "
	labelValidators  = "  Validators: "
	labelMedia       = "  Media: "
	labelMode        = "  Mode: "
	labelParams      = "  Params: "
	labelTimeout     = "  Timeout: "
	labelFeatures    = "  Features: "
	labelFlags       = "  Flags: "
	labelProviders   = "  Providers: "
	labelGoals       = "  Goals:"

	// Section names
	sectionValidation = "validation"
)

// Style definitions for terminal output
var (
	// Box styles
	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(theme.ColorPrimary)).
			Padding(0, 1).
			Width(boxWidth)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(theme.ColorPrimary)).
			MarginBottom(1)

	sectionHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color(theme.ColorWhite)).
				Background(lipgloss.Color(theme.ColorPrimary)).
				Padding(0, 1).
				MarginTop(1)

	labelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ColorLightGray))

	valueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ColorWhite))

	highlightStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ColorEmerald)).
			Bold(true)

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ColorGray))

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ColorSuccess))

	warningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ColorWarning))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ColorError))

	tagStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ColorPrimary)).
			Background(lipgloss.Color("#2D2D3D")).
			Padding(0, 1)
)

// RenderText renders all inspection sections to a string.
// opts controls which sections and detail levels are included.
// It captures stdout so that the lipgloss Print calls are collected into
// the returned string.
func RenderText(data *InspectionData, opts RenderOptions) string {
	// Capture everything written to stdout during rendering.
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		// Fallback: render directly to stdout and return empty string.
		outputText(data, opts)
		return ""
	}
	os.Stdout = w

	outputText(data, opts)

	// Restore stdout and collect output.
	_ = w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	_ = r.Close()

	return buf.String()
}

func outputText(data *InspectionData, opts RenderOptions) {
	printBanner(data.ConfigFile)
	printSections(data, opts)
}

// printBanner prints the inspector header banner
func printBanner(configFile string) {
	banner := headerStyle.Render("✨ PromptArena Configuration Inspector ✨")
	fmt.Println(banner)
	fmt.Println()
	fmt.Println(boxStyle.Render(
		labelStyle.Render("Configuration: ") + highlightStyle.Render(configFile),
	))
	fmt.Println()
}

// getSectionVisibility determines which sections to show.
// Pass an empty string to show all sections.
func getSectionVisibility(section string) SectionVisibility {
	showAll := section == ""
	return SectionVisibility{
		All:        showAll,
		Prompts:    showAll || section == "prompts",
		Providers:  showAll || section == "providers",
		Scenarios:  showAll || section == "scenarios",
		Tools:      showAll || section == "tools",
		Selfplay:   showAll || section == "selfplay" || section == "personas",
		Judges:     showAll || section == "judges",
		Defaults:   showAll || section == "defaults",
		Validation: showAll || section == sectionValidation,
	}
}

// printSections prints all visible sections
func printSections(data *InspectionData, opts RenderOptions) {
	vis := getSectionVisibility(opts.Section)
	printConfigSections(data, vis, opts)
	printSummarySections(data, vis, opts)
	if opts.Stats && data.CacheStats != nil {
		printCacheStatistics(data.CacheStats)
	}
}

// printConfigSections prints the main configuration sections
func printConfigSections(data *InspectionData, vis SectionVisibility, opts RenderOptions) {
	if vis.Prompts && len(data.PromptConfigs) > 0 {
		printPromptsSection(data, opts)
	}
	if vis.Providers && len(data.Providers) > 0 {
		printProvidersSection(data, opts)
	}
	if vis.Scenarios && len(data.Scenarios) > 0 {
		printScenariosSection(data, opts)
	}
	if vis.Tools && len(data.Tools) > 0 {
		printToolsSection(data, opts)
	}
	if vis.Selfplay && (len(data.Personas) > 0 || len(data.SelfPlayRoles) > 0) {
		printPersonasSection(data, opts)
	}
	if vis.Judges && len(data.Judges) > 0 {
		printJudgesSection(data)
	}
}

// printSummarySections prints the summary sections (defaults, validation)
func printSummarySections(data *InspectionData, vis SectionVisibility, opts RenderOptions) {
	if vis.Defaults && data.Defaults != nil {
		printDefaultsSection(data, opts)
	}
	if vis.Validation {
		printValidationSection(data)
	}
}

// truncateInspectString truncates a string to the given length with ellipsis
func truncateInspectString(s string, maxLen int) string {
	// Remove newlines and extra whitespace
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
