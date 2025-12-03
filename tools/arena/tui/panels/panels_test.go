package panels

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

func TestConversationPanel_ViewAndNavigation(t *testing.T) {
	panel := NewConversationPanel()
	panel.SetDimensions(120, 40)

	res := &statestore.RunResult{
		RunID: "run-1",
		Messages: []types.Message{
			{Role: "user", Content: "hello"},
			{
				Role:    "assistant",
				Content: "hi there",
				ToolCalls: []types.MessageToolCall{
					{Name: "list_devices", Args: []byte(`{"customer_id":"acme"}`)},
				},
				ToolResult: &types.MessageToolResult{
					Name:    "list_devices",
					Content: `{"devices":[1,2]}`,
				},
			},
		},
	}

	panel.SetData("run-1", "scn", "prov", res)
	down := tea.KeyMsg{Type: tea.KeyDown}
	_ = panel.Update(down)
	out := panel.View()
	assert.Contains(t, out, "Conversation")
	assert.Contains(t, out, "list_devices")
	assert.Contains(t, out, "customer_id")
	assert.Contains(t, out, "Turn 2")
	assert.Contains(t, out, "Tokens:")
	assert.Contains(t, out, "scroll")
}

func TestConversationPanel_Reset(t *testing.T) {
	panel := NewConversationPanel()
	panel.SetDimensions(80, 30)
	res := &statestore.RunResult{
		RunID: "run-1",
		Messages: []types.Message{
			{Role: "user", Content: "hello"},
		},
	}
	panel.SetData("run-1", "", "", res)
	assert.NotEmpty(t, panel.View())

	panel.Reset()
	panel.SetData("run-1", "", "", nil)
	out := panel.View()
	assert.Contains(t, out, "No conversation")
}

func TestRunsPanel_Basic(t *testing.T) {
	panel := NewRunsPanel()
	panel.Init(30)

	runs := []RunInfo{
		{RunID: "run-1", Scenario: "s1", Provider: "p1", Status: StatusRunning},
		{RunID: "run-2", Scenario: "s2", Provider: "p2", Status: StatusCompleted},
	}

	panel.Update(runs, 100, 30)
	view := panel.View(true)
	assert.Contains(t, view, "s1")
	assert.Contains(t, view, "p1")
}

func TestLogsPanel_Basic(t *testing.T) {
	panel := NewLogsPanel()

	logs := []LogEntry{
		{Level: "INFO", Message: "test log"},
		{Level: "ERROR", Message: "error log"},
	}

	panel.Update(logs, 100, 30)
	view := panel.View(true)
	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Logs")

	// Test unfocused
	view = panel.View(false)
	assert.NotEmpty(t, view)

	// Test with empty logs
	panel.Update([]LogEntry{}, 100, 30)
	view = panel.View(true)
	assert.Contains(t, view, "No logs")

	// Test with many logs
	manyLogs := make([]LogEntry, 100)
	for i := 0; i < 100; i++ {
		manyLogs[i] = LogEntry{
			Level:   "INFO",
			Message: "Log entry",
		}
	}
	panel.Update(manyLogs, 100, 30)
	view = panel.View(true)
	assert.NotEmpty(t, view)

	// Test viewport accessor
	vp := panel.Viewport()
	assert.NotNil(t, vp)

	// Test dimension changes
	panel.Update(logs, 50, 20)
	view = panel.View(true)
	assert.NotEmpty(t, view)

	// Test very small dimensions
	panel.Update(logs, 30, 10)
	view = panel.View(true)
	assert.NotEmpty(t, view)
}

