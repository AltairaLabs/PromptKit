package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func buildProgressBar(done, total, width int) string {
	if total == 0 || width <= 0 {
		return strings.Repeat(".", width)
	}
	filled := int(float64(done) / float64(total) * float64(width))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	return fmt.Sprintf("%s%s", strings.Repeat("█", filled), strings.Repeat("·", width-filled))
}

func tick() tea.Cmd {
	return tea.Tick(time.Millisecond*tickIntervalMs, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}
