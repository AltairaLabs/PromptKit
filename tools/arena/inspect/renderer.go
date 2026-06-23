package inspect

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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

func printPromptsSection(data *InspectionData, opts RenderOptions) {
	fmt.Println(sectionHeaderStyle.Render(fmt.Sprintf(" 📋 Prompt Configs (%d) ", len(data.PromptConfigs))))
	fmt.Println()

	for i := range data.PromptConfigs {
		p := &data.PromptConfigs[i]
		lines := buildPromptLines(p, opts)
		fmt.Println(boxStyle.Render(strings.Join(lines, "\n")))
	}
	fmt.Println()
}

// buildPromptLines builds display lines for a prompt config.
// Verbose detail lines are only included when opts.Verbose is true.
func buildPromptLines(p *PromptInspectData, opts RenderOptions) []string {
	lines := []string{highlightStyle.Render(p.ID)}

	if p.TaskType != "" {
		lines = append(lines, labelStyle.Render(labelTaskType)+tagStyle.Render(p.TaskType))
	}
	lines = append(lines, labelStyle.Render(labelFile)+dimStyle.Render(p.File))
	if opts.Verbose {
		lines = append(lines, buildPromptVerboseLines(p)...)
	}
	return lines
}

// buildPromptVerboseLines builds verbose detail lines for a prompt
func buildPromptVerboseLines(p *PromptInspectData) []string {
	var lines []string
	if p.Description != "" {
		desc := truncateInspectString(p.Description, maxDescLengthPrompt)
		lines = append(lines, labelStyle.Render(labelDesc)+valueStyle.Render(desc))
	}
	if p.Version != "" {
		lines = append(lines, labelStyle.Render(labelVersion)+valueStyle.Render(p.Version))
	}
	if len(p.Variables) > 0 {
		lines = append(lines, labelStyle.Render(labelVariables)+tagStyle.Render(strings.Join(p.Variables, ", ")))
		for k, v := range p.VariableValues {
			lines = append(lines, dimStyle.Render("    "+k+": ")+valueStyle.Render(v))
		}
	}
	if len(p.AllowedTools) > 0 {
		lines = append(lines, labelStyle.Render(labelTools)+valueStyle.Render(strings.Join(p.AllowedTools, ", ")))
	}
	if len(p.Validators) > 0 {
		lines = append(lines, labelStyle.Render(labelValidators)+valueStyle.Render(strings.Join(p.Validators, ", ")))
	}
	if p.HasMedia {
		lines = append(lines, labelStyle.Render(labelMedia)+successStyle.Render("✓ enabled"))
	}
	return lines
}

func printProvidersSection(data *InspectionData, opts RenderOptions) {
	fmt.Println(sectionHeaderStyle.Render(fmt.Sprintf(" 🔌 Providers (%d) ", len(data.Providers))))
	fmt.Println()

	byGroup := groupProvidersByGroup(data.Providers)
	groups := getSortedGroups(byGroup)

	for _, group := range groups {
		fmt.Println(labelStyle.Render("Group: ") + tagStyle.Render(group))
		for _, p := range byGroup[group] {
			lines := buildProviderLines(&p, opts)
			fmt.Println(boxStyle.Render(strings.Join(lines, "\n")))
		}
	}
	fmt.Println()
}

// groupProvidersByGroup groups providers by their group field
func groupProvidersByGroup(providers []ProviderInspectData) map[string][]ProviderInspectData {
	byGroup := make(map[string][]ProviderInspectData)
	for _, p := range providers {
		group := p.Group
		if group == "" {
			group = "default"
		}
		byGroup[group] = append(byGroup[group], p)
	}
	return byGroup
}

