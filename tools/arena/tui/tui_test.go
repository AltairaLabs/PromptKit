package tui

import (
	"context"
	"fmt"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/logging"
)

func TestNewModel(t *testing.T) {
	m := NewModel("test.yaml", 5)

	require.NotNil(t, m)
	assert.Equal(t, "test.yaml", m.configFile)
	assert.Equal(t, 5, m.totalRuns)
	assert.Equal(t, 0, m.completedCount)
	assert.Equal(t, 0, m.successCount)
	assert.Equal(t, 0, m.failedCount)
	assert.Equal(t, 0, len(m.activeRuns))
	assert.Equal(t, 0, len(m.logs))
	assert.False(t, m.startTime.IsZero())
}

func TestModel_Init(t *testing.T) {
	m := NewModel("test.yaml", 5)
	cmd := m.Init()

	require.NotNil(t, cmd)
	msg := cmd()
	_, ok := msg.(tickMsg)
	assert.True(t, ok, "Init should return a tick command")
}

func TestModel_Update_TickMsg(t *testing.T) {
	m := NewModel("test.yaml", 5)

	m.activeRuns = append(m.activeRuns, RunInfo{
		RunID:     "run-1",
		Scenario:  "test",
		Status:    StatusRunning,
		StartTime: time.Now(),
	})

	updatedModel, cmd := m.Update(tickMsg{})

	require.NotNil(t, updatedModel)
	require.NotNil(t, cmd, "TickMsg should return another tick command")
	updatedM := updatedModel.(*Model)
	assert.Equal(t, 1, len(updatedM.activeRuns))
}

func TestModel_Update_ResizeMsg(t *testing.T) {
	m := NewModel("test.yaml", 5)

	msg := tea.WindowSizeMsg{
		Width:  120,
		Height: 40,
	}

	updatedModel, cmd := m.Update(msg)

	require.NotNil(t, updatedModel)
	assert.Nil(t, cmd, "Resize should not return a command")
	updatedM := updatedModel.(*Model)
	assert.Equal(t, 120, updatedM.width)
	assert.Equal(t, 40, updatedM.height)
}

func TestModel_Update_KeyMsg(t *testing.T) {
	m := NewModel("test.yaml", 5)

	msg := tea.KeyMsg{
		Type: tea.KeyCtrlC,
	}

	updatedModel, cmd := m.Update(msg)

	require.NotNil(t, updatedModel)
	require.NotNil(t, cmd, "Ctrl+C should return a quit command")
}

func TestModel_View(t *testing.T) {
	m := NewModel("test.yaml", 5)
	m.width = 100
	m.height = 30
	m.isTUIMode = true // Enable TUI mode for testing
	view := m.View()

	assert.NotEmpty(t, view)
	assert.Contains(t, view, "PromptArena")
}

func TestModel_View_WithData(t *testing.T) {
	m := NewModel("test.yaml", 10)
	m.width = 120
	m.height = 40
	m.isTUIMode = true
	m.completedCount = 5
	m.successCount = 4
	m.failedCount = 1

	m.activeRuns = append(m.activeRuns, RunInfo{
		RunID:     "run-1",
		Scenario:  "test-scenario",
		Provider:  "openai",
		Status:    StatusRunning,
		StartTime: time.Now(),
	})

	view := m.View()

	assert.Contains(t, view, "PromptArena")
	assert.Contains(t, view, "5/10")
	// Run details show provider/scenario, not run ID
	assert.Contains(t, view, "openai")
	assert.Contains(t, view, "test-scenario")
}

type mockRunResultStore struct {
	result *statestore.RunResult
	err    error
}

func (m *mockRunResultStore) GetResult(ctx context.Context, runID string) (*statestore.RunResult, error) {
	return m.result, m.err
}

func TestModel_View_SelectedRunShowsResult(t *testing.T) {
	m := NewModel("test.yaml", 1)
	m.width = 120
	m.height = 40
	m.isTUIMode = true
	m.currentPage = pageConversation // Switch to conversation page

	m.activeRuns = []RunInfo{
		{
			RunID:     "run-1",
			Scenario:  "scn",
			Provider:  "prov",
			Status:    StatusCompleted,
			Duration:  2 * time.Second,
			Selected:  true,
			StartTime: time.Now().Add(-2 * time.Second),
		},
	}
	m.stateStore = &mockRunResultStore{
		result: &statestore.RunResult{
			RunID:      "run-1",
			ScenarioID: "scn",
			ProviderID: "prov",
			Region:     "us",
			Duration:   2 * time.Second,
			Messages: []types.Message{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "hi there"},
			},
			Cost: types.CostInfo{
				TotalCost:    1.23,
				InputTokens:  10,
				OutputTokens: 5,
			},
			ConversationAssertions: statestore.AssertionsSummary{
				Total:  2,
				Failed: 1,
			},
		},
	}

	// Initialize conversation data (normally done in toggleSelection)
	m.initializeConversationData(&m.activeRuns[0])

	view := m.View()
	assert.Contains(t, view, "Conversation")
	assert.Contains(t, view, "hello")
	assert.Contains(t, view, "assistant")
}

