package panels

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

func TestNewConversationPanel(t *testing.T) {
	panel := NewConversationPanel()

	assert.NotNil(t, panel)
	assert.Equal(t, focusConversationTurns, panel.focus)
	assert.Equal(t, 0, panel.selectedTurnIdx)
	assert.NotNil(t, panel.renderedCache)
	assert.Empty(t, panel.renderedCache)
	assert.False(t, panel.tableReady)
	assert.False(t, panel.detailReady)
}

func TestConversationPanel_SetDimensions(t *testing.T) {
	panel := NewConversationPanel()

	panel.SetDimensions(100, 50)

	assert.Equal(t, 100, panel.width)
	assert.Equal(t, 50, panel.height)
}

func TestConversationPanel_SetData_NilResult(t *testing.T) {
	panel := NewConversationPanel()
	panel.tableReady = true
	panel.detailReady = true
	panel.selectedTurnIdx = 5

	panel.SetData("run-123", "scenario", "provider", nil)

	// Should reset everything
	assert.False(t, panel.tableReady)
	assert.False(t, panel.detailReady)
	assert.Equal(t, 0, panel.selectedTurnIdx)
	assert.Empty(t, panel.lastRunID)
	assert.Nil(t, panel.res)
}

func TestConversationPanel_SetData_ValidResult(t *testing.T) {
	panel := NewConversationPanel()
	panel.SetDimensions(100, 50)

	result := &statestore.RunResult{
		RunID: "run-123",
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there"},
		},
		Cost: types.CostInfo{
			InputTokens:  10,
			OutputTokens: 20,
			TotalCost:    0.001,
		},
		Duration: time.Second,
	}

	panel.SetData("run-123", "test-scenario", "test-provider", result)

	assert.True(t, panel.tableReady)
	assert.Equal(t, "run-123", panel.lastRunID)
	assert.Equal(t, "test-scenario", panel.scenario)
	assert.Equal(t, "test-provider", panel.provider)
	assert.Equal(t, result, panel.res)
}

func TestConversationPanel_Update_NotReady(t *testing.T) {
	panel := NewConversationPanel()

	cmd := panel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})

	assert.Nil(t, cmd)
}

func TestConversationPanel_Update_KeyNavigation(t *testing.T) {
	panel := NewConversationPanel()
	panel.SetDimensions(100, 50)

	result := &statestore.RunResult{
		RunID: "run-123",
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi"},
		},
		Cost:     types.CostInfo{},
		Duration: time.Second,
	}

	panel.SetData("run-123", "scenario", "provider", result)

	// Test switching from turns to detail
	assert.Equal(t, focusConversationTurns, panel.focus)

	cmd := panel.Update(tea.KeyMsg{Type: tea.KeyRight})
	assert.Equal(t, focusConversationDetail, panel.focus)
	_ = cmd

	// Test switching back
	cmd = panel.Update(tea.KeyMsg{Type: tea.KeyLeft})
	assert.Equal(t, focusConversationTurns, panel.focus)
	_ = cmd
}

func TestConversationPanel_HandleKeyMsg_RightKey(t *testing.T) {
	panel := NewConversationPanel()
	panel.focus = focusConversationTurns
	panel.SetDimensions(100, 50)

	result := &statestore.RunResult{
		RunID: "run-123",
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
		Cost:     types.CostInfo{},
		Duration: time.Second,
	}
	panel.SetData("run-123", "scenario", "provider", result)

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")}
	cmd := panel.handleKeyMsg(msg)

	assert.Equal(t, focusConversationDetail, panel.focus)
	_ = cmd // cmd may be nil, that's OK
}

func TestConversationPanel_HandleKeyMsg_LeftKey(t *testing.T) {
	panel := NewConversationPanel()
	panel.focus = focusConversationDetail
	panel.tableReady = true

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")}
	cmd := panel.handleKeyMsg(msg)

	assert.Equal(t, focusConversationTurns, panel.focus)
	_ = cmd // cmd may be nil, that's OK
}

func TestConversationPanel_HandleKeyMsg_NotKeyMsg(t *testing.T) {
	panel := NewConversationPanel()

	cmd := panel.handleKeyMsg(tea.WindowSizeMsg{Width: 100, Height: 50})

	assert.Nil(t, cmd)
}

func TestConversationPanel_View_NilResult(t *testing.T) {
	panel := NewConversationPanel()
	panel.res = nil

	view := panel.View()

	assert.Contains(t, view, "No conversation available")
}