// getSortedGroups returns sorted group names
func getSortedGroups(byGroup map[string][]ProviderInspectData) []string {
	groups := make([]string, 0, len(byGroup))
	for g := range byGroup {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	return groups
}

// buildProviderLines builds display lines for a provider.
// Temperature and MaxTokens are only included when opts.Verbose is true.
func buildProviderLines(p *ProviderInspectData, opts RenderOptions) []string {
	headerLine := highlightStyle.Render(p.ID)
	if p.Type != "" {
		headerLine += dimStyle.Render(" (") + valueStyle.Render(p.Type) + dimStyle.Render(")")
	}
	lines := []string{headerLine}

	if p.Model != "" {
		lines = append(lines, labelStyle.Render(labelModel)+valueStyle.Render(p.Model))
	}
	lines = append(lines, labelStyle.Render(labelFile)+dimStyle.Render(p.File))
	if opts.Verbose {
		if p.Temperature > 0 {
			lines = append(lines, labelStyle.Render(labelTemperature)+valueStyle.Render(fmt.Sprintf("%.2f", p.Temperature)))
		}
		if p.MaxTokens > 0 {
			lines = append(lines, labelStyle.Render(labelMaxTok)+valueStyle.Render(fmt.Sprintf("%d", p.MaxTokens)))
		}
	}
	return lines
}

func printScenariosSection(data *InspectionData, opts RenderOptions) {
	fmt.Println(sectionHeaderStyle.Render(fmt.Sprintf(" 🎬 Scenarios (%d) ", len(data.Scenarios))))
	fmt.Println()

	for i := range data.Scenarios {
		s := &data.Scenarios[i]
		lines := buildScenarioLines(s, opts)
		fmt.Println(boxStyle.Render(strings.Join(lines, "\n")))
	}
	fmt.Println()
}

// buildScenarioLines builds display lines for a scenario.
// Verbose detail lines are only included when opts.Verbose is true.
func buildScenarioLines(s *ScenarioInspectData, opts RenderOptions) []string {
	headerLine := highlightStyle.Render(s.ID)
	if s.Mode != "" {
		headerLine += dimStyle.Render(" [") + tagStyle.Render(s.Mode) + dimStyle.Render("]")
	}

	infoLine := labelStyle.Render(labelTask) + valueStyle.Render(s.TaskType)
	infoLine += dimStyle.Render(" • ") + labelStyle.Render("Turns: ") + valueStyle.Render(fmt.Sprintf("%d", s.TurnCount))

	lines := []string{headerLine, infoLine}
	if opts.Verbose {
		lines = append(lines, buildScenarioVerboseLines(s)...)
	}
	return lines
}

// buildScenarioVerboseLines builds verbose detail lines for a scenario
func buildScenarioVerboseLines(s *ScenarioInspectData) []string {
	var lines []string
	lines = append(lines, labelStyle.Render(labelFile)+dimStyle.Render(s.File))

	if s.Description != "" {
		desc := truncateInspectString(s.Description, maxDescLength)
		lines = append(lines, labelStyle.Render(labelDesc)+valueStyle.Render(desc))
	}

	flags := buildScenarioFlags(s)
	if len(flags) > 0 {
		lines = append(lines, labelStyle.Render(labelFlags)+strings.Join(flags, " "))
	}

	if len(s.Providers) > 0 {
		lines = append(lines, labelStyle.Render(labelProviders)+valueStyle.Render(strings.Join(s.Providers, ", ")))
	}
	return lines
}

// buildScenarioFlags builds the flags list for a scenario
func buildScenarioFlags(s *ScenarioInspectData) []string {
	var flags []string
	if s.HasSelfPlay {
		flags = append(flags, tagStyle.Render("self-play"))
	}
	if s.Streaming {
		flags = append(flags, tagStyle.Render("streaming"))
	}
	if s.AssertionCount > 0 {
		flags = append(flags, tagStyle.Render(fmt.Sprintf("%d turn assertions", s.AssertionCount)))
	}
	if s.ConvAssertionCount > 0 {
		flags = append(flags, tagStyle.Render(fmt.Sprintf("%d conv assertions", s.ConvAssertionCount)))
	}
	return flags
}

func printToolsSection(data *InspectionData, opts RenderOptions) {
	fmt.Println(sectionHeaderStyle.Render(fmt.Sprintf(" 🔧 Tools (%d) ", len(data.Tools))))
	fmt.Println()

	for _, t := range data.Tools {
		lines := buildToolLines(&t, opts)
		fmt.Println(boxStyle.Render(strings.Join(lines, "\n")))
	}
	fmt.Println()
}

// buildToolLines builds display lines for a tool.
// Verbose detail lines are only included when opts.Verbose is true.
func buildToolLines(t *ToolInspectData, opts RenderOptions) []string {
	var lines []string

	// Header with name or filename
	header := t.Name
	if header == "" {
		header = filepath.Base(t.File)
	}
	lines = append(lines, highlightStyle.Render(header))

	if t.Mode != "" {
		lines = append(lines, labelStyle.Render(labelMode)+tagStyle.Render(getToolModeDisplay(t.Mode)))
	}

	if t.Name != "" {
		lines = append(lines, labelStyle.Render(labelFile)+dimStyle.Render(filepath.Base(t.File)))
	}

	if opts.Verbose {
		lines = append(lines, buildToolVerboseLines(t)...)
	}
	return lines
}

// getToolModeDisplay returns a display string for tool mode
func getToolModeDisplay(mode string) string {
	switch mode {
	case "mock":
		return "🧪 mock"
	case "live":
		return "🔴 live"
	default:
		return mode
	}
}

// buildToolVerboseLines builds verbose detail lines for a tool
func buildToolVerboseLines(t *ToolInspectData) []string {
	var lines []string
	if t.Description != "" {
		desc := truncateInspectString(t.Description, maxDescLength)
		lines = append(lines, labelStyle.Render(labelDesc)+valueStyle.Render(desc))
	}
	if len(t.InputParams) > 0 {
		lines = append(lines, labelStyle.Render(labelParams)+tagStyle.Render(strings.Join(t.InputParams, ", ")))
	}
	if t.TimeoutMs > 0 {
		lines = append(lines, labelStyle.Render(labelTimeout)+valueStyle.Render(fmt.Sprintf("%dms", t.TimeoutMs)))
	}

	var flags []string
	if t.HasMockData {
		flags = append(flags, "mock data")
	}
	if t.HasHTTPConfig {
		flags = append(flags, "HTTP")
	}
	if len(flags) > 0 {
		lines = append(lines, labelStyle.Render(labelFeatures)+dimStyle.Render(strings.Join(flags, ", ")))
	}
	return lines
}

func printPersonasSection(data *InspectionData, opts RenderOptions) {
	// Combined Self-Play section showing personas and roles together
	header := fmt.Sprintf(" 🎭 Self-Play (%d personas, %d roles) ", len(data.Personas), len(data.SelfPlayRoles))
	fmt.Println(sectionHeaderStyle.Render(header))
	fmt.Println()

	// Show personas
	if len(data.Personas) > 0 {
		fmt.Println(labelStyle.Render("Personas:"))
		for _, p := range data.Personas {
			lines := buildPersonaLines(&p, opts)
			fmt.Println(boxStyle.Render(strings.Join(lines, "\n")))
		}
	}

	// Show self-play roles
	if len(data.SelfPlayRoles) > 0 {
		fmt.Println(labelStyle.Render("Roles:"))
		var lines []string
		for _, role := range data.SelfPlayRoles {
			line := highlightStyle.Render(role.ID)
			if role.Persona != "" {
				line += dimStyle.Render(" (") + valueStyle.Render(role.Persona) + dimStyle.Render(")")
			}
			if role.Provider != "" {
				line += dimStyle.Render(" → ") + valueStyle.Render(role.Provider)
			}
			lines = append(lines, line)
		}
		fmt.Println(boxStyle.Render(strings.Join(lines, "\n")))
	}
	fmt.Println()
}

// buildPersonaLines builds display lines for a persona.
// Description and Goals are only included when opts.Verbose is true.
func buildPersonaLines(p *PersonaInspectData, opts RenderOptions) []string {
	lines := []string{highlightStyle.Render(p.ID)}

	if opts.Verbose {
		if p.Description != "" {
			desc := truncateInspectString(p.Description, maxDescLength)
			lines = append(lines, labelStyle.Render(labelDesc)+valueStyle.Render(desc))
		}
		if len(p.Goals) > 0 {
			lines = append(lines, labelStyle.Render(labelGoals))
			lines = append(lines, buildGoalLines(p.Goals)...)
		}
	}
	return lines
}

// buildGoalLines builds display lines for persona goals (limited to maxGoalsDisplayed)
func buildGoalLines(goals []string) []string {
	var lines []string
	for i, goal := range goals {
		if i >= maxGoalsDisplayed {
			remaining := len(goals) - maxGoalsDisplayed
			lines = append(lines, dimStyle.Render(fmt.Sprintf("    ... and %d more", remaining)))
			break
		}
		goalStr := truncateInspectString(goal, maxGoalLength)
		lines = append(lines, dimStyle.Render("    • ")+valueStyle.Render(goalStr))
	}
	return lines
}

func printJudgesSection(data *InspectionData) {
	fmt.Println(sectionHeaderStyle.Render(fmt.Sprintf(" ⚖️  Judges (%d) ", len(data.Judges))))
	fmt.Println()

	var lines []string
	for _, j := range data.Judges {
		line := highlightStyle.Render(j.Name)
		line += dimStyle.Render(" → ") + valueStyle.Render(j.Provider)
		if j.Model != "" {
			line += dimStyle.Render(" (") + valueStyle.Render(j.Model) + dimStyle.Render(")")
		}
		lines = append(lines, line)
	}
	fmt.Println(boxStyle.Render(strings.Join(lines, "\n")))
	fmt.Println()
}

func printDefaultsSection(data *InspectionData, _ RenderOptions) {
	fmt.Println(sectionHeaderStyle.Render(" ⚙️  Defaults "))
	fmt.Println()

	var lines []string
	d := data.Defaults

	if d.Temperature > 0 {
		lines = append(lines, labelStyle.Render("Temperature: ")+valueStyle.Render(fmt.Sprintf("%.2f", d.Temperature)))
	}
	if d.MaxTokens > 0 {
		lines = append(lines, labelStyle.Render("Max Tokens: ")+valueStyle.Render(fmt.Sprintf("%d", d.MaxTokens)))
	}
	if d.Seed > 0 {
		lines = append(lines, labelStyle.Render("Seed: ")+valueStyle.Render(fmt.Sprintf("%d", d.Seed)))
	}
	if d.Concurrency > 0 {
		lines = append(lines, labelStyle.Render("Concurrency: ")+valueStyle.Render(fmt.Sprintf("%d", d.Concurrency)))
	}
	if d.OutputDir != "" {
		lines = append(lines, labelStyle.Render("Output Dir: ")+valueStyle.Render(d.OutputDir))
	}
	if len(d.OutputFormats) > 0 {
		lines = append(lines, labelStyle.Render("Formats: ")+valueStyle.Render(strings.Join(d.OutputFormats, ", ")))
	}

	if len(lines) > 0 {
		fmt.Println(boxStyle.Render(strings.Join(lines, "\n")))
	}
	fmt.Println()
}

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

// printCacheStatistics prints cache statistics when --stats is enabled.
func printCacheStatistics(stats *CacheStatsData) {
	fmt.Println(sectionHeaderStyle.Render(" 📊 Cache Statistics "))
	fmt.Println()

	var lines []string

	promptVal := valueStyle.Render(fmt.Sprintf("%d entries", stats.PromptCache.Size))
	lines = append(lines, labelStyle.Render("Prompt Cache: ")+promptVal)
	if len(stats.PromptCache.Entries) > 0 {
		lines = append(lines, dimStyle.Render("  "+strings.Join(stats.PromptCache.Entries, ", ")))
	}

	if stats.FragmentCache.Size > 0 {
		fragVal := valueStyle.Render(fmt.Sprintf("%d entries", stats.FragmentCache.Size))
		lines = append(lines, labelStyle.Render("Fragment Cache: ")+fragVal)
	}

	if stats.SelfPlayCache.Size > 0 {
		spVal := valueStyle.Render(fmt.Sprintf("%d pairs", stats.SelfPlayCache.Size))
		lines = append(lines, labelStyle.Render("Self-Play Cache: ")+spVal)
		if len(stats.SelfPlayCache.Entries) > 0 {
			lines = append(lines, dimStyle.Render("  "+strings.Join(stats.SelfPlayCache.Entries, ", ")))
		}
	}

	if len(lines) > 0 {
		fmt.Println(boxStyle.Render(strings.Join(lines, "\n")))
	}
	fmt.Println()
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
