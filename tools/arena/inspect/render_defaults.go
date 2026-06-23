package inspect

import (
	"fmt"
	"strings"
)

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