func TestConversationPanel_View_EmptyMessages(t *testing.T) {
	panel := NewConversationPanel()
	panel.res = &statestore.RunResult{
		Messages: []types.Message{},
	}

	view := panel.View()

	assert.Contains(t, view, "No conversation recorded")
}

func TestConversationPanel_View_WithMessages(t *testing.T) {
	panel := NewConversationPanel()
	panel.SetDimensions(100, 50)

	result := &statestore.RunResult{
		RunID: "run-123",
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there"},
		},
		Cost: types.CostInfo{
			InputTokens:  10,
			OutputTokens: 20,
			TotalCost:    0.001,
		},
		Duration: time.Second,
	}

	panel.SetData("run-123", "test-scenario", "test-provider", result)

	view := panel.View()

	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Conversation")
}

func TestConversationPanel_BuildTitle_NoMetadata(t *testing.T) {
	panel := NewConversationPanel()

	title := panel.buildTitle()

	assert.Contains(t, title, "Conversation")
}

func TestConversationPanel_BuildTitle_WithMetadata(t *testing.T) {
	panel := NewConversationPanel()
	panel.scenario = "test-scenario"
	panel.provider = "test-provider"

	title := panel.buildTitle()

	assert.Contains(t, title, "Conversation")
	assert.Contains(t, title, "test-scenario")
	assert.Contains(t, title, "test-provider")
}

func TestConversationPanel_GetBorderColors(t *testing.T) {
	panel := NewConversationPanel()

	// Test with turns focus
	panel.focus = focusConversationTurns
	tableColor, detailColor := panel.getBorderColors()
	assert.NotEmpty(t, tableColor)
	assert.NotEmpty(t, detailColor)

	// Test with detail focus
	panel.focus = focusConversationDetail
	tableColor2, detailColor2 := panel.getBorderColors()
	assert.NotEmpty(t, tableColor2)
	assert.NotEmpty(t, detailColor2)
}

func TestConversationPanel_CalculateViewportWidth(t *testing.T) {
	panel := NewConversationPanel()

	tests := []struct {
		name     string
		width    int
		expected int
	}{
		{"large width", 200, 200 - conversationListWidth - conversationPanelGap - conversationDetailWidthPad},
		{"small width", 50, conversationDetailMinWidth},
		{"exact minimum", conversationListWidth + conversationPanelGap + conversationDetailWidthPad + conversationDetailMinWidth, conversationDetailMinWidth},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			panel.width = tt.width
			result := panel.calculateViewportWidth()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConversationPanel_UpdateTable(t *testing.T) {
	panel := NewConversationPanel()
	panel.SetDimensions(100, 50)

	result := &statestore.RunResult{
		RunID: "run-123",
		Messages: []types.Message{
			{Role: "user", Content: "Message 1"},
			{Role: "assistant", Content: "Message 2"},
			{Role: "user", Content: "Message 3"},
		},
		Cost:     types.CostInfo{},
		Duration: time.Second,
	}

	panel.SetData("run-123", "scenario", "provider", result)
	panel.updateTable(result)

	// Table should be updated
	assert.True(t, panel.tableReady)
}

func TestConversationPanel_UpdateTable_NotReady(t *testing.T) {
	panel := NewConversationPanel()
	panel.tableReady = false

	result := &statestore.RunResult{
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
	}

	// Should not panic
	panel.updateTable(result)
}

func TestConversationPanel_UpdateTable_IndexOutOfBounds(t *testing.T) {
	panel := NewConversationPanel()
	panel.SetDimensions(100, 50)

	result := &statestore.RunResult{
		RunID: "run-123",
		Messages: []types.Message{
			{Role: "user", Content: "Message 1"},
		},
		Cost:     types.CostInfo{},
		Duration: time.Second,
	}

	panel.SetData("run-123", "scenario", "provider", result)
	panel.selectedTurnIdx = 10 // Out of bounds

	panel.updateTable(result)

	// Should be clamped
	assert.Equal(t, 0, panel.selectedTurnIdx)
}

func TestConversationPanel_UpdateDetail(t *testing.T) {
	panel := NewConversationPanel()
	panel.SetDimensions(100, 50)

	result := &statestore.RunResult{
		RunID: "run-123",
		Messages: []types.Message{
			{
				Role:    "user",
				Content: "Hello",
			},
		},
		Cost: types.CostInfo{
			InputTokens:  10,
			OutputTokens: 20,
			TotalCost:    0.001,
		},
		Duration: time.Second,
	}

	panel.SetData("run-123", "scenario", "provider", result)

	assert.True(t, panel.detailReady)
}

func TestConversationPanel_UpdateDetail_EmptyMessages(t *testing.T) {
	panel := NewConversationPanel()
	panel.SetDimensions(100, 50)

	result := &statestore.RunResult{
		Messages: []types.Message{},
	}

	panel.updateDetail(result)

	// Should not panic
	assert.False(t, panel.detailReady)
}

func TestConversationPanel_UpdateDetail_IndexClamping(t *testing.T) {
	panel := NewConversationPanel()
	panel.SetDimensions(100, 50)

	result := &statestore.RunResult{
		RunID: "run-123",
		Messages: []types.Message{
			{Role: "user", Content: "Message"},
		},
		Cost:     types.CostInfo{},
		Duration: time.Second,
	}

	// Test negative index
	panel.selectedTurnIdx = -5
	panel.updateDetail(result)
	assert.Equal(t, 0, panel.selectedTurnIdx)

	// Test out of bounds index
	panel.selectedTurnIdx = 10
	panel.updateDetail(result)
	assert.Equal(t, 0, panel.selectedTurnIdx)
}

func TestConversationPanel_FormatJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantErr  bool
		contains string
	}{
		{
			name:     "valid json",
			input:    `{"key":"value"}`,
			wantErr:  false,
			contains: "key",
		},
		{
			name:     "invalid json",
			input:    `{invalid}`,
			wantErr:  false,
			contains: "{invalid}",
		},
		{
			name:     "empty string",
			input:    "",
			wantErr:  false,
			contains: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatJSON(tt.input)
			assert.Contains(t, result, tt.contains)
		})
	}
}

