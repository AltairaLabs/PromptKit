package tui

import (
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewObserver(t *testing.T) {
	program := &tea.Program{}
	obs := NewObserver(program)

	require.NotNil(t, obs)
	assert.Equal(t, program, obs.program)
}

func TestObserver_OnRunStarted(t *testing.T) {
	// Create a mock program
	program := tea.NewProgram(&Model{}, tea.WithoutRenderer())

	obs := NewObserver(program)

	// Start program in background
	go func() {
		_, _ = program.Run()
	}()

	// Give program time to start
	time.Sleep(50 * time.Millisecond)

	// Send a message via observer
	obs.OnRunStarted("run-1", "scenario-1", "openai", "us-west-1")

	// Stop program
	program.Quit()
}

func TestObserver_OnRunCompleted(t *testing.T) {
	program := tea.NewProgram(&Model{}, tea.WithoutRenderer())
	obs := NewObserver(program)

	go func() {
		_, _ = program.Run()
	}()

	time.Sleep(50 * time.Millisecond)

	obs.OnRunCompleted("run-1", 2*time.Second, 0.05)

	program.Quit()
}

func TestObserver_OnRunFailed(t *testing.T) {
	program := tea.NewProgram(&Model{}, tea.WithoutRenderer())
	obs := NewObserver(program)

	go func() {
		_, _ = program.Run()
	}()

	time.Sleep(50 * time.Millisecond)

	obs.OnRunFailed("run-1", errors.New("test error"))

	program.Quit()
}

func TestObserver_NilProgram(t *testing.T) {
	obs := NewObserver(nil)

	// Should not panic with nil program
	assert.NotPanics(t, func() {
		obs.OnRunStarted("run-1", "scenario-1", "openai", "us-west-1")
		obs.OnRunCompleted("run-1", time.Second, 0.01)
		obs.OnRunFailed("run-1", errors.New("test"))
	})
}

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

func TestModel_Update_RunStartedMsg(t *testing.T) {
	m := NewModel("test.yaml", 10)
	m.isTUIMode = true

	msg := RunStartedMsg{
		RunID:    "run-1",
		Scenario: "test",
		Provider: "openai",
		Region:   "us",
		Time:     time.Now(),
	}

	updatedModel, cmd := m.Update(msg)

	require.NotNil(t, updatedModel)
	assert.Nil(t, cmd)

	updatedM := updatedModel.(*Model)
	assert.Equal(t, 1, len(updatedM.activeRuns))
	assert.Equal(t, "run-1", updatedM.activeRuns[0].RunID)
}

func TestModel_Update_RunCompletedMsg(t *testing.T) {
	m := NewModel("test.yaml", 10)
	m.isTUIMode = true
	m.activeRuns = append(m.activeRuns, RunInfo{
		RunID:     "run-1",
		Status:    StatusRunning,
		StartTime: time.Now(),
	})

	msg := RunCompletedMsg{
		RunID:    "run-1",
		Duration: 3 * time.Second,
		Cost:     0.03,
		Time:     time.Now(),
	}

	updatedModel, cmd := m.Update(msg)

	require.NotNil(t, updatedModel)
	assert.Nil(t, cmd)

	updatedM := updatedModel.(*Model)
	assert.Equal(t, 1, updatedM.completedCount)
	assert.Equal(t, 1, updatedM.successCount)
}

func TestModel_Update_RunFailedMsg(t *testing.T) {
	m := NewModel("test.yaml", 10)
	m.isTUIMode = true
	m.activeRuns = append(m.activeRuns, RunInfo{
		RunID:     "run-1",
		Status:    StatusRunning,
		StartTime: time.Now(),
	})

	msg := RunFailedMsg{
		RunID: "run-1",
		Error: errors.New("test error"),
		Time:  time.Now(),
	}

	updatedModel, cmd := m.Update(msg)

	require.NotNil(t, updatedModel)
	assert.Nil(t, cmd)

	updatedM := updatedModel.(*Model)
	assert.Equal(t, 1, updatedM.completedCount)
	assert.Equal(t, 1, updatedM.failedCount)
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

func TestObserver_Integration(t *testing.T) {
	// Create model with TUI enabled
	m := NewModel("test.yaml", 3)
	m.isTUIMode = true
	m.width = 120
	m.height = 40

	// Create program
	program := tea.NewProgram(m, tea.WithoutRenderer())

	// Create observer
	obs := NewObserver(program)

	// Start program in background
	done := make(chan bool)
	go func() {
		_, _ = program.Run()
		done <- true
	}()

	// Give program time to start
	time.Sleep(50 * time.Millisecond)

	// Simulate run lifecycle
	obs.OnRunStarted("run-1", "scenario-1", "openai", "us")
	time.Sleep(10 * time.Millisecond)

	obs.OnRunStarted("run-2", "scenario-2", "claude", "uk")
	time.Sleep(10 * time.Millisecond)

	obs.OnRunCompleted("run-1", 2*time.Second, 0.05)
	time.Sleep(10 * time.Millisecond)

	obs.OnRunFailed("run-2", errors.New("timeout"))
	time.Sleep(10 * time.Millisecond)

	// Quit program
	program.Quit()

	// Wait for program to finish
	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Program didn't stop in time")
	}
}

// Headless mode tests (for CI mode without TUI)

func TestNewObserverWithModel(t *testing.T) {
	m := NewModel("test.yaml", 10)
	obs := NewObserverWithModel(m)

	require.NotNil(t, obs)
	assert.Nil(t, obs.program)
	assert.Equal(t, m, obs.model)
}

func TestObserver_OnRunStarted_HeadlessMode(t *testing.T) {
	m := NewModel("test.yaml", 10)
	obs := NewObserverWithModel(m)

	// Call observer method
	obs.OnRunStarted("run-1", "scenario-1", "openai", "us-west-1")

	// Verify model was updated
	require.Equal(t, 1, len(m.activeRuns))
	run := m.activeRuns[0]
	assert.Equal(t, "run-1", run.RunID)
	assert.Equal(t, "scenario-1", run.Scenario)
	assert.Equal(t, "openai", run.Provider)
	assert.Equal(t, "us-west-1", run.Region)
	assert.Equal(t, StatusRunning, run.Status)
}

func TestObserver_OnRunCompleted_HeadlessMode(t *testing.T) {
	m := NewModel("test.yaml", 10)
	m.activeRuns = append(m.activeRuns, RunInfo{
		RunID:     "run-1",
		Scenario:  "test",
		Provider:  "openai",
		Status:    StatusRunning,
		StartTime: time.Now(),
	})

	obs := NewObserverWithModel(m)

	// Call observer method
	obs.OnRunCompleted("run-1", 2*time.Second, 0.05)

	// Verify model was updated
	require.Equal(t, 1, len(m.activeRuns))
	run := m.activeRuns[0]
	assert.Equal(t, StatusCompleted, run.Status)
	assert.Equal(t, 2*time.Second, run.Duration)
	assert.Equal(t, 0.05, run.Cost)

	assert.Equal(t, 1, m.completedCount)
	assert.Equal(t, 1, m.successCount)
	assert.Equal(t, 2*time.Second, m.totalDuration)
	assert.Equal(t, 0.05, m.totalCost)
}

func TestObserver_OnRunFailed_HeadlessMode(t *testing.T) {
	m := NewModel("test.yaml", 10)
	m.activeRuns = append(m.activeRuns, RunInfo{
		RunID:     "run-1",
		Scenario:  "test",
		Provider:  "openai",
		Status:    StatusRunning,
		StartTime: time.Now(),
	})

	obs := NewObserverWithModel(m)

	// Call observer method
	testErr := errors.New("connection timeout")
	obs.OnRunFailed("run-1", testErr)

	// Verify model was updated
	require.Equal(t, 1, len(m.activeRuns))
	run := m.activeRuns[0]
	assert.Equal(t, StatusFailed, run.Status)
	assert.Equal(t, "connection timeout", run.Error)

	assert.Equal(t, 1, m.completedCount)
	assert.Equal(t, 1, m.failedCount)
}

func TestObserver_HeadlessMode_MultipleRuns(t *testing.T) {
	m := NewModel("test.yaml", 10)
	obs := NewObserverWithModel(m)

	// Start multiple runs
	obs.OnRunStarted("run-1", "scenario-1", "openai", "us")
	obs.OnRunStarted("run-2", "scenario-2", "claude", "eu")
	obs.OnRunStarted("run-3", "scenario-3", "gemini", "us")

	assert.Equal(t, 3, len(m.activeRuns))

	// Complete some, fail others
	obs.OnRunCompleted("run-1", 1*time.Second, 0.01)
	obs.OnRunCompleted("run-2", 2*time.Second, 0.02)
	obs.OnRunFailed("run-3", errors.New("timeout"))

	// Verify metrics
	assert.Equal(t, 3, m.completedCount)
	assert.Equal(t, 2, m.successCount)
	assert.Equal(t, 1, m.failedCount)
	assert.Equal(t, 3*time.Second, m.totalDuration)
	assert.Equal(t, 0.03, m.totalCost)

	// Verify all runs have final status
	for _, run := range m.activeRuns {
		assert.NotEqual(t, StatusRunning, run.Status)
	}
}

func TestObserver_NilProgramAndModel(t *testing.T) {
	obs := &Observer{
		program: nil,
		model:   nil,
	}

	// Should not panic with both nil
	assert.NotPanics(t, func() {
		obs.OnRunStarted("run-1", "scenario-1", "openai", "us")
		obs.OnRunCompleted("run-1", time.Second, 0.01)
		obs.OnRunFailed("run-1", errors.New("test"))
	})
}
