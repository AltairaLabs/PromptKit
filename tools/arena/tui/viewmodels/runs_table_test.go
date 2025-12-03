package viewmodels

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewRunsTableViewModel(t *testing.T) {
	runs := []RunData{
		{Status: StatusRunning, Provider: "openai", Scenario: "test"},
	}

	vm := NewRunsTableViewModel(runs)

	assert.NotNil(t, vm)
	assert.Equal(t, 1, vm.GetRowCount())
}

func TestGetRows_EmptyRuns(t *testing.T) {
	vm := NewRunsTableViewModel([]RunData{})

	rows := vm.GetRows()

	assert.Empty(t, rows)
	assert.Equal(t, 0, vm.GetRowCount())
}

func TestGetRows_RunningStatus(t *testing.T) {
	startTime := time.Now().Add(-5 * time.Second)
	runs := []RunData{
		{
			Status:           StatusRunning,
			Provider:         "openai",
			Scenario:         "test-scenario",
			Region:           "us-east-1",
			StartTime:        startTime,
			CurrentTurnIndex: 2,
			CurrentTurnRole:  "assistant",
		},
	}

	vm := NewRunsTableViewModel(runs)
	rows := vm.GetRows()

	assert.Len(t, rows, 1)
	assert.Equal(t, "● Running", rows[0][0])
	assert.Equal(t, "openai", rows[0][1])
	assert.Equal(t, "test-scenario", rows[0][2])
	assert.Equal(t, "us-east-1", rows[0][3])
	assert.Contains(t, rows[0][4], "s") // duration should contain seconds
	assert.Equal(t, "-", rows[0][5])    // cost should be "-" for running
	assert.Contains(t, rows[0][6], "turn 3: assistant")
}

func TestGetRows_CompletedStatus(t *testing.T) {
	runs := []RunData{
		{
			Status:   StatusCompleted,
			Provider: "anthropic",
			Scenario: "completed-test",
			Region:   "eu-west-1",
			Duration: 2 * time.Second,
			Cost:     0.0123,
		},
	}

	vm := NewRunsTableViewModel(runs)
	rows := vm.GetRows()

	assert.Len(t, rows, 1)
	assert.Equal(t, "✓ Done", rows[0][0])
	assert.Equal(t, "anthropic", rows[0][1])
	assert.Equal(t, "completed-test", rows[0][2])
	assert.Equal(t, "eu-west-1", rows[0][3])
	assert.Contains(t, rows[0][4], "2s")
	assert.Equal(t, "$0.0123", rows[0][5])
}

func TestGetRows_FailedStatus(t *testing.T) {
	runs := []RunData{
		{
			Status:   StatusFailed,
			Provider: "google",
			Scenario: "failed-test",
			Region:   "asia-1",
			Error:    "This is a very long error message that should be truncated to fit in the notes column properly",
		},
	}

	vm := NewRunsTableViewModel(runs)
	rows := vm.GetRows()

	assert.Len(t, rows, 1)
	assert.Equal(t, "✗ Failed", rows[0][0])
	assert.Equal(t, "google", rows[0][1])
	assert.Equal(t, "failed-test", rows[0][2])
	assert.Equal(t, "asia-1", rows[0][3])
	assert.Equal(t, "-", rows[0][4])
	assert.Equal(t, "-", rows[0][5])
	assert.LessOrEqual(t, len(rows[0][6]), errorNoteMaxLen)
	assert.Contains(t, rows[0][6], "...") // should be truncated
}

func TestGetRows_SelectedRun(t *testing.T) {
	runs := []RunData{
		{
			Status:   StatusCompleted,
			Provider: "openai",
			Scenario: "selected-test",
			Region:   "us-west-2",
			Duration: 1 * time.Second,
			Cost:     0.0050,
			Selected: true,
		},
	}

	vm := NewRunsTableViewModel(runs)
	rows := vm.GetRows()

	assert.Len(t, rows, 1)
	assert.Contains(t, rows[0][0], "*") // selected indicator
	assert.Equal(t, "✓ Done *", rows[0][0])
}

func TestGetRows_MultipleRuns(t *testing.T) {
	runs := []RunData{
		{
			Status:    StatusRunning,
			Provider:  "openai",
			Scenario:  "run1",
			Region:    "us-east-1",
			StartTime: time.Now().Add(-2 * time.Second),
		},
		{
			Status:   StatusCompleted,
			Provider: "anthropic",
			Scenario: "run2",
			Region:   "eu-west-1",
			Duration: 3 * time.Second,
			Cost:     0.0100,
		},
		{
			Status:   StatusFailed,
			Provider: "google",
			Scenario: "run3",
			Region:   "asia-1",
			Error:    "Connection timeout",
		},
	}

	vm := NewRunsTableViewModel(runs)
	rows := vm.GetRows()

	assert.Len(t, rows, 3)
	assert.Equal(t, "● Running", rows[0][0])
	assert.Equal(t, "✓ Done", rows[1][0])
	assert.Equal(t, "✗ Failed", rows[2][0])
}

func TestGetRows_RunningWithoutTurn(t *testing.T) {
	runs := []RunData{
		{
			Status:    StatusRunning,
			Provider:  "openai",
			Scenario:  "no-turn",
			Region:    "us-east-1",
			StartTime: time.Now(),
		},
	}

	vm := NewRunsTableViewModel(runs)
	rows := vm.GetRows()

	assert.Len(t, rows, 1)
	assert.Equal(t, "● Running", rows[0][0])
	assert.Empty(t, rows[0][6]) // notes should be empty when no turn info
}

func TestGetRowCount(t *testing.T) {
	testCases := []struct {
		name     string
		runs     []RunData
		expected int
	}{
		{
			name:     "empty",
			runs:     []RunData{},
			expected: 0,
		},
		{
			name: "single run",
			runs: []RunData{
				{Status: StatusRunning, Provider: "openai"},
			},
			expected: 1,
		},
		{
			name: "multiple runs",
			runs: []RunData{
				{Status: StatusRunning, Provider: "openai"},
				{Status: StatusCompleted, Provider: "anthropic"},
				{Status: StatusFailed, Provider: "google"},
			},
			expected: 3,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			vm := NewRunsTableViewModel(tc.runs)
			assert.Equal(t, tc.expected, vm.GetRowCount())
		})
	}
}
