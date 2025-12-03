package views

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/theme"
)

const (
	headerProgressBarWidth = 12
)

// HeaderFooterView renders header and footer components
type HeaderFooterView struct {
	width int
}

// NewHeaderFooterView creates a new header/footer view
func NewHeaderFooterView(width int) *HeaderFooterView {
	return &HeaderFooterView{width: width}
}

// RenderHeader renders the top banner with progress
func (v *HeaderFooterView) RenderHeader(
	configFile string,
	completedCount, totalRuns int,
	elapsed time.Duration,
) string {
	// Banner style
	bannerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(theme.ColorPrimary)).
		Align(lipgloss.Center).
		Width(v.width)

	infoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.ColorLightGray)).
		Align(lipgloss.Center).
		Width(v.width)

	progressStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.ColorSuccess)).
		Bold(true)

	timeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.ColorLightBlue))

	tagStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.ColorWarning)).
		Bold(true)

	mockTag := ""
	if strings.Contains(strings.ToLower(filepath.Base(configFile)), "mock") {
		mockTag = tagStyle.Render("MOCK MODE")
	}

	banner := bannerStyle.Render("✨ PromptArena ✨")
	progressBar := buildProgressBar(completedCount, totalRuns, headerProgressBarWidth)
	progress := progressStyle.Render(fmt.Sprintf("[%s %d/%d]", progressBar, completedCount, totalRuns))
	timeStr := timeStyle.Render(fmt.Sprintf("⏱  %s", theme.FormatDuration(elapsed)))

	parts := []string{filepath.Base(configFile), progress, timeStr}
	if mockTag != "" {
		parts = append([]string{mockTag}, parts...)
	}

	infoLine := infoStyle.Render(strings.Join(parts, "  •  "))

	return lipgloss.JoinVertical(lipgloss.Left, banner, infoLine)
}

// RenderFooter renders the bottom help text
func (v *HeaderFooterView) RenderFooter(isConversationPage bool) string {
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorLightGray)).Italic(true)
	items := []string{"q: quit"}

	if isConversationPage {
		items = append(items, "esc: back", "tab: focus turns/detail", "↑/↓: navigate")
	} else {
		items = append(items, "tab: focus runs/logs", "enter: open conversation")
	}

	items = append(items, "enter: select")
	return helpStyle.Render(strings.Join(items, "  •  "))
}

// buildProgressBar creates a text-based progress bar
func buildProgressBar(completed, total, width int) string {
	if total == 0 {
		return strings.Repeat("░", width)
	}

	filledCount := (completed * width) / total
	if filledCount > width {
		filledCount = width
	}

	filled := strings.Repeat("█", filledCount)
	empty := strings.Repeat("░", width-filledCount)
	return filled + empty
}
