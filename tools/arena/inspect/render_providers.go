package inspect

import (
	"fmt"
	"sort"
	"strings"
)

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
