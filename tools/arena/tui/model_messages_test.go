package tui

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModel_handleRunStarted(t *testing.T) {
	m := NewModel("test.yaml", 10)
	m.isTUIMode = true

	msg := &RunStartedMsg{
		RunID:    "run-1",
		Scenario: "test-scenario",
		Provider: "openai",
		Region:   "us-west-1",
		Time:     time.Now(),
	}

	m.handleRunStarted(msg)

	require.Equal(t, 1, len(m.activeRuns))
	run := m.activeRuns[0]
	assert.Equal(t, "run-1", run.RunID)
	assert.Equal(t, "test-scenario", run.Scenario)
	assert.Equal(t, "openai", run.Provider)
	assert.Equal(t, "us-west-1", run.Region)
	assert.Equal(t, StatusRunning, run.Status)

	require.Equal(t, 1, len(m.logs))
	assert.Contains(t, m.logs[0].Message, "Started")
	assert.Contains(t, m.logs[0].Message, "openai")
}

func TestModel_handleRunCompleted(t *testing.T) {
	m := NewModel("test.yaml", 10)
	m.isTUIMode = true

	// Add a running run
	startTime := time.Now()
	m.activeRuns = append(m.activeRuns, RunInfo{
		RunID:     "run-1",
		Scenario:  "test",
		Provider:  "openai",
		Status:    StatusRunning,
		StartTime: startTime,
	})

	msg := &RunCompletedMsg{
		RunID:    "run-1",
		Duration: 2 * time.Second,
		Cost:     0.05,
		Time:     time.Now(),
	}

	m.handleRunCompleted(msg)

	require.Equal(t, 1, len(m.activeRuns))
	run := m.activeRuns[0]
	assert.Equal(t, StatusCompleted, run.Status)
	assert.Equal(t, 2*time.Second, run.Duration)
	assert.Equal(t, 0.05, run.Cost)

	assert.Equal(t, 1, m.completedCount)
	assert.Equal(t, 1, m.successCount)
	assert.Equal(t, 2*time.Second, m.totalDuration)
	assert.Equal(t, 0.05, m.totalCost)

	require.Greater(t, len(m.logs), 0)
	assert.Contains(t, m.logs[len(m.logs)-1].Message, "Completed")
}

func TestModel_handleRunFailed(t *testing.T) {
	m := NewModel("test.yaml", 10)
	m.isTUIMode = true

	// Add a running run
	m.activeRuns = append(m.activeRuns, RunInfo{
		RunID:     "run-1",
		Scenario:  "test",
		Provider:  "openai",
		Status:    StatusRunning,
		StartTime: time.Now(),
	})

	msg := &RunFailedMsg{
		RunID: "run-1",
		Error: errors.New("connection timeout"),
		Time:  time.Now(),
	}

	m.handleRunFailed(msg)

	require.Equal(t, 1, len(m.activeRuns))
	run := m.activeRuns[0]
	assert.Equal(t, StatusFailed, run.Status)
	assert.Equal(t, "connection timeout", run.Error)

	assert.Equal(t, 1, m.completedCount)
	assert.Equal(t, 1, m.failedCount)

	require.Greater(t, len(m.logs), 0)
	assert.Contains(t, m.logs[len(m.logs)-1].Message, "Failed")
	assert.Equal(t, "ERROR", m.logs[len(m.logs)-1].Level)
}

func TestModel_Update_RunMessages(t *testing.T) {
	m := NewModel("test.yaml", 10)
	m.isTUIMode = true

	start := RunStartedMsg{
		RunID:    "run-1",
		Scenario: "test",
		Provider: "openai",
		Region:   "us",
		Time:     time.Now(),
	}

	updatedModel, cmd := m.Update(start)
	require.Nil(t, cmd)
	updated := updatedModel.(*Model)
	require.Len(t, updated.activeRuns, 1)

	complete := RunCompletedMsg{
		RunID:    "run-1",
		Duration: 3 * time.Second,
		Cost:     0.03,
		Time:     time.Now(),
	}
	updatedModel, cmd = updated.Update(complete)
	require.Nil(t, cmd)
	updated = updatedModel.(*Model)
	assert.Equal(t, 1, updated.completedCount)
	assert.Equal(t, 1, updated.successCount)

	fail := RunFailedMsg{
		RunID: "run-1",
		Error: errors.New("boom"),
		Time:  time.Now(),
	}
	updatedModel, cmd = updated.Update(fail)
	require.Nil(t, cmd)
	updated = updatedModel.(*Model)
	assert.Equal(t, 2, updated.completedCount) // completed + failed increments
	assert.Equal(t, 1, updated.failedCount)
}

func TestModel_trimLogs(t *testing.T) {
	m := NewModel("test.yaml", 10)

	// Add more logs than buffer size
	for i := 0; i < maxLogBufferSize+20; i++ {
		m.logs = append(m.logs, LogEntry{
			Timestamp: time.Now(),
			Message:   "test",
		})
	}

	m.trimLogs()

	assert.Equal(t, maxLogBufferSize, len(m.logs))
}

func TestModel_handleRunCompleted_NonExistentRun(t *testing.T) {
	m := NewModel("test.yaml", 10)

	msg := &RunCompletedMsg{
		RunID:    "non-existent",
		Duration: time.Second,
		Cost:     0.01,
		Time:     time.Now(),
	}

	// Should not panic
	assert.NotPanics(t, func() {
		m.handleRunCompleted(msg)
	})

	// Metrics should still be updated
	assert.Equal(t, 1, m.completedCount)
	assert.Equal(t, 1, m.successCount)
}

func TestModel_handleRunFailed_NonExistentRun(t *testing.T) {
	m := NewModel("test.yaml", 10)

	msg := &RunFailedMsg{
		RunID: "non-existent",
		Error: errors.New("test"),
		Time:  time.Now(),
	}

	// Should not panic
	assert.NotPanics(t, func() {
		m.handleRunFailed(msg)
	})

	// Metrics should still be updated
	assert.Equal(t, 1, m.completedCount)
	assert.Equal(t, 1, m.failedCount)
}