func TestConversationPanel_RenderMarkdown(t *testing.T) {
	panel := NewConversationPanel()
	panel.SetDimensions(100, 50)

	// Test with empty content
	result := panel.renderMarkdown("")
	assert.Empty(t, result)

	// Test with simple content
	result = panel.renderMarkdown("# Hello\n\nThis is a test")
	assert.NotEmpty(t, result)
}

func TestConversationPanel_RenderMarkdown_Caching(t *testing.T) {
	panel := NewConversationPanel()
	panel.SetDimensions(100, 50)
	panel.selectedTurnIdx = 0

	// First render
	content := "# Test Content"
	result1 := panel.renderMarkdown(content)

	// Second render with same index and width should use cache
	result2 := panel.renderMarkdown(content)

	// Results should be the same (cached)
	assert.Equal(t, result1, result2)
}

func TestConversationPanel_RenderMarkdown_CacheInvalidation(t *testing.T) {
	panel := NewConversationPanel()
	panel.SetDimensions(100, 50)
	panel.selectedTurnIdx = 0

	// First render
	content := "# Test Content"
	_ = panel.renderMarkdown(content)

	// Change width - should invalidate cache
	panel.SetDimensions(150, 50)
	result := panel.renderMarkdown(content)
	assert.NotEmpty(t, result)
}

func TestConversationPanel_AppendToolCallsMarkdown(t *testing.T) {
	panel := NewConversationPanel()
	var md strings.Builder

	msg := &types.Message{
		ToolCalls: []types.MessageToolCall{
			{
				Name: "test_tool",
				Args: []byte(`{"arg1":"value1"}`),
			},
		},
	}

	panel.appendToolCallsMarkdown(&md, msg)

	result := md.String()
	assert.Contains(t, result, "Tool Calls")
	assert.Contains(t, result, "test_tool")
}

func TestConversationPanel_AppendToolCallsMarkdown_Empty(t *testing.T) {
	panel := NewConversationPanel()
	var md strings.Builder

	msg := &types.Message{
		ToolCalls: []types.MessageToolCall{},
	}

	panel.appendToolCallsMarkdown(&md, msg)

	result := md.String()
	assert.Empty(t, result)
}

func TestConversationPanel_AppendToolResultMarkdown(t *testing.T) {
	panel := NewConversationPanel()
	var md strings.Builder

	msg := &types.Message{
		ToolResult: &types.MessageToolResult{
			Name:    "test_tool",
			Content: `{"result":"success"}`,
		},
	}

	panel.appendToolResultMarkdown(&md, msg)

	result := md.String()
	assert.Contains(t, result, "Tool Result")
	assert.Contains(t, result, "test_tool")
}

func TestConversationPanel_AppendToolResultMarkdown_WithError(t *testing.T) {
	panel := NewConversationPanel()
	var md strings.Builder

	msg := &types.Message{
		ToolResult: &types.MessageToolResult{
			Name:  "test_tool",
			Error: "something went wrong",
		},
	}

	panel.appendToolResultMarkdown(&md, msg)

	result := md.String()
	assert.Contains(t, result, "Error")
	assert.Contains(t, result, "something went wrong")
}

