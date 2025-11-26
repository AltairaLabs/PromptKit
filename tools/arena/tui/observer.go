package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Observer implements the ExecutionObserver interface to bridge engine callbacks to bubbletea messages.
// It converts engine events into bubbletea messages that can be processed by the TUI Model.
type Observer struct {
	program *tea.Program
}

// NewObserver creates a new TUI observer that sends messages to the given bubbletea program.
func NewObserver(program *tea.Program) *Observer {
	return &Observer{
		program: program,
	}
}

// RunStartedMsg is sent when a run begins execution.
type RunStartedMsg struct {
	RunID    string
	Scenario string
	Provider string
	Region   string
	Time     time.Time
}

// RunCompletedMsg is sent when a run completes successfully.
type RunCompletedMsg struct {
	RunID    string
	Duration time.Duration
	Cost     float64
	Time     time.Time
}

// RunFailedMsg is sent when a run fails with an error.
type RunFailedMsg struct {
	RunID string
	Error error
	Time  time.Time
}

// OnRunStarted is called when a test run begins execution.
// This method is goroutine-safe and converts the callback to a bubbletea message.
func (o *Observer) OnRunStarted(runID, scenario, provider, region string) {
	if o.program != nil {
		o.program.Send(RunStartedMsg{
			RunID:    runID,
			Scenario: scenario,
			Provider: provider,
			Region:   region,
			Time:     time.Now(),
		})
	}
}

// OnRunCompleted is called when a test run finishes successfully.
// This method is goroutine-safe and converts the callback to a bubbletea message.
func (o *Observer) OnRunCompleted(runID string, duration time.Duration, cost float64) {
	if o.program != nil {
		o.program.Send(RunCompletedMsg{
			RunID:    runID,
			Duration: duration,
			Cost:     cost,
			Time:     time.Now(),
		})
	}
}

// OnRunFailed is called when a test run fails with an error.
// This method is goroutine-safe and converts the callback to a bubbletea message.
func (o *Observer) OnRunFailed(runID string, err error) {
	if o.program != nil {
		o.program.Send(RunFailedMsg{
			RunID: runID,
			Error: err,
			Time:  time.Now(),
		})
	}
}
