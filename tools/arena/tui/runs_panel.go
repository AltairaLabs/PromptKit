package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
)

func (m *Model) renderActiveRuns() string {
	// Initialize table on first render
	if !m.tableReady {
		m.initRunsTable(15) // Default reasonable height
	}

	// Update table rows with current active runs
	m.updateRunsTable()

	// Set table dimensions
	tableHeight := m.height / 3 // Keep headroom for header + logs
	if tableHeight < 5 {
		tableHeight = 5
	}
	m.runsTable.SetHeight(tableHeight)
	m.runsTable.SetWidth(m.width - 8)
	if m.activePane == paneRuns {
		m.runsTable.Focus()
	} else {
		m.runsTable.Blur()
	}

	borderColor := lipgloss.Color(colorIndigo)
	if m.activePane == paneRuns {
		borderColor = lipgloss.Color(colorWhite)
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(colorViolet))

	title := titleStyle.Render(fmt.Sprintf("📊 Active Runs (%d concurrent workers)", len(m.activeRuns)))

	content := lipgloss.JoinVertical(lipgloss.Left, title, m.runsTable.View())

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(1, 2).
		Width(m.width - 4).
		Render(content)
}

// initRunsTable initializes the table for active runs
func (m *Model) initRunsTable(height int) {
	columns := []table.Column{
		{Title: "Status", Width: 10},
		{Title: "Provider", Width: 20},
		{Title: "Scenario", Width: 30},
		{Title: "Region", Width: 12},
		{Title: "Duration", Width: 12},
		{Title: "Cost", Width: 10},
		{Title: "Notes", Width: 24},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows([]table.Row{}),
		table.WithFocused(false),
		table.WithHeight(height),
	)

	// Style the table
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(colorIndigo)).
		BorderBottom(true).
		Bold(true).
		Foreground(lipgloss.Color(colorViolet))
	s.Selected = s.Selected.
		Foreground(lipgloss.Color(colorWhite)).
		Background(lipgloss.Color(colorIndigo)).
		Bold(false)

	t.SetStyles(s)
	m.runsTable = t
	m.tableReady = true
}

// updateRunsTable updates the table rows with current active runs
func (m *Model) updateRunsTable() {
	rows := make([]table.Row, 0, len(m.activeRuns))

	for i := range m.activeRuns {
		run := &m.activeRuns[i]

		var status, duration, cost, notes string
		switch run.Status {
		case StatusRunning:
			status = "● Running"
			elapsed := time.Since(run.StartTime).Truncate(time.Millisecond * durationPrecisionMs)
			duration = formatDuration(elapsed)
			cost = "-"
			if run.CurrentTurnRole != "" {
				notes = fmt.Sprintf("turn %d: %s", run.CurrentTurnIndex+1, run.CurrentTurnRole)
			}
		case StatusCompleted:
			status = "✓ Done"
			duration = formatDuration(run.Duration)
			cost = fmt.Sprintf("$%.4f", run.Cost)
		case StatusFailed:
			status = "✗ Failed"
			duration = "-"
			cost = "-"
			notes = truncateString(run.Error, 40)
		}

		if run.Selected {
			status = fmt.Sprintf("%s *", status)
		}

		rows = append(rows, table.Row{
			status,
			run.Provider,
			run.Scenario,
			run.Region,
			duration,
			cost,
			notes,
		})
	}

	m.runsTable.SetRows(rows)
}

func (m *Model) formatRunLine(run *RunInfo) string {
	var status string
	var statusColor lipgloss.Color

	switch run.Status {
	case StatusRunning:
		status = "●"
		statusColor = lipgloss.Color(colorBlue) // Blue for running
	case StatusCompleted:
		status = "✓"
		statusColor = lipgloss.Color(colorGreen) // Green for success
	case StatusFailed:
		status = "✗"
		statusColor = lipgloss.Color(colorRed) // Red for failure
	}

	statusStyle := lipgloss.NewStyle().Foreground(statusColor)
	runInfo := fmt.Sprintf("%s/%s/%s", run.Provider, run.Scenario, run.Region)

	switch run.Status {
	case StatusRunning:
		elapsed := time.Since(run.StartTime).Truncate(time.Millisecond * durationPrecisionMs)
		return fmt.Sprintf("[%s] %-40s ⏱ %s", statusStyle.Render(status), runInfo, formatDuration(elapsed))
	case StatusFailed:
		return fmt.Sprintf("[%s] %-40s ERROR", statusStyle.Render(status), runInfo)
	case StatusCompleted:
		return fmt.Sprintf(
			"[%s] %-40s ⏱ %s  $%.4f",
			statusStyle.Render(status),
			runInfo,
			formatDuration(run.Duration),
			run.Cost,
		)
	}
	return ""
}