func TestConversationPanel_AppendToolResultMarkdown_Nil(t *testing.T) {
	panel := NewConversationPanel()
	var md strings.Builder

	msg := &types.Message{
		ToolResult: nil,
	}

	panel.appendToolResultMarkdown(&md, msg)

	result := md.String()
	assert.Empty(t, result)
}

func TestConversationPanel_AppendContentMarkdown(t *testing.T) {
	panel := NewConversationPanel()
	var md strings.Builder

	msg := &types.Message{
		Content: "Hello, world!",
	}

	panel.appendContentMarkdown(&md, msg)

	result := md.String()
	assert.Contains(t, result, "Message")
	assert.Contains(t, result, "Hello, world!")
}

func TestConversationPanel_AppendContentMarkdown_Empty(t *testing.T) {
	panel := NewConversationPanel()
	var md strings.Builder

	msg := &types.Message{
		Content: "",
	}

	panel.appendContentMarkdown(&md, msg)

	result := md.String()
	assert.Empty(t, result)
}

func TestConversationPanel_AppendValidationsMarkdown(t *testing.T) {
	panel := NewConversationPanel()
	var md strings.Builder

	msg := &types.Message{
		Validations: []types.ValidationResult{
			{ValidatorType: "test_validator", Passed: true},
			{ValidatorType: "test_validator_2", Passed: false},
		},
	}

	panel.appendValidationsMarkdown(&md, msg)

	result := md.String()
	assert.Contains(t, result, "Validations")
	assert.Contains(t, result, "PASS")
	assert.Contains(t, result, "FAIL")
}

func TestConversationPanel_AppendValidationsMarkdown_Empty(t *testing.T) {
	panel := NewConversationPanel()
	var md strings.Builder

	msg := &types.Message{
		Validations: []types.ValidationResult{},
	}

	panel.appendValidationsMarkdown(&md, msg)

	result := md.String()
	assert.Empty(t, result)
}

func TestConversationPanel_RenderDetailHeader(t *testing.T) {
	panel := NewConversationPanel()

	result := &statestore.RunResult{
		Cost: types.CostInfo{
			InputTokens:  100,
			OutputTokens: 200,
			TotalCost:    0.015,
		},
		Duration: 2 * time.Second,
	}

	msg := &types.Message{
		Role: "assistant",
		CostInfo: &types.CostInfo{
			InputTokens:  100,
			OutputTokens: 200,
			TotalCost:    0.015,
		},
		LatencyMs: 2000,
	}

	header := panel.renderDetailHeader(result, 0, msg)

	assert.Contains(t, header, "Turn 1")
	assert.Contains(t, header, "assistant")
	assert.Contains(t, header, "100")
	assert.Contains(t, header, "200")
}

func TestConversationPanel_BuildDetailLines(t *testing.T) {
	panel := NewConversationPanel()
	panel.SetDimensions(100, 50)

	result := &statestore.RunResult{
		Cost: types.CostInfo{
			InputTokens:  10,
			OutputTokens: 20,
			TotalCost:    0.001,
		},
		Duration: time.Second,
	}

	msg := &types.Message{
		Role:    "user",
		Content: "Test message",
	}

	lines := panel.buildDetailLines(result, msg)

	assert.NotEmpty(t, lines)
	// Should have at least header
	assert.GreaterOrEqual(t, len(lines), 1)
}

func TestConversationPanel_UpdateTablePanel(t *testing.T) {
	panel := NewConversationPanel()
	panel.SetDimensions(100, 50)

	result := &statestore.RunResult{
		RunID: "run-123",
		Messages: []types.Message{
			{Role: "user", Content: "Msg 1"},
			{Role: "assistant", Content: "Msg 2"},
		},
		Cost:     types.CostInfo{},
		Duration: time.Second,
	}

	panel.SetData("run-123", "scenario", "provider", result)

	// Test cursor movement
	oldIdx := panel.selectedTurnIdx
	msg := tea.KeyMsg{Type: tea.KeyDown}
	_ = panel.updateTablePanel(msg)

	// Cursor should have moved (or stayed if at boundary)
	_ = oldIdx
}

func TestConversationPanel_AddScrollIndicators_NotScrollable(t *testing.T) {
	panel := NewConversationPanel()
	panel.SetDimensions(100, 50)

	content := "Short content"

	// Create a detail viewport
	panel.detail.Width = 80
	panel.detail.Height = 20
	panel.detail.SetContent(content)
	panel.detailReady = true

	result := panel.addScrollIndicators(content)

	// Should return content unchanged if not scrollable
	assert.Equal(t, content, result)
}

