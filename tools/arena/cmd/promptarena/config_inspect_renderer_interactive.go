package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/theme"
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

func outputText(data *InspectionData, _ *config.Config) error {
	printBanner(data.ConfigFile)
	printSections(data)
	return nil
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

// getSectionVisibility determines which sections to show based on inspectSection flag
func getSectionVisibility() sectionVisibility {
	showAll := inspectSection == ""
	return sectionVisibility{
		all:        showAll,
		prompts:    showAll || inspectSection == "prompts",
		providers:  showAll || inspectSection == "providers",
		scenarios:  showAll || inspectSection == "scenarios",
		tools:      showAll || inspectSection == "tools",
		selfplay:   showAll || inspectSection == "selfplay" || inspectSection == "personas",
		judges:     showAll || inspectSection == "judges",
		defaults:   showAll || inspectSection == "defaults",
		validation: showAll || inspectSection == sectionValidation,
	}
}

// printSections prints all visible sections
func printSections(data *InspectionData) {
	vis := getSectionVisibility()
	printConfigSections(data, vis)
	printSummarySections(data, vis)
}

// printConfigSections prints the main configuration sections
func printConfigSections(data *InspectionData, vis sectionVisibility) {
	if vis.prompts && len(data.PromptConfigs) > 0 {
		printPromptsSection(data)
	}
	if vis.providers && len(data.Providers) > 0 {
		printProvidersSection(data)
	}
	if vis.scenarios && len(data.Scenarios) > 0 {
		printScenariosSection(data)
	}
	if vis.tools && len(data.Tools) > 0 {
		printToolsSection(data)
	}
	if vis.selfplay && (len(data.Personas) > 0 || len(data.SelfPlayRoles) > 0) {
		printPersonasSection(data)
	}
	if vis.judges && len(data.Judges) > 0 {
		printJudgesSection(data)
	}
}

// printSummarySections prints the summary sections (defaults, validation, cache)
func printSummarySections(data *InspectionData, vis sectionVisibility) {
	if vis.defaults && data.Defaults != nil {
		printDefaultsSection(data)
	}
	if vis.validation {
		printValidationSection(data)
	}
	if inspectStats && data.CacheStats != nil {
		printCacheStatistics(data.CacheStats)
	}
}

func printPromptsSection(data *InspectionData) {
	fmt.Println(sectionHeaderStyle.Render(fmt.Sprintf(" 📋 Prompt Configs (%d) ", len(data.PromptConfigs))))
	fmt.Println()

	for i := range data.PromptConfigs {
		p := &data.PromptConfigs[i]
		lines := buildPromptLines(p)
		fmt.Println(boxStyle.Render(strings.Join(lines, "\n")))
	}
	fmt.Println()
}

// buildPromptLines builds display lines for a prompt config
func buildPromptLines(p *PromptInspectData) []string {
	lines := []string{highlightStyle.Render(p.ID)}

	if p.TaskType != "" {
		lines = append(lines, labelStyle.Render(labelTaskType)+tagStyle.Render(p.TaskType))
	}
	lines = append(lines, labelStyle.Render(labelFile)+dimStyle.Render(p.File))

	if inspectVerbose {
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

func printProvidersSection(data *InspectionData) {
	fmt.Println(sectionHeaderStyle.Render(fmt.Sprintf(" 🔌 Providers (%d) ", len(data.Providers))))
	fmt.Println()

	byGroup := groupProvidersByGroup(data.Providers)
	groups := getSortedGroups(byGroup)

	for _, group := range groups {
		fmt.Println(labelStyle.Render("Group: ") + tagStyle.Render(group))
		for _, p := range byGroup[group] {
			lines := buildProviderLines(&p)
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

// buildProviderLines builds display lines for a provider
func buildProviderLines(p *ProviderInspectData) []string {
	headerLine := highlightStyle.Render(p.ID)
	if p.Type != "" {
		headerLine += dimStyle.Render(" (") + valueStyle.Render(p.Type) + dimStyle.Render(")")
	}
	lines := []string{headerLine}

	if p.Model != "" {
		lines = append(lines, labelStyle.Render(labelModel)+valueStyle.Render(p.Model))
	}
	if inspectVerbose {
		lines = append(lines, labelStyle.Render(labelFile)+dimStyle.Render(p.File))
		if p.Temperature > 0 {
			lines = append(lines, labelStyle.Render(labelTemperature)+valueStyle.Render(fmt.Sprintf("%.2f", p.Temperature)))
		}
		if p.MaxTokens > 0 {
			lines = append(lines, labelStyle.Render(labelMaxTok)+valueStyle.Render(fmt.Sprintf("%d", p.MaxTokens)))
		}
	}
	return lines
}

func printScenariosSection(data *InspectionData) {
	fmt.Println(sectionHeaderStyle.Render(fmt.Sprintf(" 🎬 Scenarios (%d) ", len(data.Scenarios))))
	fmt.Println()

	for i := range data.Scenarios {
		s := &data.Scenarios[i]
		lines := buildScenarioLines(s)
		fmt.Println(boxStyle.Render(strings.Join(lines, "\n")))
	}
	fmt.Println()
}

// buildScenarioLines builds display lines for a scenario
func buildScenarioLines(s *ScenarioInspectData) []string {
	headerLine := highlightStyle.Render(s.ID)
	if s.Mode != "" {
		headerLine += dimStyle.Render(" [") + tagStyle.Render(s.Mode) + dimStyle.Render("]")
	}

	infoLine := labelStyle.Render(labelTask) + valueStyle.Render(s.TaskType)
	infoLine += dimStyle.Render(" • ") + labelStyle.Render("Turns: ") + valueStyle.Render(fmt.Sprintf("%d", s.TurnCount))

	lines := []string{headerLine, infoLine}
	if inspectVerbose {
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

func printToolsSection(data *InspectionData) {
	fmt.Println(sectionHeaderStyle.Render(fmt.Sprintf(" 🔧 Tools (%d) ", len(data.Tools))))
	fmt.Println()

	for _, t := range data.Tools {
		lines := buildToolLines(&t)
		fmt.Println(boxStyle.Render(strings.Join(lines, "\n")))
	}
	fmt.Println()
}

// buildToolLines builds display lines for a tool
func buildToolLines(t *ToolInspectData) []string {
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

	if inspectVerbose {
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

func printPersonasSection(data *InspectionData) {
	// Combined Self-Play section showing personas and roles together
	header := fmt.Sprintf(" 🎭 Self-Play (%d personas, %d roles) ", len(data.Personas), len(data.SelfPlayRoles))
	fmt.Println(sectionHeaderStyle.Render(header))
	fmt.Println()

	// Show personas
	if len(data.Personas) > 0 {
		fmt.Println(labelStyle.Render("Personas:"))
		for _, p := range data.Personas {
			lines := buildPersonaLines(&p)
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

// buildPersonaLines builds display lines for a persona
func buildPersonaLines(p *PersonaInspectData) []string {
	lines := []string{highlightStyle.Render(p.ID)}

	if inspectVerbose {
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

func printDefaultsSection(data *InspectionData) {
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

// printCacheStatistics prints the cache statistics section
func printCacheStatistics(cacheStats *CacheStatsData) {
	fmt.Println(sectionHeaderStyle.Render(" 💾 Cache Statistics "))
	fmt.Println()

	var lines []string

	promptCacheEntry := fmt.Sprintf(entriesFormat, cacheStats.PromptCache.Size)
	lines = append(lines, labelStyle.Render("Prompt Cache: ")+valueStyle.Render(promptCacheEntry))
	if inspectVerbose && len(cacheStats.PromptCache.Entries) > 0 {
		for _, entry := range cacheStats.PromptCache.Entries {
			lines = append(lines, dimStyle.Render("  • "+entry))
		}
	}

	fragmentCacheEntry := fmt.Sprintf(entriesFormat, cacheStats.FragmentCache.Size)
	lines = append(lines, labelStyle.Render("Fragment Cache: ")+valueStyle.Render(fragmentCacheEntry))

	if cacheStats.SelfPlayCache.Size > 0 {
		selfPlayEntry := fmt.Sprintf(entriesFormat, cacheStats.SelfPlayCache.Size)
		lines = append(lines, labelStyle.Render("Self-Play Cache: ")+valueStyle.Render(selfPlayEntry))
		if cacheStats.SelfPlayCache.HitRate > 0 {
			hitRate := cacheStats.SelfPlayCache.HitRate * percentMultiplier
			lines = append(lines, dimStyle.Render(fmt.Sprintf("  Hit Rate: %.1f%%", hitRate)))
		}
	}

	fmt.Println(boxStyle.Render(strings.Join(lines, "\n")))
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
