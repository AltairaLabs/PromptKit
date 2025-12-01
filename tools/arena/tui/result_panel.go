package tui

import (
	"context"
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

const (
	resultPaddingVertical   = 1
	resultPaddingHorizontal = 2
)

// selectedRun returns the first selected run, if any.
func (m *Model) selectedRun() *RunInfo {
	for i := range m.activeRuns {
		if m.activeRuns[i].Selected {
			return &m.activeRuns[i]
		}
	}
	return nil
}

// renderSelectedResult renders a result summary for a selected run from the state store.
func (m *Model) renderSelectedResult(run *RunInfo) string {
	if m.stateStore == nil {
		return "No state store attached."
	}
	res, err := m.stateStore.GetResult(context.Background(), run.RunID)
	if err != nil {
		return fmt.Sprintf("Failed to load result: %v", err)
	}

	lines := []string{
		fmt.Sprintf("Run: %s", res.RunID),
		fmt.Sprintf("Scenario: %s", res.ScenarioID),
		fmt.Sprintf("Provider: %s", res.ProviderID),
		fmt.Sprintf("Region: %s", res.Region),
		fmt.Sprintf("Status: %s", statusString(run.Status)),
		fmt.Sprintf("Duration: %s", formatDuration(res.Duration)),
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
		BorderForeground(lipgloss.Color(colorGray)).
		Padding(resultPaddingVertical, resultPaddingHorizontal).
		Render(content)
}

func statusString(status RunStatus) string {
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