func TestResultPanel_Basic(t *testing.T) {
	panel := NewResultPanel()
	panel.Update(100, 30)

	// Test empty
	view := panel.View(nil)
	assert.NotEmpty(t, view)
	assert.Contains(t, view, "No run selected")

	// Test with no assertions
	data := &ResultPanelData{
		Result: &statestore.RunResult{
			ConversationAssertions: statestore.AssertionsSummary{
				Total:  0,
				Failed: 0,
			},
		},
	}
	view = panel.View(data)
	assert.Contains(t, view, "No assertions")

	// Test with passing assertions
	data = &ResultPanelData{
		Result: &statestore.RunResult{
			ConversationAssertions: statestore.AssertionsSummary{
				Total:  2,
				Failed: 0,
				Results: []statestore.ConversationValidationResult{
					{
						Type:    "content_includes",
						Passed:  true,
						Message: "Content check passed",
					},
					{
						Type:       "response_length",
						Passed:     true,
						Message:    "Length within bounds",
						Violations: []assertions.ConversationViolation{},
					},
				},
			},
		},
	}
	view = panel.View(data)
	assert.Contains(t, view, "2/2 passed")
	assert.Contains(t, view, "✓")

	// Test with failing assertions
	data = &ResultPanelData{
		Result: &statestore.RunResult{
			ConversationAssertions: statestore.AssertionsSummary{
				Total:  2,
				Failed: 1,
				Results: []statestore.ConversationValidationResult{
					{
						Type:    "content_includes",
						Passed:  false,
						Message: "Missing required pattern",
						Violations: []assertions.ConversationViolation{
							{Description: "Pattern 'important' not found"},
						},
					},
					{
						Type:    "response_length",
						Passed:  true,
						Message: "Length OK",
					},
				},
			},
		},
	}
	view = panel.View(data)
	assert.Contains(t, view, "1/2 passed")
	assert.Contains(t, view, "✗")
	// The ellipsis shows as "…" in the rendered view
	assert.Contains(t, view, "1 …")

	// Test long type name truncation
	data = &ResultPanelData{
		Result: &statestore.RunResult{
			ConversationAssertions: statestore.AssertionsSummary{
				Total:  1,
				Failed: 0,
				Results: []statestore.ConversationValidationResult{
					{
						Type:    "very_long_assertion_type_name_that_exceeds_limit",
						Passed:  true,
						Message: "OK",
					},
				},
			},
		},
	}
	view = panel.View(data)
	// The ellipsis character shows as "…" in lipgloss rendering
	assert.Contains(t, view, "…")

	// Test long details truncation
	data = &ResultPanelData{
		Result: &statestore.RunResult{
			ConversationAssertions: statestore.AssertionsSummary{
				Total:  1,
				Failed: 0,
				Results: []statestore.ConversationValidationResult{
					{
						Type:    "test",
						Passed:  true,
						Message: "This is a very long message that should be truncated to fit within the column width limits",
					},
				},
			},
		},
	}
	view = panel.View(data)
	assert.Contains(t, view, "…")

	// Test not ready state
	newPanel := NewResultPanel()
	view = newPanel.View(nil)
	assert.Equal(t, "Loading...", view)

	// Test table accessor
	panel.Update(100, 30)
	table := panel.Table()
	assert.NotNil(t, table)
}

func TestResultPanel_Update_MultipleTimes(t *testing.T) {
	panel := NewResultPanel()

	// First update initializes
	panel.Update(100, 30)
	assert.True(t, panel.ready)

	// Second update adjusts dimensions
	panel.Update(200, 50)
	assert.True(t, panel.ready)

	// Small dimensions
	panel.Update(40, 10)
	assert.True(t, panel.ready)

	// Very small height
	panel.Update(100, 3)
	view := panel.View(nil)
	assert.NotEmpty(t, view)
	assert.Contains(t, view, "No run selected")
}

func TestResultPanel_WithAssertions(t *testing.T) {
	panel := NewResultPanel()
	panel.Update(100, 30)

	// Test with assertions
	data := &ResultPanelData{
		Result: &statestore.RunResult{
			RunID: "run-1",
			ConversationAssertions: statestore.AssertionsSummary{
				Total:  2,
				Failed: 1,
				Results: []statestore.ConversationValidationResult{
					{
						Type:    "content_includes",
						Passed:  true,
						Message: "Content check passed",
					},
					{
						Type:    "tools_called",
						Passed:  false,
						Message: "Tool not called",
						Violations: []assertions.ConversationViolation{
							{Description: "expected tool X"},
						},
					},
				},
			},
		},
	}

	view := panel.View(data)
	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Assertions")
	assert.Contains(t, view, "1/2")
	assert.Contains(t, view, "content_includes")
	assert.Contains(t, view, "tools_called")
}

func TestResultPanel_NoAssertions(t *testing.T) {
	panel := NewResultPanel()
	panel.Update(100, 30)

	data := &ResultPanelData{
		Result: &statestore.RunResult{
			RunID: "run-1",
			ConversationAssertions: statestore.AssertionsSummary{
				Total: 0,
			},
		},
	}

	view := panel.View(data)
	assert.NotEmpty(t, view)
	assert.Contains(t, view, "No assertions")
}