func TestConversationPanel_Constants(t *testing.T) {
	// Verify constants are defined
	assert.Equal(t, 40, conversationListWidth)
	assert.Equal(t, 1, conversationPanelPadding)
	assert.Equal(t, 1, conversationPanelGap)
	assert.Equal(t, 2, conversationPanelHorizontal)
	assert.Equal(t, 60, conversationSnippetMaxLength)
	assert.Equal(t, 6, conversationDetailWidthPad)
	assert.Equal(t, 40, conversationDetailMinWidth)
	assert.Equal(t, 6, conversationTableHeightPad)
	assert.Equal(t, 6, conversationTableMinHeight)
	assert.Equal(t, 6, conversationDetailMinHeight)
	assert.Equal(t, 4, conversationColNumWidth)
	assert.Equal(t, 10, conversationColRoleWidth)
	assert.Equal(t, 20, conversationContentPadding)
}

func TestConversationPanel_EnsureTable_SameRunID(t *testing.T) {
	panel := NewConversationPanel()
	panel.SetDimensions(100, 50)

	result := &statestore.RunResult{
		RunID: "run-123",
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
		Cost:     types.CostInfo{},
		Duration: time.Second,
	}

	// First call
	panel.SetData("run-123", "scenario", "provider", result)
	assert.True(t, panel.tableReady)

	// Second call with same runID should not recreate table
	panel.ensureTable("run-123")
	assert.True(t, panel.tableReady)
	assert.Equal(t, "run-123", panel.lastRunID)
}

func TestConversationPanel_EnsureTable_DifferentRunID(t *testing.T) {
	panel := NewConversationPanel()
	panel.SetDimensions(100, 50)

	// First run
	result1 := &statestore.RunResult{
		RunID: "run-123",
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
		Cost:     types.CostInfo{},
		Duration: time.Second,
	}
	panel.SetData("run-123", "scenario", "provider", result1)

	// Different runID should recreate table
	panel.ensureTable("run-456")
	assert.True(t, panel.tableReady)
	assert.Equal(t, "run-456", panel.lastRunID)
}

func TestConversationPanel_BuildTableView(t *testing.T) {
	panel := NewConversationPanel()
	panel.SetDimensions(100, 50)

	result := &statestore.RunResult{
		RunID: "run-123",
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
		Cost:     types.CostInfo{},
		Duration: time.Second,
	}

	panel.SetData("run-123", "scenario", "provider", result)

	view := panel.buildTableView()
	assert.NotEmpty(t, view)
}

func TestConversationPanel_BuildDetailView(t *testing.T) {
	panel := NewConversationPanel()
	panel.SetDimensions(100, 50)

	result := &statestore.RunResult{
		RunID: "run-123",
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
		Cost:     types.CostInfo{},
		Duration: time.Second,
	}

	panel.SetData("run-123", "scenario", "provider", result)

	view := panel.buildDetailView()
	assert.NotEmpty(t, view)
}

func TestConversationPanel_UpdateFocusedPanel_TurnsFocus(t *testing.T) {
	panel := NewConversationPanel()
	panel.SetDimensions(100, 50)

	result := &statestore.RunResult{
		RunID: "run-123",
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
		Cost:     types.CostInfo{},
		Duration: time.Second,
	}

	panel.SetData("run-123", "scenario", "provider", result)
	panel.focus = focusConversationTurns

	msg := tea.KeyMsg{Type: tea.KeyDown}
	cmd := panel.updateFocusedPanel(msg)

	// Should update table panel
	_ = cmd
}

func TestConversationPanel_UpdateFocusedPanel_DetailFocus(t *testing.T) {
	panel := NewConversationPanel()
	panel.SetDimensions(100, 50)

	result := &statestore.RunResult{
		RunID: "run-123",
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
		Cost:     types.CostInfo{},
		Duration: time.Second,
	}

	panel.SetData("run-123", "scenario", "provider", result)
	panel.focus = focusConversationDetail

	msg := tea.KeyMsg{Type: tea.KeyDown}
	cmd := panel.updateFocusedPanel(msg)

	// Should update detail viewport
	_ = cmd
}

func TestConversationPanel_UpdateFocusedPanel_NoFocus(t *testing.T) {
	panel := NewConversationPanel()

	msg := tea.KeyMsg{Type: tea.KeyDown}
	cmd := panel.updateFocusedPanel(msg)

	assert.Nil(t, cmd)
}