func TestModel_BuildSummary_FromStateStore(t *testing.T) {
	m := NewModel("test.yaml", 1)
	m.activeRuns = []RunInfo{{RunID: "run-1", Scenario: "scn", Provider: "prov", Region: "us"}}
	m.stateStore = &mockRunResultStore{
		result: &statestore.RunResult{
			RunID:      "run-1",
			ScenarioID: "scn",
			ProviderID: "prov",
			Region:     "us",
			Messages: []types.Message{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "hi"},
			},
			Duration: 2 * time.Second,
			Cost: types.CostInfo{
				TotalCost:    2.5,
				InputTokens:  10,
				OutputTokens: 5,
			},
			ConversationAssertions: statestore.AssertionsSummary{
				Total:  3,
				Failed: 1,
			},
		},
	}

	summary := m.BuildSummary("out", "")
	require.NotNil(t, summary)
	assert.Equal(t, 1, summary.TotalRuns)
	assert.Equal(t, 1, summary.ScenarioCount)
	assert.Equal(t, 2.5, summary.TotalCost)
	assert.Equal(t, int64(15), summary.TotalTokens)
	assert.Equal(t, 3, summary.AssertionTotal)
	assert.Equal(t, 1, summary.AssertionFailed)
}

func TestModel_BuildSummary_FromStateStoreError(t *testing.T) {
	m := NewModel("test.yaml", 1)
	m.activeRuns = []RunInfo{{RunID: "run-err", Scenario: "scn", Provider: "prov", Region: "us"}}
	m.stateStore = &mockRunResultStore{
		err: fmt.Errorf("load failed"),
	}

	summary := m.BuildSummary("out", "")
	require.NotNil(t, summary)
	assert.Equal(t, 1, summary.FailedCount)
	assert.Len(t, summary.Errors, 1)
}

// Tests for old render methods removed - now handled by panels/pages tests

func TestHandleTurnEvents(t *testing.T) {
	m := NewModel("test.yaml", 1)
	m.activeRuns = []RunInfo{{RunID: "run-1"}}

	start := TurnStartedMsg{RunID: "run-1", TurnIndex: 0, Role: "user", Time: time.Now()}
	m.handleTurnStarted(&start)
	assert.Equal(t, "user", m.activeRuns[0].CurrentTurnRole)

	done := TurnCompletedMsg{RunID: "run-1", TurnIndex: 0, Role: "assistant", Time: time.Now()}
	m.handleTurnCompleted(&done)
	assert.Equal(t, "assistant", m.activeRuns[0].CurrentTurnRole)
	assert.Greater(t, len(m.logs), 0)
}

func TestHandleRunLifecycle(t *testing.T) {
	m := NewModel("test.yaml", 1)
	now := time.Now()
	start := RunStartedMsg{RunID: "run-1", Scenario: "scn", Provider: "prov", Region: "us", Time: now}
	m.handleRunStarted(&start)
	require.Len(t, m.activeRuns, 1)
	require.Equal(t, StatusRunning, m.activeRuns[0].Status)

	complete := RunCompletedMsg{RunID: "run-1", Duration: time.Second, Cost: 1.0, Time: now.Add(time.Second)}
	m.handleRunCompleted(&complete)
	assert.Equal(t, StatusCompleted, m.activeRuns[0].Status)
	assert.Equal(t, 1, m.completedCount)

	fail := RunFailedMsg{RunID: "run-1", Error: fmt.Errorf("err"), Time: now.Add(2 * time.Second)}
	m.handleRunFailed(&fail)
	assert.Equal(t, StatusFailed, m.activeRuns[0].Status)
	assert.Equal(t, 1, m.failedCount)
}

func TestRenderHeaderFooter(t *testing.T) {
	// Test via View() which now uses HeaderFooterView internally
	m := NewModel("test.yaml", 2)
	m.width = 100
	m.height = 40
	m.isTUIMode = true
	view := m.View()
	assert.Contains(t, view, "test.yaml")
	assert.Contains(t, view, "PromptArena")
	assert.Contains(t, view, "tab")
	assert.Contains(t, view, "enter")
}

