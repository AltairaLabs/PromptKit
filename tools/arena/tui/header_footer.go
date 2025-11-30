package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const (
	headerProgressBarWidth = 12
)

func (m *Model) renderHeader(elapsed time.Duration) string {
	// Banner style
	bannerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(colorPurple)).
		Align(lipgloss.Center).
		Width(m.width)

	infoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorLightGray)).
		Align(lipgloss.Center).
		Width(m.width)

	progressStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorGreen)).
		Bold(true)

	timeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorLightBlue))

	tagStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorAmber)).
		Bold(true)

	mockTag := ""
	if strings.Contains(strings.ToLower(filepath.Base(m.configFile)), "mock") {
		mockTag = tagStyle.Render("MOCK MODE")
	}

	banner := bannerStyle.Render("✨ PromptArena ✨")
	progressBar := buildProgressBar(m.completedCount, m.totalRuns, headerProgressBarWidth)
	progress := progressStyle.Render(fmt.Sprintf("[%s %d/%d]", progressBar, m.completedCount, m.totalRuns))
	timeStr := timeStyle.Render(fmt.Sprintf("⏱  %s", formatDuration(elapsed)))

	parts := []string{filepath.Base(m.configFile), progress, timeStr}
	if mockTag != "" {
		parts = append([]string{mockTag}, parts...)
	}

	infoLine := infoStyle.Render(strings.Join(parts, "  •  "))

	return lipgloss.JoinVertical(lipgloss.Left, banner, infoLine)
}

func (m *Model) renderFooter() string {
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorLightGray)).Italic(true)
	items := []string{"q: quit"}

	if m.currentPage == pageConversation {
		items = append(items, "esc: back", "tab: focus turns/detail", "↑/↓: navigate")
	} else {
		items = append(items, "tab: focus runs/logs", "enter: open conversation")
	}

	items = append(items, "enter: select")
	return helpStyle.Render(strings.Join(items, "  •  "))
}