func TestResultPanel_TruncatesLongText(t *testing.T) {
	panel := NewResultPanel()
	panel.Update(100, 30)

	longType := "this_is_a_very_long_assertion_type_name_that_should_be_truncated"
	longMessage := "This is a very long message that should be truncated because it exceeds the maximum length allowed for the details column"

	data := &ResultPanelData{
		Result: &statestore.RunResult{
			RunID: "run-1",
			ConversationAssertions: statestore.AssertionsSummary{
				Total:  1,
				Failed: 0,
				Results: []statestore.ConversationValidationResult{
					{
						Type:    longType,
						Passed:  true,
						Message: longMessage,
					},
				},
			},
		},
	}

	view := panel.View(data)
	assert.NotEmpty(t, view)
	// Just verify it renders without error - truncation is visible in the output
	assert.Contains(t, view, "this_is_a_very_long")
}

func TestResultPanel_TableAccessor(t *testing.T) {
	panel := NewResultPanel()
	panel.Update(100, 30)

	table := panel.Table()
	assert.NotNil(t, table)
}

func TestSummaryPanel_Basic(t *testing.T) {
	panel := NewSummaryPanel()
	panel.Update(100)

	// Basic smoke test - full test would need viewmodel
	assert.NotNil(t, panel)
}

func TestLogsPanel_Empty(t *testing.T) {
	panel := NewLogsPanel()
	panel.Update([]LogEntry{}, 100, 30)

	view := panel.View(false)
	assert.NotEmpty(t, view)
}

func TestLogsPanel_Focused(t *testing.T) {
	panel := NewLogsPanel()
	panel.Init(100, 30)

	logs := []LogEntry{
		{Level: "INFO", Message: "test log 1"},
		{Level: "WARN", Message: "test log 2"},
		{Level: "ERROR", Message: "test log 3"},
	}

	panel.Update(logs, 100, 30)

	// Test unfocused
	viewUnfocused := panel.View(false)
	assert.NotEmpty(t, viewUnfocused)
	assert.Contains(t, viewUnfocused, "Logs")

	// Test focused
	viewFocused := panel.View(true)
	assert.NotEmpty(t, viewFocused)
	assert.Contains(t, viewFocused, "Logs")
}

func TestLogsPanel_DifferentLevels(t *testing.T) {
	panel := NewLogsPanel()
	panel.Init(120, 40)

	logs := []LogEntry{
		{Level: "DEBUG", Message: "debug message"},
		{Level: "INFO", Message: "info message"},
		{Level: "WARN", Message: "warning message"},
		{Level: "ERROR", Message: "error message"},
	}

	panel.Update(logs, 120, 40)
	view := panel.View(false)

	// Verify panel renders with content
	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Logs")
}

func TestLogsPanel_GotoBottom(t *testing.T) {
	panel := NewLogsPanel()
	panel.Init(100, 30)

	// Should not panic
	panel.GotoBottom()

	// Add logs
	logs := []LogEntry{
		{Level: "INFO", Message: "line 1"},
		{Level: "INFO", Message: "line 2"},
	}
	panel.Update(logs, 100, 30)
	panel.GotoBottom()

	view := panel.View(false)
	assert.NotEmpty(t, view)
}

func TestLogsPanel_Viewport(t *testing.T) {
	panel := NewLogsPanel()

	// Create many logs to test viewport scrolling
	logs := make([]LogEntry, 50)
	for i := 0; i < 50; i++ {
		logs[i] = LogEntry{
			Level:   "INFO",
			Message: "log message",
		}
	}

	panel.Update(logs, 100, 20)
	viewport := panel.Viewport()
	assert.NotNil(t, viewport)
}

func TestBuildDetailsText(t *testing.T) {
	// Test with message only
	result := statestore.ConversationValidationResult{
		Message: "Test message",
	}
	details := buildDetailsText(result)
	assert.Equal(t, "Test message", details)

	// Test with violations only
	result = statestore.ConversationValidationResult{
		Violations: []assertions.ConversationViolation{
			{Description: "violation1"},
			{Description: "violation2"},
		},
	}
	details = buildDetailsText(result)
	assert.Contains(t, details, "2 violation(s)")

	// Test with both
	result = statestore.ConversationValidationResult{
		Message: "Test message",
		Violations: []assertions.ConversationViolation{
			{Description: "violation1"},
		},
	}
	details = buildDetailsText(result)
	assert.Contains(t, details, "Test message")
	assert.Contains(t, details, "1 violation(s)")
}

func TestTruncateString(t *testing.T) {
	// Short string - no truncation
	result := truncateString("short", 10, 7)
	assert.Equal(t, "short", result)

	// Long string - truncate
	result = truncateString("this is a very long string", 10, 7)
	assert.Equal(t, "this is...", result)
	assert.Equal(t, 10, len(result))
}