func TestSelectedRunHelper(t *testing.T) {
	m := NewModel("test.yaml", 1)
	assert.Nil(t, m.selectedRun())
	m.activeRuns = []RunInfo{{RunID: "run-1", Selected: true}}
	r := m.selectedRun()
	require.NotNil(t, r)
	assert.Equal(t, "run-1", r.RunID)
}

func TestUtilsHelpers(t *testing.T) {
	// buildProgressBar has been moved to views/header_footer.go
	// and is tested in views/header_footer_test.go
	// This test is kept for backwards compatibility
	assert.True(t, true)
}

func TestTick(t *testing.T) {
	cmd := tick()
	require.NotNil(t, cmd)

	msg := cmd()
	_, ok := msg.(tickMsg)
	assert.True(t, ok)
}

func TestCheckTerminalSize(t *testing.T) {
	width, height, supported, reason := CheckTerminalSize()

	// In CI/test environments, terminal may not be available
	if supported {
		assert.Greater(t, width, MinTerminalWidth-1)
		assert.Greater(t, height, MinTerminalHeight-1)
	} else {
		assert.NotEmpty(t, reason, "If not supported, reason should be provided")
	}
	// Width and height should always be set to some value (default or actual)
	assert.GreaterOrEqual(t, width, 0)
	assert.GreaterOrEqual(t, height, 0)
}

func TestModel_renderHeader(t *testing.T) {
	// Test header via View() which uses HeaderFooterView internally
	m := NewModel("test.yaml", 10)
	m.width = 120
	m.height = 40
	m.isTUIMode = true
	m.completedCount = 5
	view := m.View()
	assert.Contains(t, view, "PromptArena")
	assert.Contains(t, view, "5/10")
}

// Old render tests removed - now handled by panels/views tests

func TestRunStatus_Constants(t *testing.T) {
	// Verify the status constants exist and are distinct
	assert.NotEqual(t, StatusRunning, StatusCompleted)
	assert.NotEqual(t, StatusRunning, StatusFailed)
	assert.NotEqual(t, StatusCompleted, StatusFailed)
}

func TestModel_ThreadSafety(t *testing.T) {
	m := NewModel("test.yaml", 100)

	done := make(chan bool, 10)

	// Start 10 goroutines performing concurrent operations
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 10; j++ {
				// Update with different message types
				m.Update(tickMsg{})
				m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

				// Render the view (reads state)
				_ = m.View()

				// Modify state directly (simulating external updates)
				m.mu.Lock()
				m.activeRuns = append(m.activeRuns, RunInfo{
					RunID:     "run-test",
					Status:    StatusRunning,
					StartTime: time.Now(),
				})
				m.completedCount++
				m.successCount++
				m.mu.Unlock()
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Just verify we didn't panic and counts increased
	assert.Greater(t, m.completedCount, 0)
	assert.Greater(t, m.successCount, 0)
}

// TestStatusString removed - status conversion tested in views/result_test.go

func TestSetStateStore(t *testing.T) {
	m := NewModel("test.yaml", 1)
	store := &mockRunResultStore{}
	m.SetStateStore(store)
	m.mu.Lock()
	defer m.mu.Unlock()
	assert.Equal(t, store, m.stateStore)
}

func TestRun_NotSupported(t *testing.T) {
	m := NewModel("test.yaml", 1)
	m.isTUIMode = false
	m.fallbackReason = "notty"
	ctx := context.Background()
	err := Run(ctx, m)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "notty")
}

func TestEscapeClearsSelection(t *testing.T) {
	m := NewModel("test.yaml", 1)
	m.activeRuns = []RunInfo{{RunID: "run-1", Selected: true}}
	m.currentPage = pageConversation
	msg := tea.KeyMsg{Type: tea.KeyEsc}
	m.Update(msg)
	assert.Nil(t, m.selectedRun())
}

func TestHandleLogMsg(t *testing.T) {
	m := NewModel("test.yaml", 1)
	now := time.Now()

	// Use the Update method with logging.Msg
	msg := logging.Msg{
		Timestamp: now,
		Level:     "INFO",
		Message:   "Test log message",
	}
	m.Update(msg)

	m.mu.Lock()
	defer m.mu.Unlock()
	require.Len(t, m.logs, 1)
	assert.Equal(t, "INFO", m.logs[0].Level)
	assert.Equal(t, "Test log message", m.logs[0].Message)
	assert.Equal(t, now, m.logs[0].Timestamp)
}

func TestTrimLogs(t *testing.T) {
	m := NewModel("test.yaml", 1)
	m.mu.Lock()
	defer m.mu.Unlock()

	// Fill logs beyond maxLogBufferSize
	for i := 0; i < maxLogBufferSize+50; i++ {
		m.logs = append(m.logs, LogEntry{
			Timestamp: time.Now(),
			Level:     "INFO",
			Message:   fmt.Sprintf("Log %d", i),
		})
	}

	m.trimLogs()
	assert.Equal(t, maxLogBufferSize, len(m.logs))
	// Should keep the most recent logs
	assert.Contains(t, m.logs[len(m.logs)-1].Message, "149")
}

