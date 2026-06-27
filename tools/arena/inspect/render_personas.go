package inspect

import (
	"fmt"
)

func printPersonasSection(data *InspectionData, opts RenderOptions) {
	// Combined Self-Play section showing personas and roles together
	header := fmt.Sprintf(" 🎭 Self-Play (%d personas, %d roles) ", len(data.Personas), len(data.SelfPlayRoles))
	fmt.Println(sectionHeaderStyle.Render(header))
	fmt.Println()

	// Show personas as a grid.
	if len(data.Personas) > 0 {
		fmt.Println(labelStyle.Render("Personas:"))
		sets := make([][]string, len(data.Personas))
		for i := range data.Personas {
			sets[i] = buildPersonaLines(&data.Personas[i], opts)
		}
		printBoxes(sets)
	}

	// Show self-play roles in a single box.
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
		printBoxes([][]string{lines})
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
