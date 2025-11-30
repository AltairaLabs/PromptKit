package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return d.Truncate(time.Millisecond * durationPrecisionMs).String()
}

func formatNumber(n int64) string {
	if n < numberSeparator {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%s,%03d", formatNumber(n/numberSeparator), n%numberSeparator)
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func buildProgressBar(done, total int, width int) string {
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