func TestConvertToRunInfos(t *testing.T) {
	m := NewModel("test.yaml", 1)
	m.activeRuns = []RunInfo{
		{
			RunID:            "run-1",
			Scenario:         "scn1",
			Provider:         "prov1",
			Region:           "us",
			Status:           StatusRunning,
			Duration:         time.Second,
			Cost:             1.5,
			Error:            "",
			StartTime:        time.Now(),
			CurrentTurnIndex: 2,
			CurrentTurnRole:  "assistant",
			Selected:         false,
		},
		{
			RunID:    "run-2",
			Scenario: "scn2",
			Provider: "prov2",
			Status:   StatusCompleted,
			Duration: 2 * time.Second,
			Cost:     0.5,
		},
	}

	runs := m.convertToRunInfos()
	require.Len(t, runs, 2)
	assert.Equal(t, "run-1", runs[0].RunID)
	assert.Equal(t, "scn1", runs[0].Scenario)
	assert.Equal(t, "prov1", runs[0].Provider)
	assert.Equal(t, 2, runs[0].CurrentTurnIndex)
	assert.Equal(t, "assistant", runs[0].CurrentTurnRole)
	assert.Equal(t, "run-2", runs[1].RunID)
	// Status is converted to panel's RunStatus type
	assert.Equal(t, int(StatusCompleted), int(runs[1].Status))
}

func TestConvertToLogEntries(t *testing.T) {
	m := NewModel("test.yaml", 1)
	m.logs = []LogEntry{
		{Level: "INFO", Message: "info message"},
		{Level: "WARN", Message: "warning message"},
		{Level: "ERROR", Message: "error message"},
	}

	logs := m.convertToLogEntries()
	require.Len(t, logs, 3)
	assert.Equal(t, "INFO", logs[0].Level)
	assert.Equal(t, "info message", logs[0].Message)
	assert.Equal(t, "WARN", logs[1].Level)
	assert.Equal(t, "ERROR", logs[2].Level)
}

func TestCurrentRunForDetail(t *testing.T) {
	m := NewModel("test.yaml", 2)

	// No runs - returns nil
	run := m.currentRunForDetail()
	assert.Nil(t, run)

	// With runs but no selection
	m.activeRuns = []RunInfo{
		{RunID: "run-1", Scenario: "scn1"},
		{RunID: "run-2", Scenario: "scn2"},
	}
	run = m.currentRunForDetail()
	// Should return first run when no selection
	assert.NotNil(t, run)
	assert.Equal(t, "run-1", run.RunID)

	// With selected run
	m.activeRuns[1].Selected = true
	run = m.currentRunForDetail()
	assert.NotNil(t, run)
	assert.Equal(t, "run-2", run.RunID)
}

func TestRenderMainPage_NoStateStore(t *testing.T) {
	m := NewModel("test.yaml", 1)
	m.width = 100
	m.height = 30
	m.isTUIMode = true
	m.activeRuns = []RunInfo{{RunID: "run-1", Scenario: "scn", Provider: "prov"}}

	// stateStore is nil by default
	body := m.renderMainPage(25)
	assert.NotEmpty(t, body)
}

func TestRenderMainPage_WithResultData(t *testing.T) {
	m := NewModel("test.yaml", 1)
	m.width = 120
	m.height = 40
	m.isTUIMode = true
	m.activeRuns = []RunInfo{{RunID: "run-1", Scenario: "scn", Provider: "prov", Status: StatusCompleted}}

	m.stateStore = &mockRunResultStore{
		result: &statestore.RunResult{
			RunID:      "run-1",
			ScenarioID: "scn",
			ProviderID: "prov",
			Messages:   []types.Message{{Role: "user", Content: "hello"}},
			ConversationAssertions: statestore.AssertionsSummary{
				Total:  2,
				Failed: 1,
			},
		},
	}

	body := m.renderMainPage(35)
	assert.NotEmpty(t, body)
}

func TestRenderConversationPage_NoSelection(t *testing.T) {
	m := NewModel("test.yaml", 1)
	m.width = 100
	m.height = 30
	m.isTUIMode = true

	body := m.renderConversationPage(25)
	assert.Contains(t, body, "Select a run")
}

func TestRenderConversationPage_NoStateStore(t *testing.T) {
	m := NewModel("test.yaml", 1)
	m.width = 100
	m.height = 30
	m.isTUIMode = true
	m.activeRuns = []RunInfo{{RunID: "run-1", Selected: true}}

	body := m.renderConversationPage(25)
	assert.Contains(t, body, "No state store")
}

