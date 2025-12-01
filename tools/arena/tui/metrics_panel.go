package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const (
	metricsPanelWidth        = 40
	metricsPaddingVertical   = 1
	metricsPaddingHorizontal = 2
)

func (m *Model) renderMetrics() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorEmerald))
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorLightGray))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorWhite)).Bold(true)
	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorGreen)).Bold(true)
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorRed)).Bold(true)
	costStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorYellow)).Bold(true)

	avgDuration := time.Duration(0)
	if m.completedCount > 0 {
		avgDuration = m.totalDuration / time.Duration(m.completedCount)
	}

	lines := []string{
		titleStyle.Render("📈 Metrics"),
		"",
		fmt.Sprintf(
			"%s %s",
			labelStyle.Render("Completed:"),
			valueStyle.Render(fmt.Sprintf("%d/%d", m.completedCount, m.totalRuns)),
		),
		fmt.Sprintf("%s %s", labelStyle.Render("Success:  "), successStyle.Render(fmt.Sprintf("%d", m.successCount))),
		fmt.Sprintf("%s %s", labelStyle.Render("Errors:   "), errorStyle.Render(fmt.Sprintf("%d", m.failedCount))),
		"",
		fmt.Sprintf("%s %s", labelStyle.Render("Total Cost:  "), costStyle.Render(fmt.Sprintf("$%.4f", m.totalCost))),
		fmt.Sprintf("%s %s", labelStyle.Render("Total Tokens:"), valueStyle.Render(formatNumber(m.totalTokens))),
		fmt.Sprintf("%s %s", labelStyle.Render("Avg Duration:"), valueStyle.Render(formatDuration(avgDuration))),
	}

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorGreen)).
		Padding(metricsPaddingVertical, metricsPaddingHorizontal).
		Width(metricsPanelWidth).
		Render(content)
}
