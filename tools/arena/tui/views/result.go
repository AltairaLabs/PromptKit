package views

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/theme"
)

const (
	resultPaddingVertical   = 1
	resultPaddingHorizontal = 2
)

// RunStatus represents the status of a run
type RunStatus int

// Run status constants
const (
	// StatusRunning indicates a run is currently executing
	StatusRunning RunStatus = iota
	// StatusCompleted indicates a run finished successfully
	StatusCompleted
	// StatusFailed indicates a run encountered an error
	StatusFailed
)

// ResultView renders a detailed result for a completed run
type ResultView struct{}

// NewResultView creates a new result view
func NewResultView() *ResultView {
	return &ResultView{}
}

// Render renders the result details
func (v *ResultView) Render(res *statestore.RunResult, status RunStatus) string {
	lines := []string{
		fmt.Sprintf("Run: %s", res.RunID),
		fmt.Sprintf("Scenario: %s", res.ScenarioID),
		fmt.Sprintf("Provider: %s", res.ProviderID),
		fmt.Sprintf("Region: %s", res.Region),
		fmt.Sprintf("Status: %s", formatStatus(status)),
		fmt.Sprintf("Duration: %s", theme.FormatDuration(res.Duration)),
		fmt.Sprintf("Cost: $%.4f", res.Cost.TotalCost),
	}

	if res.ConversationAssertions.Total > 0 {
		assertLine := fmt.Sprintf(
			"Assertions: %d total, %d failed",
			res.ConversationAssertions.Total,
			res.ConversationAssertions.Failed,
		)
		lines = append(lines, assertLine)
	}

	if res.Error != "" {
		lines = append(lines, fmt.Sprintf("Error: %s", res.Error))
	}

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.BorderColorUnfocused()).
		Padding(resultPaddingVertical, resultPaddingHorizontal).
		Render(content)
}

func formatStatus(status RunStatus) string {
	switch status {
	case StatusRunning:
		return "running"
	case StatusCompleted:
		return "completed"
	case StatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}
