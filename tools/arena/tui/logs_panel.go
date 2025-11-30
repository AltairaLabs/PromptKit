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

	if showResult && !m.viewportReady {
		m.initViewport()
		m.viewportReady = true
	}

	if showResult {
		m.updateResultViewport(selected)
	} else {
		m.updateLogViewport()
	}

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

func (m *Model) updateResultViewport(run *RunInfo) {
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

	if m.stateStore == nil {
		m.logViewport.SetContent("No state store attached.")
		return
	}

	res, err := m.stateStore.GetResult(context.Background(), run.RunID)
	if err != nil {
		m.logViewport.SetContent(fmt.Sprintf("Failed to load result: %v", err))
		return
	}

	lines := []string{
		fmt.Sprintf("Run: %s", res.RunID),
		fmt.Sprintf("Scenario: %s", res.ScenarioID),
		fmt.Sprintf("Provider: %s", res.ProviderID),
		fmt.Sprintf("Region: %s", res.Region),
		fmt.Sprintf("Status: %s", statusString(run.Status)),
		fmt.Sprintf("Duration: %s", formatDuration(res.Duration)),
		fmt.Sprintf("Cost: $%.4f", res.Cost.TotalCost),
		fmt.Sprintf("Assertions: %d total, %d failed", res.ConversationAssertions.Total, res.ConversationAssertions.Failed),
	}

	for _, r := range res.ConversationAssertions.Results {
		state := "PASS"
		if !r.Passed {
			state = "FAIL"
		}
		line := fmt.Sprintf("[%s] %s", state, r.Message)
		if r.Type != "" {
			line = fmt.Sprintf("[%s] %s - %s", state, r.Type, r.Message)
		}
		lines = append(lines, line)
		for _, v := range r.Violations {
			lines = append(lines, fmt.Sprintf("  • turn %d: %s", v.TurnIndex+1, v.Description))
		}
	}

	if res.Error != "" {
		lines = append(lines, fmt.Sprintf("Error: %s", res.Error))
	}

	m.logViewport.SetContent(strings.Join(lines, "\n"))
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
