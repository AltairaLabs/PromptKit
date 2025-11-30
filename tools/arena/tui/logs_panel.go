package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

const (
	logsHeightDivisor     = 3
	logsMinHeight         = 5
	logsWidthPadding      = 50
	logsMinWidth          = 40
	logsViewportOffset    = 15
	logsViewportDivisor   = 2
	logsPaddingVertical   = 1
	logsPaddingHorizontal = 2
)

func (m *Model) renderLogs() string {
	selected := m.selectedRun()
	showResult := selected != nil &&
		(selected.Status == StatusCompleted || selected.Status == StatusFailed) &&
		m.stateStore != nil

	if showResult {
		res, err := m.stateStore.GetResult(context.Background(), selected.RunID)
		if err != nil {
			return fmt.Sprintf("Failed to load result: %v", err)
		}
		m.convPane.SetDimensions(m.width, m.height)
		m.convPane.SetData(selected.RunID, res)
		return m.convPane.View(res)
	}

	m.updateLogViewport()

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorSky))
	title := titleStyle.Render("📝 Logs (↑/↓ to scroll, 's' summary)")

	borderColor := lipgloss.Color(colorLightBlue)
	if m.activePane != paneLogs {
		borderColor = lipgloss.Color(colorGray)
	}

	if !m.viewportReady {
		content := lipgloss.JoinVertical(lipgloss.Left, title, "", "Initializing...")
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Padding(logsPaddingVertical, logsPaddingHorizontal).
			Render(content)
	}

	content := lipgloss.JoinVertical(lipgloss.Left, title, m.logViewport.View())

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(logsPaddingVertical, logsPaddingHorizontal).
		Render(content)
}

func (m *Model) updateLogViewport() {
	// Update viewport dimensions
	if !m.viewportReady {
		return
	}

	viewportHeight := m.height / logsHeightDivisor // Leave room for header + active runs
	if viewportHeight < logsMinHeight {
		viewportHeight = logsMinHeight
	}
	viewportWidth := m.width - logsWidthPadding // Leave room for metrics (40) + padding
	if viewportWidth < logsMinWidth {
		viewportWidth = logsMinWidth
	}

	m.logViewport.Width = viewportWidth
	m.logViewport.Height = viewportHeight

	switch {
	case m.viewMode == viewSummary && m.summary != nil:
		m.logViewport.SetContent("Summary view (press 'l' to view logs)")
	case len(m.logs) == 0:
		m.logViewport.SetContent("No logs yet...")
	default:
		logLines := make([]string, len(m.logs))
		for i, log := range m.logs {
			logLines[i] = m.formatLogLine(log)
		}
		m.logViewport.SetContent(strings.Join(logLines, "\n"))
	}
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
	viewportHeight := (m.height - logsViewportOffset) / logsViewportDivisor
	if viewportHeight < logsMinHeight {
		viewportHeight = logsMinHeight
	}
	viewportWidth := m.width - logsWidthPadding
	if viewportWidth < logsMinWidth {
		viewportWidth = logsMinWidth
	}

	m.logViewport = viewport.New(viewportWidth, viewportHeight)
	m.logViewport.SetContent("Waiting for logs...")
}
