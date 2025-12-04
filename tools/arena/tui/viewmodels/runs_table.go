// Package viewmodels provides data transformation and presentation logic for TUI views.
package viewmodels

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/table"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/theme"
)

// RunStatus represents the status of a run
type RunStatus int

const (
	// StatusRunning indicates the run is currently executing
	StatusRunning RunStatus = iota
	// StatusCompleted indicates the run finished successfully
	StatusCompleted
	// StatusFailed indicates the run failed
	StatusFailed
)

const (
	errorNoteMaxLen = 40
)

// RunData contains the raw data for a single run
type RunData struct {
	Status           RunStatus
	Provider         string
	Scenario         string
	Region           string
	StartTime        time.Time
	Duration         time.Duration
	Cost             float64
	Error            string
	CurrentTurnIndex int
	CurrentTurnRole  string
	Selected         bool
}

// RunsTableViewModel transforms run data into table rows
type RunsTableViewModel struct {
	runs []RunData
}

// NewRunsTableViewModel creates a new RunsTableViewModel
func NewRunsTableViewModel(runs []RunData) *RunsTableViewModel {
	return &RunsTableViewModel{
		runs: runs,
	}
}

// GetRows returns formatted table rows for the runs
func (vm *RunsTableViewModel) GetRows() []table.Row {
	rows := make([]table.Row, 0, len(vm.runs))

	for i := range vm.runs {
		row := vm.formatRun(&vm.runs[i])
		rows = append(rows, row)
	}

	return rows
}

// GetRowCount returns the number of runs
func (vm *RunsTableViewModel) GetRowCount() int {
	return len(vm.runs)
}

// formatRun transforms a RunData into a table row
func (vm *RunsTableViewModel) formatRun(run *RunData) table.Row {
	var status, duration, cost, notes string

	switch run.Status {
	case StatusRunning:
		status = "● Running"
		elapsed := time.Since(run.StartTime)
		duration = theme.FormatDuration(elapsed)
		cost = "-"
		if run.CurrentTurnRole != "" {
			notes = fmt.Sprintf("turn %d: %s", run.CurrentTurnIndex+1, run.CurrentTurnRole)
		}
	case StatusCompleted:
		status = "✓ Done"
		duration = theme.FormatDuration(run.Duration)
		cost = theme.FormatCost(run.Cost)
	case StatusFailed:
		status = "✗ Failed"
		duration = "-"
		cost = "-"
		notes = theme.TruncateString(run.Error, errorNoteMaxLen)
	}

	if run.Selected {
		status = fmt.Sprintf("%s *", status)
	}

	return table.Row{
		status,
		run.Provider,
		run.Scenario,
		run.Region,
		duration,
		cost,
		notes,
	}
}
