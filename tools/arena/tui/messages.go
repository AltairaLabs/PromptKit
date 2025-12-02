package tui

import "time"

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

// ShowSummaryMsg is sent when execution completes and the final summary should be displayed.
type ShowSummaryMsg struct {
	Summary *Summary
}

// TurnStartedMsg is sent when a turn starts.
type TurnStartedMsg struct {
	RunID     string
	TurnIndex int
	Role      string
	Scenario  string
	Time      time.Time
}

// TurnCompletedMsg is sent when a turn completes.
type TurnCompletedMsg struct {
	RunID     string
	TurnIndex int
	Role      string
	Scenario  string
	Error     error
	Time      time.Time
}
