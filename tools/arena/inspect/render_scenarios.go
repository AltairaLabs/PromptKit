package inspect

import (
	"fmt"
	"strings"
)

func printScenariosSection(data *InspectionData, opts RenderOptions) {
	fmt.Println(sectionHeaderStyle.Render(fmt.Sprintf(" 🎬 Scenarios (%d) ", len(data.Scenarios))))
	fmt.Println()

	sets := make([][]string, len(data.Scenarios))
	for i := range data.Scenarios {
		sets[i] = buildScenarioLines(&data.Scenarios[i], opts)
	}
	printBoxes(sets)
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
