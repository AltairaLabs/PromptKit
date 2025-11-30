package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// MainPage renders the primary view with active runs and logs.
type MainPage struct{}

// Render builds the main page body.
func (MainPage) Render(m *Model) string {
	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.renderActiveRuns(),
		"",
		m.renderLogs(),
	)
}
