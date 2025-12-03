package tui

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/views"
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
	ctx := m.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	res, err := m.stateStore.GetResult(ctx, run.RunID)
	if err != nil {
		return fmt.Sprintf("Failed to load result: %v", err)
	}

	// Use new ResultView
	resultView := views.NewResultView()
	viewStatus := convertRunStatusToViewStatus(run.Status)
	return resultView.Render(res, viewStatus)
}

// convertRunStatusToViewStatus converts TUI RunStatus to views.RunStatus
func convertRunStatusToViewStatus(status RunStatus) views.RunStatus {
	switch status {
	case StatusRunning:
		return views.StatusRunning
	case StatusCompleted:
		return views.StatusCompleted
	case StatusFailed:
		return views.StatusFailed
	default:
		return views.StatusRunning
	}
}
