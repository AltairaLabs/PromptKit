package inspect

import (
	"fmt"
	"strings"
)

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
