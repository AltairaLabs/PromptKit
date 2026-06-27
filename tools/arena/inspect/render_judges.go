package inspect

import (
	"fmt"
)

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
	printBoxes([][]string{lines})
	fmt.Println()
}