func TestRenderConversationPage_LoadError(t *testing.T) {
	m := NewModel("test.yaml", 1)
	m.width = 100
	m.height = 30
	m.isTUIMode = true
	m.activeRuns = []RunInfo{{RunID: "run-1", Scenario: "test", Provider: "openai", Selected: true}}
	m.stateStore = &mockRunResultStore{err: fmt.Errorf("load failed")}

	// Initialize conversation (normally done in toggleSelection)
	m.initializeConversationData(&m.activeRuns[0])

	body := m.renderConversationPage(25)
	// When result doesn't exist (e.g., run in progress), show waiting message
	// Real-time events will populate it as messages arrive
	assert.Contains(t, body, "Waiting for conversation to start")
}

func TestView_SmallHeight(t *testing.T) {
	m := NewModel("test.yaml", 1)
	m.width = 100
	m.height = 10 // Very small height
	m.isTUIMode = true

	view := m.View()
	assert.NotEmpty(t, view)
}

func TestView_NoTUIMode(t *testing.T) {
	m := NewModel("test.yaml", 1)
	m.width = 100
	m.height = 30
	m.isTUIMode = false

	view := m.View()
	assert.Empty(t, view)
}

func TestView_NotInitialized(t *testing.T) {
	m := NewModel("test.yaml", 1)
	m.isTUIMode = true
	// NewModel sets minimum dimensions, so explicitly set to 0
	m.width = 0
	m.height = 0

	view := m.View()
	assert.Equal(t, "Loading...", view)
}

func TestUpdate_RunStartedMsg(t *testing.T) {
	m := NewModel("test.yaml", 2)
	now := time.Now()
	msg := RunStartedMsg{
		RunID:    "run-1",
		Scenario: "scenario1",
		Provider: "provider1",
		Region:   "us-west",
		Time:     now,
	}

	updatedModel, cmd := m.Update(msg)
	assert.Nil(t, cmd)

	updated := updatedModel.(*Model)
	require.Len(t, updated.activeRuns, 1)
	assert.Equal(t, "run-1", updated.activeRuns[0].RunID)
	assert.Equal(t, "scenario1", updated.activeRuns[0].Scenario)
	assert.Equal(t, "provider1", updated.activeRuns[0].Provider)
	assert.Equal(t, "us-west", updated.activeRuns[0].Region)
	assert.Equal(t, StatusRunning, updated.activeRuns[0].Status)
	assert.Greater(t, len(updated.logs), 0)
}

func TestUpdate_RunCompletedMsg(t *testing.T) {
	m := NewModel("test.yaml", 2)
	m.activeRuns = []RunInfo{{RunID: "run-1", Status: StatusRunning}}

	msg := RunCompletedMsg{
		RunID:    "run-1",
		Duration: 2 * time.Second,
		Cost:     1.5,
		Time:     time.Now(),
	}

	updatedModel, cmd := m.Update(msg)
	assert.Nil(t, cmd)

	updated := updatedModel.(*Model)
	require.Len(t, updated.activeRuns, 1)
	assert.Equal(t, StatusCompleted, updated.activeRuns[0].Status)
	assert.Equal(t, 2*time.Second, updated.activeRuns[0].Duration)
	assert.Equal(t, 1.5, updated.activeRuns[0].Cost)
	assert.Equal(t, 1, updated.completedCount)
	assert.Equal(t, 1, updated.successCount)
	assert.Equal(t, 1.5, updated.totalCost)
	assert.Equal(t, 2*time.Second, updated.totalDuration)
}

func TestUpdate_RunFailedMsg(t *testing.T) {
	m := NewModel("test.yaml", 2)
	m.activeRuns = []RunInfo{{RunID: "run-1", Status: StatusRunning}}

	msg := RunFailedMsg{
		RunID: "run-1",
		Error: fmt.Errorf("execution failed"),
		Time:  time.Now(),
	}

	updatedModel, cmd := m.Update(msg)
	assert.Nil(t, cmd)

	updated := updatedModel.(*Model)
	require.Len(t, updated.activeRuns, 1)
	assert.Equal(t, StatusFailed, updated.activeRuns[0].Status)
	assert.Contains(t, updated.activeRuns[0].Error, "execution failed")
	assert.Equal(t, 1, updated.completedCount)
	assert.Equal(t, 1, updated.failedCount)
	assert.Equal(t, 0, updated.successCount)
}

