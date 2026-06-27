package inspect

import (
	"fmt"
	"strings"
)

func printPromptsSection(data *InspectionData, opts RenderOptions) {
	fmt.Println(sectionHeaderStyle.Render(fmt.Sprintf(" 📋 Prompt Configs (%d) ", len(data.PromptConfigs))))
	fmt.Println()

	sets := make([][]string, len(data.PromptConfigs))
	for i := range data.PromptConfigs {
		sets[i] = buildPromptLines(&data.PromptConfigs[i], opts)
	}
	printBoxes(sets)
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
