package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

func (m *Model) renderLogs() string {
	// Update viewport dimensions
	if m.viewportReady {
		viewportHeight := m.height / 3 // Leave room for header + active runs
		if viewportHeight < 5 {
			viewportHeight = 5
		}
		viewportWidth := m.width - 50 // Leave room for metrics (40) + padding
		if viewportWidth < 40 {
			viewportWidth = 40
		}

		m.logViewport.Width = viewportWidth
		m.logViewport.Height = viewportHeight

		if m.viewMode == viewSummary && m.summary != nil {
			m.logViewport.SetContent("Summary view (press 'l' to view logs)")
		} else if len(m.logs) == 0 {
			m.logViewport.SetContent("No logs yet...")
		} else {
			logLines := make([]string, len(m.logs))
			for i, log := range m.logs {
				logLines[i] = m.formatLogLine(log)
			}
			m.logViewport.SetContent(strings.Join(logLines, "\n"))
		}
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorSky))
	title := titleStyle.Render("📝 Logs (↑/↓ to scroll, 's' summary)")

	borderColor := lipgloss.Color(colorLightBlue)
	if m.activePane != paneLogs {
		borderColor = lipgloss.Color(colorGray)
	}

	if selected := m.selectedRun(); selected != nil && (selected.Status == StatusCompleted || selected.Status == StatusFailed) && m.stateStore != nil {
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Padding(1, 2).
			Render(lipgloss.JoinVertical(lipgloss.Left, title, m.renderSelectedResult(*selected)))
	}

	if !m.viewportReady {
		content := lipgloss.JoinVertical(lipgloss.Left, title, "", "Initializing...")
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Padding(1, 2).
			Render(content)
	}

	content := lipgloss.JoinVertical(lipgloss.Left, title, m.logViewport.View())

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(1, 2).
		Render(content)
}

func (m *Model) formatLogLine(log LogEntry) string {
	var levelColor lipgloss.Color
	switch log.Level {
	case "INFO":
		levelColor = lipgloss.Color(colorBlue) // Blue
	case "WARN":
		levelColor = lipgloss.Color(colorAmber) // Amber
	case "ERROR":
		levelColor = lipgloss.Color(colorRed) // Red
	case "DEBUG":
		levelColor = lipgloss.Color(colorGray) // Gray
	default:
		levelColor = lipgloss.Color(colorLightGray) // Light gray
	}

	levelStyle := lipgloss.NewStyle().Foreground(levelColor)
	return fmt.Sprintf("[%s] %s", levelStyle.Render(log.Level), log.Message)
}

// initViewport initializes the viewport for scrollable logs
func (m *Model) initViewport() {
	viewportHeight := (m.height - 15) / 2
	if viewportHeight < 5 {
		viewportHeight = 5
	}
	viewportWidth := m.width - 50
	if viewportWidth < 40 {
		viewportWidth = 40
	}

	m.logViewport = viewport.New(viewportWidth, viewportHeight)
	m.logViewport.SetContent("Waiting for logs...")
}