func TestUpdate_TurnStartedMsg(t *testing.T) {
	m := NewModel("test.yaml", 1)
	m.activeRuns = []RunInfo{{RunID: "run-1"}}

	msg := TurnStartedMsg{
		RunID:     "run-1",
		TurnIndex: 0,
		Role:      "user",
		Time:      time.Now(),
	}

	updatedModel, _ := m.Update(msg)
	updated := updatedModel.(*Model)

	assert.Equal(t, "user", updated.activeRuns[0].CurrentTurnRole)
	assert.Equal(t, 0, updated.activeRuns[0].CurrentTurnIndex)
	assert.Greater(t, len(updated.logs), 0)
}

func TestUpdate_TurnCompletedMsg(t *testing.T) {
	m := NewModel("test.yaml", 1)
	m.activeRuns = []RunInfo{{RunID: "run-1", CurrentTurnRole: "user"}}

	msg := TurnCompletedMsg{
		RunID:     "run-1",
		TurnIndex: 0,
		Role:      "assistant",
		Time:      time.Now(),
		Error:     nil,
	}

	updatedModel, _ := m.Update(msg)
	updated := updatedModel.(*Model)

	assert.Equal(t, "assistant", updated.activeRuns[0].CurrentTurnRole)
	assert.Greater(t, len(updated.logs), 0)
	assert.Contains(t, updated.logs[0].Message, "completed")
}

func TestUpdate_TurnCompletedMsg_WithError(t *testing.T) {
	m := NewModel("test.yaml", 1)
	m.activeRuns = []RunInfo{{RunID: "run-1"}}

	msg := TurnCompletedMsg{
		RunID:     "run-1",
		TurnIndex: 1,
		Role:      "assistant",
		Time:      time.Now(),
		Error:     fmt.Errorf("turn failed"),
	}

	updatedModel, _ := m.Update(msg)
	updated := updatedModel.(*Model)

	assert.Contains(t, updated.activeRuns[0].Error, "turn failed")
	assert.Greater(t, len(updated.logs), 0)
	assert.Contains(t, updated.logs[0].Message, "failed")
	assert.Equal(t, "ERROR", updated.logs[0].Level)
}

func TestUpdate_LoggingMsg(t *testing.T) {
	m := NewModel("test.yaml", 1)
	now := time.Now()

	msg := logging.Msg{
		Timestamp: now,
		Level:     "WARN",
		Message:   "Warning message from logger",
	}

	updatedModel, _ := m.Update(msg)
	updated := updatedModel.(*Model)

	updated.mu.Lock()
	defer updated.mu.Unlock()
	require.Greater(t, len(updated.logs), 0)
	lastLog := updated.logs[len(updated.logs)-1]
	assert.Equal(t, "WARN", lastLog.Level)
	assert.Equal(t, "Warning message from logger", lastLog.Message)
	assert.Equal(t, now, lastLog.Timestamp)
}

func TestUpdate_MouseMsg(t *testing.T) {
	m := NewModel("test.yaml", 1)

	msg := tea.MouseMsg{}
	updatedModel, cmd := m.Update(msg)

	assert.NotNil(t, updatedModel)
	assert.Nil(t, cmd)
}

func TestNewModel_InitializesPages(t *testing.T) {
	m := NewModel("test.yaml", 5)

	assert.NotNil(t, m.mainPage)
	assert.NotNil(t, m.conversationPage)
	assert.Equal(t, pageMain, m.currentPage)
}

func TestContextUsage(t *testing.T) {
	m := NewModel("test.yaml", 1)
	m.stateStore = &mockRunResultStore{
		result: &statestore.RunResult{RunID: "run-1"},
	}
	m.activeRuns = []RunInfo{{RunID: "run-1", Selected: true}}

	// ctx is used internally when fetching from state store
	// Verify it works with nil context (defaults to Background)
	m.ctx = nil
	body := m.renderConversationPage(25)
	assert.NotEmpty(t, body)

	// Verify it works with a set context
	ctx := context.WithValue(context.Background(), "key", "value")
	m.ctx = ctx
	body = m.renderConversationPage(25)
	assert.NotEmpty(t, body)
}

func TestRenderSummary(t *testing.T) {
	summary := &Summary{
		TotalRuns:      10,
		SuccessCount:   8,
		FailedCount:    2,
		TotalCost:      5.67,
		TotalTokens:    1000,
		TotalDuration:  30 * time.Second,
		AvgDuration:    3 * time.Second,
		ProviderCounts: map[string]int{"openai": 5, "anthropic": 5},
		ScenarioCount:  3,
		Regions:        []string{"us-east-1", "us-west-2"},
		Errors: []ErrorInfo{
			{RunID: "run-1", Scenario: "scn1", Provider: "prov1", Region: "us", Error: "failed"},
		},
		OutputDir:       "/tmp/output",
		HTMLReport:      "/tmp/report.html",
		AssertionTotal:  20,
		AssertionFailed: 2,
	}

	output := RenderSummary(summary, 120)
	assert.NotEmpty(t, output)
	// Verify it doesn't panic and returns a string
}

