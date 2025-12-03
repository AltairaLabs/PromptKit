package tui

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/viewport"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/views"
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

	ctx := m.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	if showResult {
		res, err := m.stateStore.GetResult(ctx, selected.RunID)
		if err != nil {
			return fmt.Sprintf("Failed to load result: %v", err)
		}
		m.convPane.SetDimensions(m.width, m.height)
		m.convPane.SetData(selected, res)
		return m.convPane.View(res)
	}

	m.updateLogViewport()

	// Use new LogsView for rendering
	logsView := views.NewLogsView(m.activePane == paneLogs)
	return logsView.Render(&m.logViewport, m.viewportReady)
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

	// Convert logs to format expected by views.FormatLogLines
	viewLogs := make([]views.LogEntry, len(m.logs))
	for i := range m.logs {
		viewLogs[i] = views.LogEntry{
			Level:   m.logs[i].Level,
			Message: m.logs[i].Message,
		}
	}
	m.logViewport.SetContent(views.FormatLogLines(viewLogs))
}

// formatLogLine delegates to views.FormatLogLine for consistency
func (m *Model) formatLogLine(log LogEntry) string {
	return views.FormatLogLine(log.Level, log.Message)
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
