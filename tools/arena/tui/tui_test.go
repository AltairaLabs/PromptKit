package tui

import (
	"context"
	"fmt"
	"strings"
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
	assert.Contains(t, view, "Run: run-1")
	assert.Contains(t, view, "Assertions: 2 total")
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
			Duration:   2 * time.Second,
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

func TestRenderLogs_NoViewport(t *testing.T) {
	m := NewModel("test.yaml", 1)
	m.viewportReady = false
	out := m.renderLogs()
	assert.Contains(t, out, "Initializing")
}

func TestRenderMetrics_Content(t *testing.T) {
	m := NewModel("test.yaml", 3)
	m.completedCount = 2
	m.successCount = 1
	m.failedCount = 1
	m.totalCost = 1.23
	m.totalTokens = 1234
	m.totalDuration = 4 * time.Second

	out := m.renderMetrics()
	assert.Contains(t, out, "Completed:")
	assert.Contains(t, out, "Errors")
	assert.Contains(t, out, "$1.2300")
	assert.Contains(t, out, "1,234")
}

func TestRenderActiveRuns_Notes(t *testing.T) {
	m := NewModel("test.yaml", 1)
	m.width = 150 // ensure all columns, including Notes, render
	m.height = 30
	m.isTUIMode = true
	m.activeRuns = []RunInfo{{
		RunID:            "run-1",
		Scenario:         "scn",
		Provider:         "prov",
		Status:           StatusRunning,
		StartTime:        time.Now(),
		CurrentTurnIndex: 1,
		CurrentTurnRole:  "assistant",
	}}
	out := m.renderActiveRuns()
	assert.Contains(t, out, "turn 2: assistant")
}

func TestRenderLogs_SelectedResultBranch(t *testing.T) {
	m := NewModel("test.yaml", 1)
	m.width = 100
	m.height = 30
	m.activeRuns = []RunInfo{{
		RunID:     "run-1",
		Status:    StatusCompleted,
		Selected:  true,
		Scenario:  "scn",
		Provider:  "prov",
		StartTime: time.Now(),
	}}
	m.stateStore = &mockRunResultStore{
		result: &statestore.RunResult{
			RunID:      "run-1",
			ScenarioID: "scn",
			ProviderID: "prov",
			Region:     "us",
			Cost: types.CostInfo{
				TotalCost:    1.0,
				InputTokens:  1,
				OutputTokens: 1,
			},
			ConversationAssertions: statestore.AssertionsSummary{Total: 1},
		},
	}
	out := m.renderLogs()
	assert.Contains(t, out, "run-1")
	assert.Contains(t, out, "Scenario: scn")
	assert.Contains(t, out, "Assertions: 1 total")
}

func TestRenderSelectedResult_Error(t *testing.T) {
	m := NewModel("test.yaml", 1)
	m.stateStore = &mockRunResultStore{err: fmt.Errorf("boom")}
	res := m.renderSelectedResult(RunInfo{RunID: "run-1"})
	assert.Contains(t, res, "Failed to load result")
}

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
	m := NewModel("test.yaml", 2)
	m.width = 100
	out := m.renderHeader(2 * time.Second)
	assert.Contains(t, out, "test.yaml")
	assert.Contains(t, out, "PromptArena")

	footer := m.renderFooter()
	assert.Contains(t, footer, "tab")
	assert.Contains(t, footer, "enter")
}

func TestRenderLogs_WithViewport(t *testing.T) {
	m := NewModel("test.yaml", 1)
	m.width = 120
	m.height = 40
	m.viewportReady = true
	m.initViewport()
	m.logs = []LogEntry{{Level: "INFO", Message: "hello"}}
	out := m.renderLogs()
	assert.Contains(t, out, "hello")
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
	assert.Equal(t, "1,234", formatNumber(1234))
	assert.Equal(t, "abcd...", truncateString("abcdefgh", 7))
	bar := buildProgressBar(1, 4, 4)
	assert.NotEmpty(t, bar)
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{"zero", 0, "0ms"},
		{"milliseconds", 500 * time.Millisecond, "500ms"},
		{"seconds", 2 * time.Second, "2s"},
		{"minutes", 65 * time.Second, "1m5s"},
		{"hours", 3725 * time.Second, "1h2m5s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDuration(tt.duration)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		name string
		num  int64
		want string
	}{
		{"zero", 0, "0"},
		{"small", 999, "999"},
		{"thousand", 1000, "1,000"},
		{"million", 1234567, "1,234,567"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatNumber(tt.num)
			assert.Equal(t, tt.want, got)
		})
	}
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
	m := NewModel("test.yaml", 10)
	m.completedCount = 5

	header := m.renderHeader(30 * time.Second)

	assert.Contains(t, header, "PromptArena")
	assert.Contains(t, header, "5/10")
	assert.Contains(t, header, "30")
}

func TestModel_renderActiveRuns(t *testing.T) {
	m := NewModel("test.yaml", 10)
	m.width = 120

	m.activeRuns = append(m.activeRuns, RunInfo{
		RunID:     "run-1",
		Scenario:  "test-scenario",
		Provider:  "openai",
		Status:    StatusRunning,
		StartTime: time.Now().Add(-2 * time.Second),
	})

	runs := m.renderActiveRuns()

	assert.Contains(t, runs, "Active Runs")
	assert.Contains(t, runs, "1 concurrent workers")
	// The actual run details may be truncated due to height constraints
	assert.NotEmpty(t, runs)
}