func TestRenderSummaryCIMode(t *testing.T) {
	summary := &Summary{
		TotalRuns:      5,
		SuccessCount:   4,
		FailedCount:    1,
		TotalCost:      2.5,
		TotalTokens:    500,
		TotalDuration:  15 * time.Second,
		AvgDuration:    3 * time.Second,
		ProviderCounts: map[string]int{"openai": 5},
		ScenarioCount:  2,
		Regions:        []string{"us-east-1"},
		Errors:         []ErrorInfo{},
		OutputDir:      "/tmp/output",
		HTMLReport:     "",
		AssertionTotal: 10,
	}

	output := RenderSummaryCIMode(summary)
	assert.NotEmpty(t, output)
	// CI mode should render plain text without fancy formatting
}

func TestConvertSummaryToData(t *testing.T) {
	summary := &Summary{
		TotalRuns:      10,
		SuccessCount:   8,
		FailedCount:    2,
		TotalCost:      5.67,
		TotalTokens:    1000,
		TotalDuration:  30 * time.Second,
		AvgDuration:    3 * time.Second,
		ProviderCounts: map[string]int{"openai": 5, "anthropic": 5},
		ScenarioCount:  3,
		Regions:        []string{"us-east-1", "us-west-2"},
		Errors: []ErrorInfo{
			{RunID: "run-1", Scenario: "scn1", Provider: "prov1", Region: "us", Error: "failed"},
		},
		OutputDir:       "/tmp/output",
		HTMLReport:      "/tmp/report.html",
		AssertionTotal:  20,
		AssertionFailed: 2,
	}

	data := convertSummaryToData(summary)
	require.NotNil(t, data)
	assert.Equal(t, 10, data.TotalRuns)
	assert.Equal(t, 8, data.CompletedRuns)
	assert.Equal(t, 2, data.FailedRuns)
	assert.Equal(t, int64(1000), data.TotalTokens)
	assert.Equal(t, 5.67, data.TotalCost)
	assert.Equal(t, 30*time.Second, data.TotalDuration)
	assert.Equal(t, 3*time.Second, data.AvgDuration)
	assert.Equal(t, 3, data.ScenarioCount)
	assert.Equal(t, []string{"us-east-1", "us-west-2"}, data.Regions)
	assert.Equal(t, "/tmp/output", data.OutputDir)
	assert.Equal(t, "/tmp/report.html", data.HTMLReport)
	assert.Equal(t, 20, data.AssertionTotal)
	assert.Equal(t, 2, data.AssertionFailed)

	// Verify provider stats conversion
	require.Len(t, data.ProviderStats, 2)
	assert.Equal(t, 5, data.ProviderStats["openai"].Runs)
	assert.Equal(t, 5, data.ProviderStats["anthropic"].Runs)

	// Verify errors conversion
	require.Len(t, data.Errors, 1)
	assert.Equal(t, "run-1", data.Errors[0].RunID)
	assert.Equal(t, "scn1", data.Errors[0].Scenario)
	assert.Equal(t, "prov1", data.Errors[0].Provider)
	assert.Equal(t, "failed", data.Errors[0].Error)
}

func TestHandleMainPageKey_TabSwitchingWithFocus(t *testing.T) {
	m := NewModel("test.yaml", 2)
	m.width = 100
	m.height = 30
	m.isTUIMode = true
	m.activePane = paneRuns

	// Initially on runs pane
	assert.Equal(t, paneRuns, m.activePane)

	// Tab switches to logs
	tabMsg := tea.KeyMsg{Type: tea.KeyTab}
	updated, _ := m.Update(tabMsg)
	updatedM := updated.(*Model)
	assert.Equal(t, paneLogs, updatedM.activePane)

	// Tab again switches to result
	updated2, _ := updatedM.Update(tabMsg)
	updatedM2 := updated2.(*Model)
	assert.Equal(t, paneResult, updatedM2.activePane)

	// Tab third time switches back to runs
	updated3, _ := updatedM2.Update(tabMsg)
	updatedM3 := updated3.(*Model)
	assert.Equal(t, paneRuns, updatedM3.activePane)
}

