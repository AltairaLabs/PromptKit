package inspect

import (
	"fmt"
	"path/filepath"
	"strings"
)

func printToolsSection(data *InspectionData, opts RenderOptions) {
	fmt.Println(sectionHeaderStyle.Render(fmt.Sprintf(" 🔧 Tools (%d) ", len(data.Tools))))
	fmt.Println()

	sets := make([][]string, len(data.Tools))
	for i := range data.Tools {
		sets[i] = buildToolLines(&data.Tools[i], opts)
	}
	printBoxes(sets)
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