func TestModel_renderActiveRuns_Empty(t *testing.T) {
	m := NewModel("test.yaml", 10)
	m.width = 100

	runs := m.renderActiveRuns()

	assert.Contains(t, runs, "Active Runs")
	// When empty, shows worker count
	assert.Contains(t, runs, "0 concurrent workers")
}

func TestModel_renderMetrics(t *testing.T) {
	m := NewModel("test.yaml", 10)
	m.completedCount = 5
	m.successCount = 4
	m.failedCount = 1
	m.totalCost = 0.25
	m.totalTokens = 5000
	m.totalDuration = 10 * time.Second

	m.width = 100
	metrics := m.renderMetrics()

	assert.Contains(t, metrics, "Metrics")
	assert.Contains(t, metrics, "5/10") // Completed count
	assert.Contains(t, metrics, "$0.2500")
	assert.Contains(t, metrics, "5,000")
}

func TestModel_renderLogs(t *testing.T) {
	m := NewModel("test.yaml", 10)
	m.width = 120
	m.height = 40

	// Initialize viewport
	m.initViewport()
	m.viewportReady = true

	// Manually add logs
	m.logs = append(m.logs, LogEntry{
		Timestamp: time.Now(),
		Level:     "INFO",
		Message:   "Test log message",
	})

	logs := m.renderLogs()

	assert.Contains(t, logs, "Logs")
	assert.Contains(t, logs, "Test log message")
}

func TestModel_formatRunLine_Running(t *testing.T) {
	m := NewModel("test.yaml", 10)

	run := RunInfo{
		RunID:     "run-1",
		Scenario:  "test",
		Provider:  "openai",
		Region:    "us-west-1",
		Status:    StatusRunning,
		StartTime: time.Now().Add(-2 * time.Second),
	}

	m.width = 120
	line := m.formatRunLine(&run)
	// Format is [status] provider/scenario/region  ⏱ duration
	assert.Contains(t, line, "test")
	assert.Contains(t, line, "openai")
	assert.Contains(t, line, "us-west-1")
}

func TestModel_formatRunLine_Completed(t *testing.T) {
	m := NewModel("test.yaml", 10)

	run := RunInfo{
		RunID:     "run-1",
		Scenario:  "test",
		Provider:  "openai",
		Status:    StatusCompleted,
		Duration:  5 * time.Second,
		Cost:      0.05,
		StartTime: time.Now().Add(-5 * time.Second),
	}

	m.width = 120
	line := m.formatRunLine(&run)
	// Format is [status] provider/scenario/region  ⏱ duration $cost
	assert.Contains(t, line, "test")
	assert.Contains(t, line, "openai")
	assert.Contains(t, line, "$0.0500")
}

func TestModel_formatRunLine_Failed(t *testing.T) {
	m := NewModel("test.yaml", 10)

	run := RunInfo{
		RunID:     "run-1",
		Scenario:  "test",
		Provider:  "openai",
		Status:    StatusFailed,
		Error:     "connection timeout",
		StartTime: time.Now().Add(-1 * time.Second),
	}

	m.width = 120
	line := m.formatRunLine(&run)
	// Format shows provider/scenario and ERROR keyword (actual error is in separate section or truncated)
	assert.Contains(t, line, "test")
	assert.Contains(t, line, "openai")
	assert.Contains(t, line, "ERROR")
}

func TestModel_formatLogLine(t *testing.T) {
	m := NewModel("test.yaml", 10)

	log := LogEntry{
		Timestamp: time.Now(),
		Level:     "INFO",
		Message:   "Test message",
	}

	line := m.formatLogLine(log)
	assert.Contains(t, line, "INFO")
	assert.Contains(t, line, "Test message")

	unknown := LogEntry{Timestamp: time.Now(), Level: "OTHER", Message: "fallback"}
	fallback := m.formatLogLine(unknown)
	assert.Contains(t, fallback, "OTHER")
	assert.Contains(t, fallback, "fallback")
}

func TestModel_formatLogLine_LongMessage(t *testing.T) {
	m := NewModel("test.yaml", 10)
	m.width = 100

	longMessage := strings.Repeat("This is a long message. ", 20)
	log := LogEntry{
		Timestamp: time.Now(),
		Level:     "ERROR",
		Message:   longMessage,
	}

	line := m.formatLogLine(log)
	assert.Contains(t, line, "ERROR")
	// Line should be truncated
	assert.Less(t, len(line), len(longMessage)+50)
}

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

func TestObserver_TurnCallbacks(t *testing.T) {
	m := NewModel("test.yaml", 1)
	m.activeRuns = []RunInfo{{RunID: "run-1"}}
	obs := NewObserverWithModel(m)

	obs.OnTurnStarted("run-1", 0, "user", "scn")
	require.Equal(t, "user", m.activeRuns[0].CurrentTurnRole)

	err := fmt.Errorf("boom")
	obs.OnTurnCompleted("run-1", 0, "assistant", "scn", err)
	require.Equal(t, "assistant", m.activeRuns[0].CurrentTurnRole)
	require.Equal(t, "boom", m.activeRuns[0].Error)
	require.NotZero(t, len(m.logs))
}

func TestStatusString(t *testing.T) {
	assert.Equal(t, "running", statusString(StatusRunning))
	assert.Equal(t, "completed", statusString(StatusCompleted))
	assert.Equal(t, "failed", statusString(StatusFailed))
	assert.Equal(t, "unknown", statusString(RunStatus(99)))
}

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
