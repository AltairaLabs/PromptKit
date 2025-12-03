package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/theme"
)

const (
	logsPaddingVertical   = 1
	logsPaddingHorizontal = 1
)

// LogEntry represents a single log line
type LogEntry struct {
	Level   string
	Message string
}

// LogsView renders the logs panel with viewport
type LogsView struct {
	focused bool
}

// NewLogsView creates a new logs view
func NewLogsView(focused bool) *LogsView {
	return &LogsView{
		focused: focused,
	}
}

// Render renders the logs panel
func (v *LogsView) Render(vp *viewport.Model, ready bool, width int) string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(theme.ColorSky))
	title := titleStyle.Render("📝 Logs (↑/↓ to scroll, 's' summary)")

	borderColor := lipgloss.Color(theme.ColorLightBlue)
	if !v.focused {
		borderColor = theme.BorderColorUnfocused()
	}

	var content string
	if !ready {
		content = lipgloss.JoinVertical(lipgloss.Left, title, "", "Initializing...")
	} else {
		content = lipgloss.JoinVertical(lipgloss.Left, title, vp.View())
	}

	// Account for chrome: horizontal padding (2 * logsPaddingHorizontal) + 1 for border adjustment
	chromeWidth := (2 * logsPaddingHorizontal) + 1
	innerWidth := width - chromeWidth
	if innerWidth < 0 {
		innerWidth = 0
	}

	return lipgloss.NewStyle().
		Width(innerWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(logsPaddingVertical, logsPaddingHorizontal).
		Render(content)
}

// FormatLogLine formats a single log entry with appropriate styling
func FormatLogLine(level, message string) string {
	var levelColor lipgloss.Color
	switch level {
	case "INFO":
		levelColor = lipgloss.Color(theme.ColorInfo) // Blue
	case "WARN":
		levelColor = lipgloss.Color(theme.ColorWarning) // Amber
	case "ERROR":
		levelColor = lipgloss.Color(theme.ColorError) // Red
	case "DEBUG":
		levelColor = lipgloss.Color(theme.ColorGray) // Gray
	default:
		levelColor = lipgloss.Color(theme.ColorLightGray) // Light gray
	}

	levelStyle := lipgloss.NewStyle().Foreground(levelColor)
	return fmt.Sprintf("[%s] %s", levelStyle.Render(level), message)
}

// FormatLogLines formats multiple log entries
func FormatLogLines(logs []LogEntry) string {
	if len(logs) == 0 {
		return "No logs yet..."
	}

	logLines := make([]string, len(logs))
	for i, log := range logs {
		logLines[i] = FormatLogLine(log.Level, log.Message)
	}
	return strings.Join(logLines, "\n")
}
