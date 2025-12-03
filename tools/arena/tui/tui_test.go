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

// TestMainPageShowsResultAndSummary removed - now tested via View() and pages/main_test.go