func TestHandleMainPageKey_EnterToggleSelection(t *testing.T) {
	m := NewModel("test.yaml", 2)
	m.width = 100
	m.height = 30
	m.isTUIMode = true
	m.activePane = paneRuns
	m.activeRuns = []RunInfo{
		{RunID: "run-1", Scenario: "scn1"},
		{RunID: "run-2", Scenario: "scn2"},
	}

	// Enter selects the current run
	enterMsg := tea.KeyMsg{Type: tea.KeyEnter}
	updated, _ := m.Update(enterMsg)
	updatedM := updated.(*Model)

	// Should have selected run-1 (first in list)
	assert.True(t, updatedM.activeRuns[0].Selected)
	assert.Equal(t, pageConversation, updatedM.currentPage)

	// Go back to main page
	updatedM.currentPage = pageMain
	updatedM.activeRuns[0].Selected = false

	// Select second run by moving cursor (simulated)
	// Note: In real usage, arrow keys would move cursor
	// For this test, we verify the toggle behavior
}

func TestHandleMainPageKey_ArrowKeysOnRunsTable(t *testing.T) {
	m := NewModel("test.yaml", 2)
	m.width = 100
	m.height = 30
	m.isTUIMode = true
	m.activePane = paneRuns
	m.activeRuns = []RunInfo{
		{RunID: "run-1", Scenario: "scn1"},
		{RunID: "run-2", Scenario: "scn2"},
	}

	// Arrow keys should be handled by the table
	downMsg := tea.KeyMsg{Type: tea.KeyDown}
	updated, cmd := m.Update(downMsg)
	// Should not panic and return a model
	assert.NotNil(t, updated)
	// May or may not return a command depending on table state
	_ = cmd
}

func TestHandleMainPageKey_LogsPaneScrolling(t *testing.T) {
	m := NewModel("test.yaml", 1)
	m.width = 100
	m.height = 30
	m.isTUIMode = true
	m.activePane = paneLogs
	m.logs = []LogEntry{
		{Level: "INFO", Message: "log 1"},
		{Level: "INFO", Message: "log 2"},
		{Level: "INFO", Message: "log 3"},
	}

	// Arrow keys on logs pane should scroll viewport
	downMsg := tea.KeyMsg{Type: tea.KeyDown}
	updated, _ := m.Update(downMsg)
	assert.NotNil(t, updated)

	upMsg := tea.KeyMsg{Type: tea.KeyUp}
	updated2, _ := updated.Update(upMsg)
	assert.NotNil(t, updated2)
}

func TestToggleSelection_OutOfBounds(t *testing.T) {
	m := NewModel("test.yaml", 1)
	m.width = 100
	m.height = 30
	m.isTUIMode = true
	m.activePane = paneRuns
	// No runs

	// Enter should not panic with empty runs
	enterMsg := tea.KeyMsg{Type: tea.KeyEnter}
	updated, _ := m.Update(enterMsg)
	assert.NotNil(t, updated)
}

func TestBuildSummary_InternalThreadSafety(t *testing.T) {
	m := NewModel("test.yaml", 5)
	m.activeRuns = []RunInfo{
		{RunID: "run-1", Scenario: "scn1", Provider: "prov1", Region: "us", Status: StatusCompleted},
	}
	m.completedCount = 1
	m.successCount = 1
	m.totalCost = 1.5
	m.totalDuration = 2 * time.Second

	// BuildSummary is thread-safe and acquires mutex
	summary := m.BuildSummary("/tmp", "")
	require.NotNil(t, summary)
	assert.Equal(t, 5, summary.TotalRuns)
	assert.Equal(t, 1, summary.SuccessCount)
}

func TestBuildSummaryFromStateStore_WithContext(t *testing.T) {
	m := NewModel("test.yaml", 1)
	m.activeRuns = []RunInfo{{RunID: "run-1", Scenario: "scn", Provider: "prov", Region: "us"}}
	m.stateStore = &mockRunResultStore{
		result: &statestore.RunResult{
			RunID:      "run-1",
			ScenarioID: "scn",
			ProviderID: "prov",
			Region:     "us",
			Messages: []types.Message{
				{Role: "user", Content: "hello"},
			},
			Duration: 2 * time.Second,
			Cost: types.CostInfo{
				TotalCost:    1.5,
				InputTokens:  10,
				OutputTokens: 5,
			},
			ConversationAssertions: statestore.AssertionsSummary{
				Total:  3,
				Failed: 1,
			},
		},
	}

	// With custom context
	customCtx := context.WithValue(context.Background(), "testKey", "testValue")
	m.ctx = customCtx

	summary := m.BuildSummary("/tmp", "")
	require.NotNil(t, summary)
	assert.Equal(t, 1, summary.TotalRuns)
	assert.Equal(t, 1.5, summary.TotalCost)
	assert.Equal(t, int64(15), summary.TotalTokens)
	assert.Equal(t, 3, summary.AssertionTotal)
	assert.Equal(t, 1, summary.AssertionFailed)
}
