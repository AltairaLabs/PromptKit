package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// MainPage renders the primary view with active runs and logs.
type MainPage struct{}

// Render builds the main page body.
func (MainPage) Render(m *Model) string {
	top := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.renderActiveRuns(),
		m.renderResultPane(),
	)

	bottom := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.renderLogs(),
		m.renderSummaryPane(),
	)

	return lipgloss.JoinVertical(lipgloss.Left, top, "", bottom)
}
